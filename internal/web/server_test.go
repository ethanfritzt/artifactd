package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"artifactd/internal/create"
	"artifactd/internal/live"
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
	server := newTestServer(store)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Your artifact is ready.") {
		t.Fatal("response did not contain artifact content")
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

func TestServerServesLiveSnapshot(t *testing.T) {
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

	liveManager := live.NewManager(store)
	if _, err := liveManager.Start(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	defer liveManager.Close()
	server := NewServer(store, "artifacts.localhost", func(id string) string {
		return "http://" + id + ".artifacts.localhost:7337/"
	}, "artifactd-home", runtime.NewStore(), system.NewProvider(), filesystem.NewProvider(), liveManager)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "data-artifactd-live") {
		t.Fatal("live HTML did not contain the refresh client")
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "data-artifactd-navigation") {
		t.Fatal("live HTML did not contain artifact navigation")
	}
	if !strings.Contains(body, "data-artifactd-live-overlay") || !strings.Contains(body, "/_artifactd/live.css") {
		t.Fatal("live HTML did not contain the build overlay")
	}

	cssRecorder := httptest.NewRecorder()
	cssRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/live.css", nil)
	cssRequest.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(cssRecorder, cssRequest)
	if cssRecorder.Code != http.StatusOK || !strings.Contains(cssRecorder.Body.String(), "artifactd-building") {
		t.Fatalf("live stylesheet response = %d %s", cssRecorder.Code, cssRecorder.Body.String())
	}

	clientRecorder := httptest.NewRecorder()
	clientRequest := httptest.NewRequest(http.MethodGet, "/_artifactd/live.js", nil)
	clientRequest.Host = "demo.artifacts.localhost"
	server.Handler().ServeHTTP(clientRecorder, clientRequest)
	if clientRecorder.Code != http.StatusOK || !strings.Contains(clientRecorder.Body.String(), "EventSource") {
		t.Fatalf("live client response = %d %s", clientRecorder.Code, clientRecorder.Body.String())
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

func newTestServer(store *storage.Store) *Server {
	return NewServer(store, "artifacts.localhost", func(id string) string { return "http://" + id + ".artifacts.localhost:7337/" }, "demo", runtime.NewStore(), system.NewProvider(), filesystem.NewProvider(), nil)
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
