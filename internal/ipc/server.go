package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"artifactd/internal/live"
	"artifactd/internal/manifest"
	"artifactd/internal/model"
	"artifactd/internal/protocol"
	"artifactd/internal/registry"
	"artifactd/internal/runtime"
	"artifactd/internal/storage"
)

const (
	maxPublishBody = 50 << 20
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
	live      *live.Manager
}

func NewServer(store *storage.Store, publicURL func(string) string, data *runtime.Store, liveManager *live.Manager) *Server {
	return &Server{store: store, publicURL: publicURL, data: data, live: liveManager}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.health)
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
	case len(parts) == 2 && parts[1] == "watch":
		s.watch(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "live":
		s.liveInfo(w, r, parts[0])
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

func (s *Server) watch(w http.ResponseWriter, r *http.Request, artifactID string) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var request protocol.WatchRequest
		if err := decodeJSON(r, w, 16<<10, &request); err != nil || protocol.ValidateAbsolutePath(request.Directory) != nil {
			writeError(w, http.StatusBadRequest, "invalid watch request")
			return
		}
		if s.live == nil {
			writeError(w, http.StatusNotImplemented, "live preview is unavailable")
			return
		}
		info, err := s.live.Start(r.Context(), request.Directory)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, live.ErrAlreadyWatching) {
				status = http.StatusConflict
			}
			if errors.Is(err, live.ErrTooManySessions) || errors.Is(err, live.ErrTooManyClients) {
				status = http.StatusTooManyRequests
			}
			writeError(w, status, err.Error())
			return
		}
		if info.ArtifactID != artifactID {
			if stopErr := s.live.Stop(info.ArtifactID); stopErr != nil {
				writeInternalError(w, stopErr)
				return
			}
			writeError(w, http.StatusBadRequest, "watch directory artifact ID does not match request")
			return
		}
		writeJSON(w, http.StatusCreated, protocol.LiveResponse{
			ArtifactID: info.ArtifactID,
			Directory:  info.Directory,
			Status:     string(info.Status),
			Hash:       info.Hash,
			Error:      info.Error,
			URL:        s.publicURL(info.ArtifactID),
		})
	case http.MethodDelete:
		if s.live == nil {
			writeError(w, http.StatusNotImplemented, "live preview is unavailable")
			return
		}
		if err := s.live.Stop(artifactID); err != nil {
			status := http.StatusNotFound
			if !errors.Is(err, live.ErrNotWatching) {
				status = http.StatusInternalServerError
			}
			writeError(w, status, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		if s.live != nil {
			if err := s.live.Stop(artifactID); err != nil && !errors.Is(err, live.ErrNotWatching) {
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

func (s *Server) liveInfo(w http.ResponseWriter, r *http.Request, artifactID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.live == nil {
		writeError(w, http.StatusNotImplemented, "live preview is unavailable")
		return
	}
	info, err := s.live.Info(artifactID)
	if errors.Is(err, live.ErrNotWatching) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.LiveResponse{
		ArtifactID: info.ArtifactID,
		Directory:  info.Directory,
		Status:     string(info.Status),
		Hash:       info.Hash,
		Error:      info.Error,
		URL:        s.publicURL(info.ArtifactID),
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
	if s.live != nil {
		if err := s.live.Stop(artifactID); err != nil && !errors.Is(err, live.ErrNotWatching) {
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
		if statusForError(err) >= http.StatusInternalServerError {
			writeInternalError(w, err)
			return
		}
		writeError(w, statusForError(err), userError(err))
		return
	}
	result, err := s.store.PublishStaged(r.Context(), staging, sourcePath)
	if err != nil {
		if statusForError(err) >= http.StatusInternalServerError {
			writeInternalError(w, err)
			return
		}
		writeError(w, statusForError(err), userError(err))
		return
	}
	writeJSON(w, http.StatusCreated, protocol.PublishResponse{
		Artifact: result.Artifact,
		Version:  result.Version.Number,
		URL:      s.publicURL(result.Artifact.ID),
	})
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
	return "internal server error"
}
