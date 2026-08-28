package ipc

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

	"artifactd/internal/create"
	"artifactd/internal/live"
	"artifactd/internal/protocol"
	"artifactd/internal/runtime"
	"artifactd/internal/storage"
)

func TestPublishHandler(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Logf("closing test store: %v", closeErr)
		}
	}()

	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "assets", "chart.svg"), []byte("<svg></svg>"), 0o640); err != nil {
		t.Fatal(err)
	}
	body, contentType := multipartArtifact(t, source)
	request := httptest.NewRequest(http.MethodPost, "/v1/artifacts/publish", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	liveManager := live.NewManager(store)
	defer liveManager.Close()
	server := NewServer(store, func(id string) string { return "http://" + id + ".artifacts.localhost:7337/" }, runtime.NewStore(), liveManager)

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response protocol.PublishResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Artifact.ID != "demo" || response.Version != 1 {
		t.Fatalf("response = %+v", response)
	}
	_, version, err := store.Current(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(version.Path, "assets", "chart.svg")); err != nil {
		t.Fatalf("nested asset missing: %v", err)
	}

	dataRequest := httptest.NewRequest(http.MethodPost, "/v1/artifacts/demo/data/metrics", bytes.NewBufferString(`{"value":42}`))
	dataRequest.Header.Set("Content-Type", "application/json")
	dataRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(dataRecorder, dataRequest)
	if dataRecorder.Code != http.StatusCreated {
		t.Fatalf("data status = %d, body = %s", dataRecorder.Code, dataRecorder.Body.String())
	}

	watchBody, err := json.Marshal(protocol.WatchRequest{Directory: source})
	if err != nil {
		t.Fatal(err)
	}
	watchRequest := httptest.NewRequest(http.MethodPost, "/v1/artifacts/demo/watch", bytes.NewReader(watchBody))
	watchRequest.Header.Set("Content-Type", "application/json")
	watchRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(watchRecorder, watchRequest)
	if watchRecorder.Code != http.StatusCreated {
		t.Fatalf("watch status = %d, body = %s", watchRecorder.Code, watchRecorder.Body.String())
	}
	var watchResponse protocol.LiveResponse
	if err := json.NewDecoder(watchRecorder.Body).Decode(&watchResponse); err != nil {
		t.Fatal(err)
	}
	if watchResponse.ArtifactID != "demo" || watchResponse.Status != "ready" {
		t.Fatalf("watch response = %+v", watchResponse)
	}

	liveRequest := httptest.NewRequest(http.MethodGet, "/v1/artifacts/demo/live", nil)
	liveRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(liveRecorder, liveRequest)
	if liveRecorder.Code != http.StatusOK {
		t.Fatalf("live status = %d, body = %s", liveRecorder.Code, liveRecorder.Body.String())
	}

	unwatchRequest := httptest.NewRequest(http.MethodDelete, "/v1/artifacts/demo/watch", nil)
	unwatchRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(unwatchRecorder, unwatchRequest)
	if unwatchRecorder.Code != http.StatusNoContent {
		t.Fatalf("unwatch status = %d, body = %s", unwatchRecorder.Code, unwatchRecorder.Body.String())
	}
}

func TestArchiveHandler(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Logf("closing test store: %v", closeErr)
		}
	}()

	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	body, contentType := multipartArtifact(t, source)
	publishRequest := httptest.NewRequest(http.MethodPost, "/v1/artifacts/publish", body)
	publishRequest.Header.Set("Content-Type", contentType)
	liveManager := live.NewManager(store)
	defer liveManager.Close()
	server := NewServer(store, func(id string) string { return "http://" + id + ".artifacts.localhost:7337/" }, runtime.NewStore(), liveManager)
	publishRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(publishRecorder, publishRequest)
	if publishRecorder.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, body = %s", publishRecorder.Code, publishRecorder.Body.String())
	}

	watchBody, err := json.Marshal(protocol.WatchRequest{Directory: source})
	if err != nil {
		t.Fatal(err)
	}
	watchRequest := httptest.NewRequest(http.MethodPost, "/v1/artifacts/demo/watch", bytes.NewReader(watchBody))
	watchRequest.Header.Set("Content-Type", "application/json")
	watchRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(watchRecorder, watchRequest)
	if watchRecorder.Code != http.StatusCreated {
		t.Fatalf("watch status = %d, body = %s", watchRecorder.Code, watchRecorder.Body.String())
	}

	archiveRecorder := httptest.NewRecorder()
	archiveRequest := httptest.NewRequest(http.MethodPost, "/v1/artifacts/demo/archive", nil)
	server.Handler().ServeHTTP(archiveRecorder, archiveRequest)
	if archiveRecorder.Code != http.StatusOK {
		t.Fatalf("archive status = %d, body = %s", archiveRecorder.Code, archiveRecorder.Body.String())
	}
	var archiveResponse protocol.ArtifactResponse
	if err := json.NewDecoder(archiveRecorder.Body).Decode(&archiveResponse); err != nil {
		t.Fatal(err)
	}
	if archiveResponse.Artifact.ArchivedAt == nil {
		t.Fatal("archive response has no archive timestamp")
	}

	liveRequest := httptest.NewRequest(http.MethodGet, "/v1/artifacts/demo/live", nil)
	liveRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(liveRecorder, liveRequest)
	if liveRecorder.Code != http.StatusNotFound {
		t.Fatalf("live status after archive = %d, want 404", liveRecorder.Code)
	}

	listRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/v1/artifacts", nil))
	var listResponse protocol.ListResponse
	if err := json.NewDecoder(listRecorder.Body).Decode(&listResponse); err != nil {
		t.Fatal(err)
	}
	if len(listResponse.Artifacts) != 0 {
		t.Fatalf("active artifact list = %+v, want none", listResponse.Artifacts)
	}

	allRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(allRecorder, httptest.NewRequest(http.MethodGet, "/v1/artifacts?include_archived=true", nil))
	if err := json.NewDecoder(allRecorder.Body).Decode(&listResponse); err != nil {
		t.Fatal(err)
	}
	if len(listResponse.Artifacts) != 1 || listResponse.Artifacts[0].ArchivedAt == nil {
		t.Fatalf("all artifact list = %+v, want archived demo", listResponse.Artifacts)
	}

	unarchiveRecorder := httptest.NewRecorder()
	unarchiveRequest := httptest.NewRequest(http.MethodDelete, "/v1/artifacts/demo/archive", nil)
	server.Handler().ServeHTTP(unarchiveRecorder, unarchiveRequest)
	if unarchiveRecorder.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d, body = %s", unarchiveRecorder.Code, unarchiveRecorder.Body.String())
	}
	var unarchiveResponse protocol.ArtifactResponse
	if err := json.NewDecoder(unarchiveRecorder.Body).Decode(&unarchiveResponse); err != nil {
		t.Fatal(err)
	}
	if unarchiveResponse.Artifact.ArchivedAt != nil {
		t.Fatal("unarchive response still has archive timestamp")
	}
}

func multipartArtifact(t *testing.T, source string) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
			"name":     "file",
			"filename": filepath.Base(relative),
		}))
		header.Set("Content-Type", "application/octet-stream")
		header.Set("X-Artifact-Path", filepath.ToSlash(relative))
		part, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		content, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(part, content)
		closeErr := content.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}
