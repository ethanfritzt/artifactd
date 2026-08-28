package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"artifactd/internal/create"
	"artifactd/internal/storage"
)

func TestServerServesWorkspaceRuntimeData(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Logf("closing test store: %v", closeErr)
		}
	}()
	if _, err := store.AddWorkspace(t.Context(), "vault", root); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "artifact")
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
	if _, err := store.PublishStaged(t.Context(), staging, source); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/_artifactd/files?path=artifact&depth=1", nil)
	request.Host = "demo.artifacts.localhost"
	recorder := httptest.NewRecorder()
	newTestServer(store).Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "index.html") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal(err)
	}
}
