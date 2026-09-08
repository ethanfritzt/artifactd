package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	"artifactd/internal/edit"
	"artifactd/internal/preview"
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
	editManager := edit.NewManager(store)
	defer editManager.Close()
	server := NewServer(store, func(id string) string { return "http://" + id + ".artifacts.localhost:7337/" }, runtime.NewStore(), editManager)

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

	beginBody, err := json.Marshal(protocol.EditBeginRequest{Directory: source, Message: "Updating demo"})
	if err != nil {
		t.Fatal(err)
	}
	beginRequest := httptest.NewRequest(http.MethodPost, "/v1/artifacts/demo/edit", bytes.NewReader(beginBody))
	beginRequest.Header.Set("Content-Type", "application/json")
	beginRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(beginRecorder, beginRequest)
	if beginRecorder.Code != http.StatusCreated {
		t.Fatalf("begin status = %d, body = %s", beginRecorder.Code, beginRecorder.Body.String())
	}
	var session protocol.EditSessionResponse
	if err := json.NewDecoder(beginRecorder.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.ArtifactID != "demo" || session.Status != "editing" {
		t.Fatalf("edit response = %+v", session)
	}

	commitBody, commitType := multipartArtifactWithEdit(t, source, session.SessionID, session.BaseVersion)
	commitRequest := httptest.NewRequest(http.MethodPost, "/v1/artifacts/demo/edit/commit", commitBody)
	commitRequest.Header.Set("Content-Type", commitType)
	commitRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(commitRecorder, commitRequest)
	if commitRecorder.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, body = %s", commitRecorder.Code, commitRecorder.Body.String())
	}
	var commitResponse protocol.PublishResponse
	if err := json.NewDecoder(commitRecorder.Body).Decode(&commitResponse); err != nil {
		t.Fatal(err)
	}
	if commitResponse.Version != 2 {
		t.Fatalf("commit response = %+v", commitResponse)
	}
}

func TestPreviewHandler(t *testing.T) {
	previewManager := preview.NewManager()
	server := NewServer(
		nil,
		func(id string) string { return "http://" + id + ".artifacts.localhost:7337/" },
		nil,
		nil,
	)
	server.SetPreviewManager(previewManager)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("# Preview")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/previews", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response protocol.PreviewResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ID == "" || response.Name != "README.md" || response.URL == "" {
		t.Fatalf("response = %+v", response)
	}
	document, err := previewManager.Get(response.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(document.Content) != "# Preview" {
		t.Fatalf("preview content = %q", document.Content)
	}
}

func TestPublishWorkspaceAssociationRequiresMatchingSource(t *testing.T) {
	t.Run("spoofed source path is not trusted", func(t *testing.T) {
		root := t.TempDir()
		store, err := storage.Open(filepath.Join(root, "data"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = store.Close() }()

		workspaceRoot := filepath.Join(root, "workspace")
		if err := os.Mkdir(workspaceRoot, 0o750); err != nil {
			t.Fatal(err)
		}
		claimedSource := filepath.Join(workspaceRoot, "claimed")
		if err := create.Run(create.Options{Directory: claimedSource, ID: "demo", Name: "Claimed"}); err != nil {
			t.Fatal(err)
		}
		workspace, err := store.AddWorkspace(t.Context(), "trusted", workspaceRoot)
		if err != nil {
			t.Fatal(err)
		}
		uploadedSource := filepath.Join(root, "uploaded")
		if err := create.Run(create.Options{Directory: uploadedSource, ID: "demo", Name: "Uploaded"}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(uploadedSource, "app.js"), []byte("spoofed package"), 0o640); err != nil {
			t.Fatal(err)
		}

		body, contentType := multipartArtifactWithSource(t, uploadedSource, claimedSource)
		request := httptest.NewRequest(http.MethodPost, "/v1/artifacts/publish", body)
		request.Header.Set("Content-Type", contentType)
		recorder := httptest.NewRecorder()
		server := NewServer(store, func(id string) string { return "http://" + id }, nil, nil)
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		_, version, err := store.Current(t.Context(), "demo")
		if err != nil {
			t.Fatal(err)
		}
		if version.WorkspaceID != "" {
			t.Fatalf("spoofed source path associated workspace %q (workspace %q)", version.WorkspaceID, workspace.ID)
		}
	})

	t.Run("matching source path is trusted", func(t *testing.T) {
		root := t.TempDir()
		store, err := storage.Open(filepath.Join(root, "data"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = store.Close() }()

		workspaceRoot := filepath.Join(root, "workspace")
		if err := os.Mkdir(workspaceRoot, 0o750); err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(workspaceRoot, "demo")
		if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
			t.Fatal(err)
		}
		workspace, err := store.AddWorkspace(t.Context(), "trusted", workspaceRoot)
		if err != nil {
			t.Fatal(err)
		}
		body, contentType := multipartArtifactWithSource(t, source, source)
		request := httptest.NewRequest(http.MethodPost, "/v1/artifacts/publish", body)
		request.Header.Set("Content-Type", contentType)
		recorder := httptest.NewRecorder()
		server := NewServer(store, func(id string) string { return "http://" + id }, nil, nil)
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		_, version, err := store.Current(t.Context(), "demo")
		if err != nil {
			t.Fatal(err)
		}
		if version.WorkspaceID != workspace.ID {
			t.Fatalf("matching source path workspace = %q, want %q", version.WorkspaceID, workspace.ID)
		}
	})
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
	editManager := edit.NewManager(store)
	defer editManager.Close()
	server := NewServer(store, func(id string) string { return "http://" + id + ".artifacts.localhost:7337/" }, runtime.NewStore(), editManager)
	publishRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(publishRecorder, publishRequest)
	if publishRecorder.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, body = %s", publishRecorder.Code, publishRecorder.Body.String())
	}

	beginBody, err := json.Marshal(protocol.EditBeginRequest{Directory: source, Message: "Updating demo"})
	if err != nil {
		t.Fatal(err)
	}
	beginRequest := httptest.NewRequest(http.MethodPost, "/v1/artifacts/demo/edit", bytes.NewReader(beginBody))
	beginRequest.Header.Set("Content-Type", "application/json")
	beginRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(beginRecorder, beginRequest)
	if beginRecorder.Code != http.StatusCreated {
		t.Fatalf("begin status = %d, body = %s", beginRecorder.Code, beginRecorder.Body.String())
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

	editRequest := httptest.NewRequest(http.MethodGet, "/v1/artifacts/demo/edit", nil)
	editRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(editRecorder, editRequest)
	if editRecorder.Code != http.StatusNotFound {
		t.Fatalf("edit status after archive = %d, want 404", editRecorder.Code)
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

func multipartArtifactWithEdit(t *testing.T, source, sessionID string, baseVersion int) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("session_id", sessionID); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("base_version", fmt.Sprint(baseVersion)); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("source_path", source); err != nil {
		t.Fatal(err)
	}
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
			"name": "file", "filename": filepath.Base(relative),
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

func multipartArtifact(t *testing.T, source string) (io.Reader, string) {
	return multipartArtifactWithSource(t, source, "")
}

func multipartArtifactWithSource(t *testing.T, source, sourcePath string) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if sourcePath != "" {
		if err := writer.WriteField("source_path", sourcePath); err != nil {
			t.Fatal(err)
		}
	}
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
