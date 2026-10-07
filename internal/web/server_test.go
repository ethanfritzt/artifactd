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

	"artifactd/internal/create"
	"artifactd/internal/edit"
	"artifactd/internal/preview"
	"artifactd/internal/providers/filesystem"
	"artifactd/internal/providers/system"
	"artifactd/internal/runtime"
	"artifactd/internal/storage"
)

func TestServerServesCurrentArtifact(t *testing.T) {
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("closing test store: %v", err)
		}
	}()

	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	staging, err := store.NewStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyForWebTest(source, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishStaged(t.Context(), staging, ""); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/demo/", nil)
	request.Host = "artifacts.localhost"
	server := newTestServer(store)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Your artifact is ready.") {
		t.Fatal("response did not contain artifact content")
	}
	if !strings.Contains(body, `data-artifactd-edit-overlay`) {
		t.Fatal("published HTML did not contain the edit overlay")
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}

	runtimeRecorder := httptest.NewRecorder()
	runtimeRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/system", nil)
	runtimeRequest.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(runtimeRecorder, runtimeRequest)
	if runtimeRecorder.Code != http.StatusOK || !strings.Contains(runtimeRecorder.Body.String(), "processes") {
		t.Fatalf("runtime response = %d %s", runtimeRecorder.Code, runtimeRecorder.Body.String())
	}

	rootRecorder := httptest.NewRecorder()
	rootRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRequest.Host = "artifacts.localhost"
	server.Handler().ServeHTTP(rootRecorder, rootRequest)
	if rootRecorder.Code != http.StatusMovedPermanently || rootRecorder.Header().Get("Location") == "" {
		t.Fatalf("root redirect = %d %q", rootRecorder.Code, rootRecorder.Header().Get("Location"))
	}

	libraryRecorder := httptest.NewRecorder()
	libraryRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/library", nil)
	libraryRequest.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(libraryRecorder, libraryRequest)
	if libraryRecorder.Code != http.StatusOK || !strings.Contains(libraryRecorder.Body.String(), "demo") {
		t.Fatalf("library response = %d %s", libraryRecorder.Code, libraryRecorder.Body.String())
	}
}

func TestServerInjectsArtifactNavigation(t *testing.T) {
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("closing test store: %v", err)
		}
	}()

	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	staging, err := store.NewStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyForWebTest(source, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishStaged(t.Context(), staging, ""); err != nil {
		t.Fatal(err)
	}

	server := NewServer(store, "artifacts.localhost", func(id string) string {
		return "http://" + id + ".artifacts.localhost:7337/"
	}, "artifactd-home", runtime.NewStore(), system.NewProvider(), filesystem.NewProvider(), nil)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `data-artifactd-navigation`) {
		t.Fatal("artifact navigation was not injected")
	}
	if !strings.Contains(body, `href="http://artifactd-home.artifacts.localhost:7337/"`) {
		t.Fatal("artifact navigation did not link to the home artifact")
	}
	if strings.Count(body, `data-artifactd-navigation`) != 1 {
		t.Fatal("artifact navigation was injected more than once")
	}

	cssRecorder := httptest.NewRecorder()
	cssRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/navigation.css", nil)
	cssRequest.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(cssRecorder, cssRequest)
	if cssRecorder.Code != http.StatusOK || !strings.Contains(cssRecorder.Body.String(), ".artifactd-navigation") {
		t.Fatalf("navigation stylesheet response = %d %s", cssRecorder.Code, cssRecorder.Body.String())
	}
}

func TestServerServesEditRuntime(t *testing.T) {
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("closing test store: %v", err)
		}
	}()

	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	staging, err := store.NewStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyForWebTest(source, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishStaged(t.Context(), staging, ""); err != nil {
		t.Fatal(err)
	}

	editManager := edit.NewManager(store)
	session, err := editManager.Begin(t.Context(), source, "Updating dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer editManager.Close()
	server := NewServer(store, "artifacts.localhost", func(id string) string {
		return "http://" + id + ".artifacts.localhost:7337/"
	}, "artifactd-home", runtime.NewStore(), system.NewProvider(), filesystem.NewProvider(), editManager)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `data-artifactd-edit`) {
		t.Fatal("edit HTML did not contain the edit client")
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "data-artifactd-navigation") {
		t.Fatal("live HTML did not contain artifact navigation")
	}
	if !strings.Contains(body, "data-artifactd-edit-overlay") || !strings.Contains(body, "/_artifactd/edit.css") {
		t.Fatal("edit HTML did not contain the edit overlay")
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/edit", nil)
	statusRequest.Host = "demo.artifacts.localhost"
	statusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK || !strings.Contains(statusRecorder.Body.String(), session.SessionID) {
		t.Fatalf("edit status response = %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	cssRecorder := httptest.NewRecorder()
	cssRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/edit.css", nil)
	cssRequest.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(cssRecorder, cssRequest)
	if cssRecorder.Code != http.StatusOK || !strings.Contains(cssRecorder.Body.String(), "artifactd-editing") {
		t.Fatalf("edit stylesheet response = %d %s", cssRecorder.Code, cssRecorder.Body.String())
	}

	clientRecorder := httptest.NewRecorder()
	clientRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/edit.js", nil)
	clientRequest.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(clientRecorder, clientRequest)
	if clientRecorder.Code != http.StatusOK || !strings.Contains(clientRecorder.Body.String(), "EventSource") {
		t.Fatalf("edit client response = %d %s", clientRecorder.Code, clientRecorder.Body.String())
	}
}

func TestServerRendersMarkdownArtifact(t *testing.T) {
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Logf("closing test store: %v", closeErr)
		}
	}()
	publishMarkdownArtifact(t, store)

	server := newTestServer(store)
	entryBody := requestMarkdown(t, server, "/").Body.String()
	requestMarkdown(t, server, "/docs/more.md")
	if strings.Contains(entryBody, "<script>alert('xss')</script>") {
		t.Fatal("raw Markdown HTML was served")
	}
	for _, wanted := range []string{`<h1 id="notes">Notes</h1>`, "data-artifactd-edit-overlay", "data-artifactd-navigation"} {
		if !strings.Contains(entryBody, wanted) {
			t.Fatalf("rendered Markdown does not contain %q", wanted)
		}
	}

	headRecorder := requestArtifact(t, server, http.MethodHead, "/")
	if headRecorder.Body.Len() != 0 {
		t.Fatalf("HEAD response has %d body bytes", headRecorder.Body.Len())
	}
}

func TestServerServesTemporaryMarkdownPreview(t *testing.T) {
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	previewManager := preview.NewManager()
	document, err := previewManager.Create("notes.md", []byte("# Temporary"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(
		store,
		"artifacts.localhost",
		func(id string) string { return "http://" + id + ".artifacts.localhost:7337/" },
		"artifactd-home",
		runtime.NewStore(),
		system.NewProvider(),
		filesystem.NewProvider(),
		nil,
	)
	server.SetPreviewManager(previewManager)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = document.ID + ".artifacts.localhost"
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "<h1") {
		t.Fatalf("preview response = %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "data-artifactd-edit-overlay") {
		t.Fatal("temporary preview contained artifact editing controls")
	}
	if !strings.Contains(recorder.Body.String(), "data-artifactd-navigation") {
		t.Fatal("temporary preview did not contain library navigation")
	}
	if got := recorder.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("preview Referrer-Policy = %q", got)
	}

	cssRecorder := httptest.NewRecorder()
	cssRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/markdown.css", nil)
	cssRequest.Host = document.ID + ".artifacts.localhost"
	server.Handler().ServeHTTP(cssRecorder, cssRequest)
	if cssRecorder.Code != http.StatusOK || !strings.Contains(cssRecorder.Body.String(), ".artifactd-markdown") {
		t.Fatalf("Markdown stylesheet response = %d %s", cssRecorder.Code, cssRecorder.Body.String())
	}
}

func TestEditableMarkdownPreviewSaveAndDOCXExport(t *testing.T) {
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("# Before"), 0o640); err != nil {
		t.Fatal(err)
	}
	manager := preview.NewManager()
	document, err := manager.CreateEditable("notes.md", path, []byte("# Before"))
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(store)
	server.SetPreviewManager(manager)
	host := document.ID + ".artifacts.localhost:7337"

	page := httptest.NewRecorder()
	pageRequest := httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil)
	pageRequest.Host = host
	server.Handler().ServeHTTP(page, pageRequest)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Export PDF") || !strings.Contains(page.Body.String(), "Export DOCX") || !strings.Contains(page.Body.String(), "Markdown source") {
		t.Fatalf("editable preview = %d %s", page.Code, page.Body.String())
	}
	if strings.Index(page.Body.String(), "artifactd-md-shell") > strings.Index(page.Body.String(), "<main id=\"artifactd-md-document\"") {
		t.Fatal("Markdown controls should appear before the document")
	}
	for _, asset := range []string{"markdown-editor.css", "markdown-editor.js"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://"+host+"/_artifactd/"+asset, nil)
		request.Host = host
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
			t.Fatalf("editor asset %s = %d", asset, recorder.Code)
		}
	}

	body, err := json.Marshal(map[string]string{"content": "# After"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := httptest.NewRecorder()
	blockedRequest := httptest.NewRequest(http.MethodPost, "http://"+host+"/_artifactd/save", bytes.NewReader(body))
	blockedRequest.Host = host
	blockedRequest.Header.Set("Origin", "http://attacker.invalid")
	blockedRequest.Header.Set("Authorization", "Bearer "+document.SaveToken)
	server.Handler().ServeHTTP(blocked, blockedRequest)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cross-origin save status = %d, want 403", blocked.Code)
	}

	save := httptest.NewRecorder()
	saveRequest := httptest.NewRequest(http.MethodPost, "http://"+host+"/_artifactd/save", bytes.NewReader(body))
	saveRequest.Host = host
	saveRequest.Header.Set("Origin", "http://"+host)
	saveRequest.Header.Set("Authorization", "Bearer "+document.SaveToken)
	saveRequest.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(save, saveRequest)
	if save.Code != http.StatusOK {
		t.Fatalf("save response = %d %s", save.Code, save.Body.String())
	}
	updated, err := os.ReadFile(path)
	if err != nil || string(updated) != "# After" {
		t.Fatalf("saved Markdown = %q, %v", updated, err)
	}

	export := httptest.NewRecorder()
	exportRequest := httptest.NewRequest(http.MethodGet, "http://"+host+"/_artifactd/document.docx", nil)
	exportRequest.Host = host
	server.Handler().ServeHTTP(export, exportRequest)
	if export.Code != http.StatusOK || !strings.Contains(export.Header().Get("Content-Type"), "wordprocessingml.document") || export.Body.Len() == 0 {
		t.Fatalf("DOCX export = %d %q, %d bytes", export.Code, export.Header().Get("Content-Type"), export.Body.Len())
	}
}

func TestPreviewEditorEscapesDocumentMetadata(t *testing.T) {
	t.Parallel()
	document := preview.Document{
		Name:      `notes-"<draft>.md`,
		SaveToken: "test-capability",
		Content:   []byte("</textarea><script>alert(1)</script>"),
	}
	page := []byte(`<html><body><main class="artifactd-markdown">Document</main></body></html>`)
	result := string(injectPreviewEditor(page, document, "http://artifacts.localhost/?a=1&b=2", "test-nonce"))
	for _, want := range []string{
		`notes-&#34;&lt;draft&gt;.md`,
		`&lt;/textarea&gt;&lt;script&gt;alert(1)&lt;/script&gt;`,
		`http://artifacts.localhost/?a=1&amp;b=2`,
		`<details class="artifactd-md-export">`,
		`aria-controls="artifactd-md-document artifactd-md-source"`,
		`<main id="artifactd-md-document"`,
		`<label id="artifactd-md-source-label" for="artifactd-md-source">`,
		`data-style-nonce="test-nonce"`,
		`data-action="toggle-view"`,
		`aria-label="Edit document"`,
		`class="artifactd-md-workspace"`,
	} {
		if !strings.Contains(result, want) {
			t.Errorf("editor page missing %q", want)
		}
	}
	if strings.Contains(result, `aria-label="Markdown formatting"`) {
		t.Fatal("editor page contains the removed formatting toolbar")
	}
	if strings.Count(result, "</textarea>") != 1 || strings.Contains(result, "<script>alert") {
		t.Fatal("Markdown source escaped its textarea")
	}
}

func TestServerRejectsUnknownArtifact(t *testing.T) {
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("closing test store: %v", err)
		}
	}()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	newTestServer(store).Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestNormalizedRequestPathRejectsTraversal(t *testing.T) {
	for _, requestPath := range []string{"/demo/../secret", "/demo/./index.html", "/demo//index.html"} {
		if _, err := normalizedRequestPath(requestPath); err == nil {
			t.Errorf("normalizedRequestPath(%q) accepted unsafe path", requestPath)
		}
	}
	if got, err := normalizedRequestPath("/demo/assets/app.js"); err != nil || got != "demo/assets/app.js" {
		t.Fatalf("normalizedRequestPath() = %q, %v", got, err)
	}
}

func TestServerRejectsUntrustedHost(t *testing.T) {
	store, err := OpenTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	request := httptest.NewRequest(http.MethodGet, "/demo/", nil)
	request.Host = "untrusted.example"
	recorder := httptest.NewRecorder()
	newTestServer(store).Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func requestMarkdown(t *testing.T, server *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := requestArtifact(t, server, http.MethodGet, path)
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("GET %s Content-Type = %q", path, got)
	}
	if !strings.Contains(recorder.Body.String(), "/_artifactd/markdown.css") {
		t.Fatalf("GET %s did not use the Markdown stylesheet", path)
	}
	return recorder
}

func requestArtifact(t *testing.T, server *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	request.Host = "notes.artifacts.localhost"
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d", method, path, recorder.Code)
	}
	return recorder
}

func publishMarkdownArtifact(t *testing.T, store *storage.Store) {
	t.Helper()
	source := t.TempDir()
	manifest := `{"specVersion":1,"artifact":{"id":"notes","name":"Notes"},"code":{"format":"files","entry":"README.md"},"runtime":{"id":"web-static","version":1}}`
	if err := os.WriteFile(filepath.Join(source, "artifact.json"), []byte(manifest), 0o640); err != nil {
		t.Fatal(err)
	}
	markdown := "# Notes\n\n- [x] rendered\n\n<script>alert('xss')</script>"
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte(markdown), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "docs", "more.md"), []byte("## More"), 0o640); err != nil {
		t.Fatal(err)
	}
	staging, err := store.NewStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyForWebTest(source, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishStaged(t.Context(), staging, ""); err != nil {
		t.Fatal(err)
	}
}

func newTestServer(store *storage.Store) *Server {
	return NewServer(
		store,
		"artifacts.localhost",
		func(id string) string { return "http://" + id + ".artifacts.localhost:7337/" },
		"demo",
		runtime.NewStore(),
		system.NewProvider(),
		filesystem.NewProvider(),
		nil,
	)
}

func OpenTestStore(t *testing.T) (*storage.Store, error) {
	t.Helper()
	return storage.Open(t.TempDir())
}

func copyForWebTest(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o640)
	})
}
