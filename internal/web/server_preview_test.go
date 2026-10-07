package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"artifactd/internal/markdown"
	"artifactd/internal/preview"
)

type editablePreviewFixture struct {
	server   *Server
	manager  *preview.Manager
	document preview.Document
	path     string
}

func newEditablePreviewFixture(t *testing.T, source string) editablePreviewFixture {
	t.Helper()
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("closing test store: %v", err)
		}
	})
	filePath := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(filePath, []byte(source), 0o640); err != nil {
		t.Fatal(err)
	}
	manager := preview.NewManager()
	t.Cleanup(manager.Close)
	document, err := manager.CreateEditable("notes.md", filePath, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(store)
	server.SetPreviewManager(manager)
	return editablePreviewFixture{server: server, manager: manager, document: document, path: filePath}
}

func (f editablePreviewFixture) post(t *testing.T, action, source string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"content": source})
	if err != nil {
		t.Fatal(err)
	}
	host := f.document.ID + ".artifacts.localhost:7337"
	request := httptest.NewRequest(http.MethodPost, "http://"+host+"/_artifactd/"+action, bytes.NewReader(body))
	request.Header.Set("Origin", "http://"+host)
	request.Header.Set("Authorization", "Bearer "+f.document.SaveToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestPreviewDraftRenderingDoesNotSave(t *testing.T) {
	t.Parallel()
	fixture := newEditablePreviewFixture(t, "# Saved")
	source := "# Draft\n\n- [x] done\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n<script>alert(1)</script>\n\n[bad](javascript:alert(1))"
	response := fixture.post(t, "render", source)
	if response.Code != http.StatusOK {
		t.Fatalf("render response = %d %s", response.Code, response.Body.String())
	}
	var result struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	want, err := markdown.RenderBody([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if result.HTML != string(want) || strings.Contains(result.HTML, "<script>") || strings.Contains(result.HTML, `href="javascript:`) {
		t.Fatalf("unsafe or mismatched draft rendering: %s", result.HTML)
	}
	current, err := fixture.manager.Get(fixture.document.ID)
	if err != nil || string(current.Content) != "# Saved" {
		t.Fatalf("render changed saved preview: %q, %v", current.Content, err)
	}
	file, err := os.ReadFile(fixture.path)
	if err != nil || string(file) != "# Saved" {
		t.Fatalf("render changed selected file: %q, %v", file, err)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("draft rendering must not be cached")
	}
}

func TestPreviewContentRequestsRejectInvalidInput(t *testing.T) {
	t.Parallel()
	fixture := newEditablePreviewFixture(t, "# Saved")
	host := fixture.document.ID + ".artifacts.localhost:7337"
	tests := []struct {
		name   string
		origin string
		token  string
		body   string
		want   int
	}{
		{name: "missing origin", token: fixture.document.SaveToken, body: `{"content":"draft"}`, want: http.StatusForbidden},
		{name: "cross origin", origin: "http://attacker.invalid", token: fixture.document.SaveToken, body: `{"content":"draft"}`, want: http.StatusForbidden},
		{name: "different scheme", origin: "https://" + host, token: fixture.document.SaveToken, body: `{"content":"draft"}`, want: http.StatusForbidden},
		{name: "origin with path", origin: "http://" + host + "/", token: fixture.document.SaveToken, body: `{"content":"draft"}`, want: http.StatusForbidden},
		{name: "missing capability", origin: "http://" + host, body: `{"content":"draft"}`, want: http.StatusUnauthorized},
		{name: "wrong capability", origin: "http://" + host, token: "wrong", body: `{"content":"draft"}`, want: http.StatusUnauthorized},
		{name: "malformed JSON", origin: "http://" + host, token: fixture.document.SaveToken, body: `{`, want: http.StatusBadRequest},
		{name: "missing content", origin: "http://" + host, token: fixture.document.SaveToken, body: `{}`, want: http.StatusBadRequest},
		{name: "null content", origin: "http://" + host, token: fixture.document.SaveToken, body: `{"content":null}`, want: http.StatusBadRequest},
		{name: "unknown field", origin: "http://" + host, token: fixture.document.SaveToken, body: `{"content":"draft","path":"/tmp/other"}`, want: http.StatusBadRequest},
		{name: "trailing JSON", origin: "http://" + host, token: fixture.document.SaveToken, body: `{"content":"draft"} {}`, want: http.StatusBadRequest},
		{name: "oversized document", origin: "http://" + host, token: fixture.document.SaveToken, body: `{"content":"` + strings.Repeat("x", preview.MaxBytes+1) + `"}`, want: http.StatusRequestEntityTooLarge},
		{name: "oversized envelope", origin: "http://" + host, token: fixture.document.SaveToken, body: `{"content":"` + strings.Repeat("x", 6*preview.MaxBytes+1024), want: http.StatusRequestEntityTooLarge},
	}
	for _, action := range []string{"render", "save"} {
		t.Run(action, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					request := httptest.NewRequest(http.MethodPost, "http://"+host+"/_artifactd/"+action, strings.NewReader(tt.body))
					request.Header.Set("Origin", tt.origin)
					request.Header.Set("Authorization", "Bearer "+tt.token)
					recorder := httptest.NewRecorder()
					fixture.server.Handler().ServeHTTP(recorder, request)
					if recorder.Code != tt.want {
						t.Fatalf("response = %d, want %d: %s", recorder.Code, tt.want, recorder.Body.String())
					}
				})
			}
		})
	}
	file, err := os.ReadFile(fixture.path)
	if err != nil || string(file) != "# Saved" {
		t.Fatalf("invalid requests changed selected file: %q, %v", file, err)
	}
}

func TestPreviewContentRequestsRequireEditableLivePreview(t *testing.T) {
	t.Parallel()
	fixture := newEditablePreviewFixture(t, "# Saved")
	readOnly, err := fixture.manager.Create("readonly.md", []byte("# Read only"))
	if err != nil {
		t.Fatal(err)
	}
	readonlyFixture := fixture
	readonlyFixture.document = readOnly
	for _, action := range []string{"render", "save"} {
		t.Run(action, func(t *testing.T) {
			if got := readonlyFixture.post(t, action, "# Draft"); got.Code != http.StatusUnauthorized {
				t.Fatalf("read-only preview response = %d %s", got.Code, got.Body.String())
			}
		})
	}
	fixture.manager.Close()
	for _, action := range []string{"render", "save"} {
		if got := fixture.post(t, action, "# Draft"); got.Code != http.StatusNotFound {
			t.Fatalf("closed preview response = %d %s", got.Code, got.Body.String())
		}
	}
}

func TestPreviewSaveReturnsRenderedContentAndPreservesConflicts(t *testing.T) {
	t.Parallel()
	fixture := newEditablePreviewFixture(t, "# Saved")
	response := fixture.post(t, "save", "# New saved version")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "new-saved-version") {
		t.Fatalf("save response = %d %s", response.Code, response.Body.String())
	}
	if err := os.WriteFile(fixture.path, []byte("# External edit"), 0o640); err != nil {
		t.Fatal(err)
	}
	response = fixture.post(t, "save", "# My conflicting edit")
	if response.Code != http.StatusConflict {
		t.Fatalf("conflicting save response = %d %s", response.Code, response.Body.String())
	}
	file, err := os.ReadFile(fixture.path)
	if err != nil || string(file) != "# External edit" {
		t.Fatalf("save overwrote external edits: %q, %v", file, err)
	}
	current, err := fixture.manager.Get(fixture.document.ID)
	if err != nil || string(current.Content) != "# New saved version" {
		t.Fatalf("failed save changed baseline: %q, %v", current.Content, err)
	}
}

func TestPreviewContentRequestsAllowLargeJSONEscapedDocuments(t *testing.T) {
	t.Parallel()
	fixture := newEditablePreviewFixture(t, "# Saved")
	// '<' is expanded to six bytes by json.Marshal; the envelope exceeds 5 MiB.
	source := strings.Repeat("<", preview.MaxBytes/6+1)
	for _, action := range []string{"render", "save"} {
		if got := fixture.post(t, action, source); got.Code != http.StatusOK {
			t.Fatalf("escaped document %s response = %d %s", action, got.Code, got.Body.String())
		}
	}
}

func TestPreviewEditorUsesScopedStyleNonce(t *testing.T) {
	t.Parallel()
	fixture := newEditablePreviewFixture(t, "# Saved")
	previous := ""
	for range 2 {
		request := httptest.NewRequest(http.MethodGet, "http://"+fixture.document.ID+".artifacts.localhost/", nil)
		response := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(response, request)
		policy := response.Header().Get("Content-Security-Policy")
		const prefix = "style-src 'self' 'nonce-"
		_, suffix, ok := strings.Cut(policy, prefix)
		if !ok || strings.Contains(policy, "unsafe-inline") {
			t.Fatalf("invalid preview CSP: %q", policy)
		}
		nonce, _, ok := strings.Cut(suffix, "'")
		if !ok || nonce == "" || nonce == previous || !strings.Contains(response.Body.String(), `data-style-nonce="`+nonce+`"`) {
			t.Fatalf("nonce must be fresh and shared with editor: %q", nonce)
		}
		previous = nonce
	}
}
