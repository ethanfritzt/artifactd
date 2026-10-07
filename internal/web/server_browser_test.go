//go:build browser

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMarkdownEditorBrowser is opt-in: npm ci and a Playwright Chromium install
// (or CHROME_PATH) are needed. Normal Go builds/tests never require Node.
func TestMarkdownEditorBrowser(t *testing.T) {
	fixtures := map[string]editablePreviewFixture{}
	configuration := map[string]map[string]string{}
	names := []string{"views", "formatting", "race", "save", "concurrent", "conflict", "mobile", "print", "large", "errors", "navigation"}
	for _, name := range names {
		source := "# Saved\n\nOriginal paragraph.\n\n- first item\n- second item"
		if name == "large" {
			source += "\n\n" + strings.Repeat("A paragraph with **Markdown** and a [link](https://example.com).\n\n", 16000)
		}
		fixture := newEditablePreviewFixture(t, source)
		fixtures[fixture.document.ID] = fixture
		configuration[name] = map[string]string{"id": fixture.document.ID, "path": fixture.path}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _, _ := strings.Cut(r.Host, ".")
		fixture, ok := fixtures[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fixture.server.Handler().ServeHTTP(w, r)
	}))
	defer server.Close()
	for _, fixture := range configuration {
		fixture["url"] = strings.Replace(server.URL, "127.0.0.1", fixture["id"]+".artifacts.localhost", 1)
	}
	data, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "node", "--test", "editor/editor.test.mjs")
	command.Env = append(os.Environ(), "ARTIFACTD_BROWSER_FIXTURES="+string(data))
	output, err := command.CombinedOutput()
	t.Logf("browser tests:\n%s", output)
	if err != nil {
		t.Fatalf("running Markdown editor browser tests: %v", err)
	}
}
