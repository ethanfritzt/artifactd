package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"artifactd/internal/edit"
	"artifactd/internal/manifest"
	"artifactd/internal/model"
	"artifactd/internal/preview"
	"artifactd/internal/protocol"
	"artifactd/internal/registry"
	"artifactd/internal/runtime"
	"artifactd/internal/storage"
)

const (
	maxPublishBody = 50 << 20
	maxPreviewBody = preview.MaxBytes + (1 << 20)
	maxDataBody    = 5 << 20
	maxSourcePath  = 4096
)

type RequestError struct {
	Message string
}

func (e *RequestError) Error() string {
	return e.Message
}

type Server struct {
	store     *storage.Store
	publicURL func(string) string
	data      *runtime.Store
	edit      *edit.Manager
	previews  *preview.Manager
}

func NewServer(store *storage.Store, publicURL func(string) string, data *runtime.Store, editManager *edit.Manager) *Server {
	return &Server{store: store, publicURL: publicURL, data: data, edit: editManager}
}

// SetPreviewManager enables temporary previews before the server starts.
func (s *Server) SetPreviewManager(manager *preview.Manager) {
	s.previews = manager
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.health)
	mux.HandleFunc("/v1/previews", s.createPreview)
	mux.HandleFunc("/v1/artifacts", s.list)
	mux.HandleFunc("/v1/artifacts/publish", s.publish)
	mux.HandleFunc("/v1/artifacts/", s.artifactRoute)
	mux.HandleFunc("/v1/workspaces", s.workspaces)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, protocol.HealthResponse{Status: "ok"})
}

func (s *Server) createPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.previews == nil {
		writeError(w, http.StatusNotImplemented, "previews are unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPreviewBody)
	name, sourcePath, content, err := receivePreviewMultipart(r)
	if err != nil {
		writePreviewFailure(w, err)
		return
	}
	var document preview.Document
	if sourcePath != "" {
		document, err = s.previews.CreateEditable(name, sourcePath, content)
	} else {
		document, err = s.previews.Create(name, content)
	}
	if err != nil {
		writePreviewFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, protocol.PreviewResponse{
		ID:        document.ID,
		Name:      document.Name,
		URL:       s.publicURL(document.ID),
		ExpiresAt: document.ExpiresAt,
		Editable:  document.SaveToken != "",
	})
}

func receivePreviewMultipart(r *http.Request) (string, string, []byte, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return "", "", nil, &RequestError{Message: fmt.Sprintf("reading preview form: %v", err)}
	}
	var name, sourcePath string
	var content []byte
	var sourcePathSeen bool
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", "", nil, &RequestError{Message: fmt.Sprintf("reading preview form: %v", err)}
		}
		switch part.FormName() {
		case "source_path":
			if sourcePathSeen {
				_ = part.Close()
				return "", "", nil, &RequestError{Message: "preview accepts one source path"}
			}
			sourcePathSeen = true
			value, readErr := io.ReadAll(io.LimitReader(part, maxSourcePath+1))
			closeErr := part.Close()
			if readErr != nil || closeErr != nil {
				return "", "", nil, errors.Join(readErr, closeErr)
			}
			if len(value) == 0 || len(value) > maxSourcePath || !filepath.IsAbs(string(value)) || filepath.Clean(string(value)) != string(value) {
				return "", "", nil, &RequestError{Message: "invalid Markdown source path"}
			}
			sourcePath = string(value)
		case "file":
			if name != "" || part.FileName() == "" {
				_ = part.Close()
				return "", "", nil, &RequestError{Message: "preview accepts exactly one file"}
			}
			name = part.FileName()
			if err := preview.ValidateName(name); err != nil {
				_ = part.Close()
				return "", "", nil, &RequestError{Message: err.Error()}
			}
			content, err = io.ReadAll(io.LimitReader(part, preview.MaxBytes+1))
			closeErr := part.Close()
			if err != nil || closeErr != nil {
				return "", "", nil, errors.Join(err, closeErr)
			}
			if len(content) > preview.MaxBytes {
				return "", "", nil, preview.ErrTooLarge
			}
		default:
			_ = part.Close()
			return "", "", nil, &RequestError{Message: "unexpected preview form field"}
		}
	}
	if name == "" {
		return "", "", nil, &RequestError{Message: "preview file is required"}
	}
	return name, sourcePath, content, nil
}

func writePreviewFailure(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.Is(err, preview.ErrTooLarge) || errors.As(err, &maxErr) {
		writeError(w, http.StatusRequestEntityTooLarge, preview.ErrTooLarge.Error())
		return
	}
	var requestErr *RequestError
	var validationErr *preview.ValidationError
	if errors.Is(err, preview.ErrConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.As(err, &requestErr) || errors.As(err, &validationErr) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeInternalError(w, err)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	includeArchived := false
	if raw := r.URL.Query().Get("include_archived"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "include_archived must be a boolean")
			return
		}
		includeArchived = parsed
	}
	artifacts, err := s.store.List(r.Context(), includeArchived)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.ListResponse{Artifacts: artifacts})
}

func (s *Server) artifactRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/artifacts/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "invalid artifact ID")
		return
	}
	if err := protocol.ValidateArtifactID(parts[0]); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch {
	case len(parts) == 3 && parts[1] == "data":
		s.pushData(w, r, parts[0], parts[2])
	case len(parts) == 2 && parts[1] == "edit":
		s.editRoute(w, r, parts[0])
	case len(parts) == 3 && parts[1] == "edit" && parts[2] == "progress":
		s.editProgress(w, r, parts[0])
	case len(parts) == 3 && parts[1] == "edit" && parts[2] == "commit":
		s.editCommit(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "versions":
		s.versions(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "restore":
		s.restore(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "archive":
		s.archive(w, r, parts[0])
	default:
		s.get(w, r)
	}
}

func (s *Server) editRoute(w http.ResponseWriter, r *http.Request, artifactID string) {
	if s.edit == nil {
		writeError(w, http.StatusNotImplemented, "editing is unavailable")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var request protocol.EditBeginRequest
		if err := decodeJSON(r, w, 16<<10, &request); err != nil || protocol.ValidateAbsolutePath(request.Directory) != nil {
			writeError(w, http.StatusBadRequest, "invalid edit request")
			return
		}
		session, err := s.edit.Begin(r.Context(), request.Directory, request.Message)
		if err != nil {
			status := statusForEditError(err)
			writeError(w, status, userError(err))
			return
		}
		if session.ArtifactID != artifactID {
			_ = s.edit.Abort(session.ArtifactID, session.SessionID)
			writeError(w, http.StatusBadRequest, "edit directory artifact ID does not match request")
			return
		}
		writeJSON(w, http.StatusCreated, editResponse(session, s.publicURL(session.ArtifactID)))
	case http.MethodGet:
		session, err := s.edit.Status(artifactID)
		if errors.Is(err, edit.ErrNoEdit) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, editResponse(session, s.publicURL(session.ArtifactID)))
	case http.MethodDelete:
		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "session_id is required")
			return
		}
		if err := s.edit.Abort(artifactID, sessionID); err != nil {
			status := statusForEditError(err)
			writeError(w, status, userError(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) editProgress(w http.ResponseWriter, r *http.Request, artifactID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.edit == nil {
		writeError(w, http.StatusNotImplemented, "editing is unavailable")
		return
	}
	var request protocol.EditProgressRequest
	if err := decodeJSON(r, w, 16<<10, &request); err != nil || request.SessionID == "" {
		writeError(w, http.StatusBadRequest, "invalid edit progress request")
		return
	}
	session, err := s.edit.Progress(artifactID, request.SessionID, request.Message)
	if err != nil {
		writeError(w, statusForEditError(err), userError(err))
		return
	}
	writeJSON(w, http.StatusOK, editResponse(session, s.publicURL(session.ArtifactID)))
}

func (s *Server) editCommit(w http.ResponseWriter, r *http.Request, artifactID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.edit == nil {
		writeError(w, http.StatusNotImplemented, "editing is unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublishBody)
	staging, err := s.store.NewStaging()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer func() {
		if cleanupErr := s.store.RemoveStaging(staging); cleanupErr != nil {
			slog.Error("removing edit staging directory", "error", cleanupErr)
		}
	}()
	sessionID, baseVersion, sourcePath, err := receiveEditMultipart(r, staging)
	if err != nil {
		writeError(w, statusForError(err), userError(err))
		return
	}
	var result registry.PublishResult
	err = s.edit.Commit(r.Context(), artifactID, sessionID, sourcePath, baseVersion, func() error {
		var publishErr error
		result, publishErr = s.store.PublishStagedIfCurrent(r.Context(), staging, sourcePath, baseVersion)
		return publishErr
	})
	if err != nil {
		writeError(w, statusForEditError(err), userError(err))
		return
	}
	writeJSON(w, http.StatusCreated, protocol.PublishResponse{
		Artifact: result.Artifact,
		Version:  result.Version.Number,
		URL:      s.publicURL(result.Artifact.ID),
	})
}

func editResponse(session edit.Session, url string) protocol.EditSessionResponse {
	return protocol.EditSessionResponse{
		SessionID:   session.SessionID,
		ArtifactID:  session.ArtifactID,
		Directory:   session.Directory,
		BaseVersion: session.BaseVersion,
		Status:      string(session.Status),
		Message:     session.Message,
		Error:       session.Error,
		ExpiresAt:   session.ExpiresAt,
		URL:         url,
	}
}

func statusForEditError(err error) int {
	switch {
	case errors.Is(err, edit.ErrAlreadyEditing), errors.Is(err, edit.ErrVersionChanged):
		return http.StatusConflict
	case errors.Is(err, edit.ErrNoEdit), errors.Is(err, edit.ErrSessionNotFound):
		return http.StatusNotFound
	case errors.Is(err, edit.ErrTooManySessions), errors.Is(err, edit.ErrTooManyClients):
		return http.StatusTooManyRequests
	case errors.Is(err, edit.ErrArchivedArtifact), errors.Is(err, registry.ErrVersionConflict):
		return http.StatusConflict
	default:
		return statusForError(err)
	}
}

func (s *Server) archive(w http.ResponseWriter, r *http.Request, artifactID string) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if artifactID == model.SystemArtifactID {
		writeError(w, http.StatusBadRequest, "system artifact cannot be archived")
		return
	}
	if r.Method == http.MethodPost {
		if _, _, err := s.store.Current(r.Context(), artifactID); err != nil {
			if errors.Is(err, registry.ErrNotFound) {
				writeError(w, http.StatusNotFound, "artifact not found")
				return
			}
			writeInternalError(w, err)
			return
		}
		if s.edit != nil {
			if err := s.edit.AbortArtifact(artifactID, "Artifact archived"); err != nil && !errors.Is(err, edit.ErrNoEdit) {
				writeInternalError(w, err)
				return
			}
		}
		if err := s.store.Archive(r.Context(), artifactID); err != nil {
			if errors.Is(err, registry.ErrNotFound) {
				writeError(w, http.StatusNotFound, "artifact not found")
				return
			}
			writeInternalError(w, err)
			return
		}
	} else if err := s.store.Unarchive(r.Context(), artifactID); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "artifact not found")
			return
		}
		writeInternalError(w, err)
		return
	}
	artifact, version, err := s.store.Current(r.Context(), artifactID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.ArtifactResponse{
		Artifact: artifact,
		Version:  version.Number,
		Entry:    version.Entry,
	})
}

func (s *Server) versions(w http.ResponseWriter, r *http.Request, artifactID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	versions, err := s.store.ListVersions(r.Context(), artifactID)
	if errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.VersionsResponse{Versions: versions})
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request, artifactID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request protocol.RestoreRequest
	if err := decodeJSON(r, w, 16<<10, &request); err != nil || request.Version < 1 {
		writeError(w, http.StatusBadRequest, "invalid restore request")
		return
	}
	if s.edit != nil {
		if err := s.edit.AbortArtifact(artifactID, "Edit canceled for restore"); err != nil && !errors.Is(err, edit.ErrNoEdit) {
			writeInternalError(w, err)
			return
		}
	}
	if err := s.store.Restore(r.Context(), artifactID, request.Version); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, registry.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	artifact, version, err := s.store.Current(r.Context(), artifactID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.ArtifactResponse{
		Artifact: artifact,
		Version:  version.Number,
		Entry:    version.Entry,
	})
}

func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		workspaces, err := s.store.ListWorkspaces(r.Context())
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, protocol.WorkspacesResponse{Workspaces: workspaces})
	case http.MethodPost:
		var request protocol.WorkspaceRequest
		if err := decodeJSON(r, w, 16<<10, &request); err != nil || protocol.ValidateArtifactID(request.ID) != nil || protocol.ValidateAbsolutePath(request.Root) != nil {
			writeError(w, http.StatusBadRequest, "invalid workspace request")
			return
		}
		workspace, err := s.store.AddWorkspace(r.Context(), request.ID, request.Root)
		if err != nil {
			writeError(w, statusForError(err), userError(err))
			return
		}
		writeJSON(w, http.StatusCreated, protocol.WorkspaceResponse{Workspace: workspace})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) pushData(w http.ResponseWriter, r *http.Request, artifactID, source string) {
	if err := protocol.ValidateDataSource(source); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.data == nil {
		writeError(w, http.StatusNotImplemented, "runtime data is unavailable")
		return
	}
	if _, _, err := s.store.Current(r.Context(), artifactID); errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	} else if err != nil {
		writeInternalError(w, err)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDataBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "reading runtime data failed")
		return
	}
	if len(body) > maxDataBody {
		writeError(w, http.StatusRequestEntityTooLarge, "runtime data is too large")
		return
	}
	response, err := s.data.Put(artifactID, source, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/")
	if protocol.ValidateArtifactID(id) != nil {
		writeError(w, http.StatusBadRequest, "invalid artifact ID")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	artifact, version, err := s.store.Current(r.Context(), id)
	if errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.ArtifactResponse{
		Artifact: artifact,
		Version:  version.Number,
		Entry:    version.Entry,
	})
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublishBody)
	staging, err := s.store.NewStaging()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer func() {
		if cleanupErr := s.store.RemoveStaging(staging); cleanupErr != nil {
			slog.Error("removing publish staging directory", "error", cleanupErr)
		}
	}()

	sourcePath, err := receiveMultipart(r, staging)
	if err != nil {
		writeRequestFailure(w, err)
		return
	}
	result, err := s.store.PublishStaged(r.Context(), staging, sourcePath)
	if err != nil {
		writeRequestFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, protocol.PublishResponse{
		Artifact: result.Artifact,
		Version:  result.Version.Number,
		URL:      s.publicURL(result.Artifact.ID),
	})
}

func receiveEditMultipart(r *http.Request, staging string) (string, int, string, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return "", 0, "", &RequestError{Message: fmt.Sprintf("reading edit form: %v", err)}
	}
	seen := make(map[string]struct{})
	var sessionID, sourcePath, baseVersionValue string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, "", &RequestError{Message: fmt.Sprintf("reading edit part: %v", err)}
		}
		switch part.FormName() {
		case "session_id", "base_version", "source_path":
			if part.FileName() != "" {
				return "", 0, "", &RequestError{Message: "invalid edit metadata field"}
			}
			value, readErr := io.ReadAll(io.LimitReader(part, maxSourcePath+1))
			closeErr := part.Close()
			if readErr != nil {
				return "", 0, "", &RequestError{Message: fmt.Sprintf("reading edit metadata: %v", readErr)}
			}
			if closeErr != nil {
				return "", 0, "", &RequestError{Message: fmt.Sprintf("closing edit metadata: %v", closeErr)}
			}
			if len(value) > maxSourcePath {
				return "", 0, "", &RequestError{Message: "edit metadata is too long"}
			}
			switch part.FormName() {
			case "session_id":
				if sessionID != "" {
					return "", 0, "", &RequestError{Message: "duplicate session_id field"}
				}
				sessionID = strings.TrimSpace(string(value))
			case "base_version":
				if baseVersionValue != "" {
					return "", 0, "", &RequestError{Message: "duplicate base_version field"}
				}
				baseVersionValue = strings.TrimSpace(string(value))
			case "source_path":
				if sourcePath != "" {
					return "", 0, "", &RequestError{Message: "duplicate source path field"}
				}
				sourcePath = strings.TrimSpace(string(value))
			}
		case "file":
			if err := receiveFilePart(part, staging, seen); err != nil {
				return "", 0, "", err
			}
		default:
			return "", 0, "", &RequestError{Message: "unexpected edit field"}
		}
	}
	if sessionID == "" || sourcePath == "" || baseVersionValue == "" {
		return "", 0, "", &RequestError{Message: "edit session metadata is required"}
	}
	if err := protocol.ValidateAbsolutePath(sourcePath); err != nil {
		return "", 0, "", &RequestError{Message: "source path must be absolute and normalized"}
	}
	baseVersion, err := strconv.Atoi(baseVersionValue)
	if err != nil || baseVersion < 1 {
		return "", 0, "", &RequestError{Message: "base version must be a positive integer"}
	}
	return sessionID, baseVersion, sourcePath, nil
}

func receiveFilePart(part *multipart.Part, staging string, seen map[string]struct{}) error {
	name := part.Header.Get("X-Artifact-Path")
	if name == "" {
		name = part.FileName()
	}
	name, nameErr := safeUploadName(name)
	if nameErr != nil {
		return &RequestError{Message: nameErr.Error()}
	}
	if _, exists := seen[name]; exists {
		return &RequestError{Message: fmt.Sprintf("duplicate artifact path: %s", name)}
	}
	seen[name] = struct{}{}
	destination := filepath.Join(staging, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("creating artifact directory: %w", err)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("creating staged file: %w", err)
	}
	_, copyErr := io.Copy(file, part)
	closeErr := file.Close()
	partCloseErr := part.Close()
	if copyErr != nil {
		return fmt.Errorf("writing staged file: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing staged file: %w", closeErr)
	}
	if partCloseErr != nil {
		return fmt.Errorf("closing upload part: %w", partCloseErr)
	}
	return nil
}

func receiveMultipart(r *http.Request, staging string) (string, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return "", &RequestError{Message: fmt.Sprintf("reading publish form: %v", err)}
	}
	seen := make(map[string]struct{})
	sourcePath := ""
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", &RequestError{Message: fmt.Sprintf("reading publish part: %v", err)}
		}
		switch part.FormName() {
		case "source_path":
			if sourcePath != "" || part.FileName() != "" {
				return "", &RequestError{Message: "invalid source path field"}
			}
			value, readErr := io.ReadAll(io.LimitReader(part, maxSourcePath+1))
			closeErr := part.Close()
			if readErr != nil {
				return "", &RequestError{Message: fmt.Sprintf("reading source path: %v", readErr)}
			}
			if closeErr != nil {
				return "", &RequestError{Message: fmt.Sprintf("closing source path: %v", closeErr)}
			}
			if len(value) > maxSourcePath {
				return "", &RequestError{Message: "source path is too long"}
			}
			sourcePath = strings.TrimSpace(string(value))
			if err := protocol.ValidateAbsolutePath(sourcePath); err != nil {
				return "", &RequestError{Message: "source path must be absolute and normalized"}
			}
		case "file":
			name := part.Header.Get("X-Artifact-Path")
			if name == "" {
				name = part.FileName()
			}
			name, nameErr := safeUploadName(name)
			if nameErr != nil {
				return "", &RequestError{Message: nameErr.Error()}
			}
			if _, exists := seen[name]; exists {
				return "", &RequestError{Message: fmt.Sprintf("duplicate artifact path: %s", name)}
			}
			seen[name] = struct{}{}
			destination := filepath.Join(staging, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
				return "", fmt.Errorf("creating artifact directory: %w", err)
			}
			file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
			if err != nil {
				return "", fmt.Errorf("creating staged file: %w", err)
			}
			_, copyErr := io.Copy(file, part)
			closeErr := file.Close()
			partCloseErr := part.Close()
			if copyErr != nil {
				return "", fmt.Errorf("writing staged file: %w", copyErr)
			}
			if closeErr != nil {
				return "", fmt.Errorf("closing staged file: %w", closeErr)
			}
			if partCloseErr != nil {
				return "", fmt.Errorf("closing upload part: %w", partCloseErr)
			}
		default:
			return "", &RequestError{Message: "unexpected publish field"}
		}
	}
	return sourcePath, nil
}

func safeUploadName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") {
		return "", errors.New("invalid artifact path")
	}
	name = filepath.ToSlash(name)
	if name == "" {
		return "", errors.New("invalid artifact path")
	}
	clean := pathClean(name)
	if clean == "." || clean == ".." || name != clean || filepath.IsAbs(filepath.FromSlash(name)) || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", errors.New("invalid artifact path")
	}
	return name, nil
}

func pathClean(name string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
}

func decodeJSON(r *http.Request, w http.ResponseWriter, limit int64, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request contains multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("writing JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, protocol.ErrorResponse{Error: message})
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func writeRequestFailure(w http.ResponseWriter, err error) {
	status := statusForError(err)
	if status >= http.StatusInternalServerError {
		writeInternalError(w, err)
		return
	}
	writeError(w, status, userError(err))
}

func statusForError(err error) int {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return http.StatusRequestEntityTooLarge
	}
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		return http.StatusBadRequest
	}
	var validationErr *manifest.ValidationError
	if errors.As(err, &validationErr) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func userError(err error) string {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return "publish request is too large"
	}
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		return requestErr.Message
	}
	var validationErr *manifest.ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Message
	}
	if errors.Is(err, registry.ErrVersionConflict) || errors.Is(err, edit.ErrVersionChanged) {
		return "artifact changed since edit began"
	}
	if errors.Is(err, edit.ErrNoEdit) {
		return "artifact is not being edited"
	}
	if errors.Is(err, edit.ErrSessionNotFound) {
		return "edit session not found"
	}
	if errors.Is(err, edit.ErrAlreadyEditing) {
		return "artifact is already being edited"
	}
	return "internal server error"
}
