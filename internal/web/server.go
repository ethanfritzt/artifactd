package web

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"artifactd/internal/docx"
	"artifactd/internal/edit"
	"artifactd/internal/markdown"
	"artifactd/internal/model"
	"artifactd/internal/preview"
	"artifactd/internal/providers/filesystem"
	"artifactd/internal/providers/system"
	"artifactd/internal/registry"
	"artifactd/internal/runtime"
	"artifactd/internal/storage"
)

var artifactHostID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const editClientScript = `(() => {
  const overlayMessage = document.querySelector("[data-artifactd-edit-overlay-message]");
  let reloading = false;

  const setEditing = (editing, message) => {
    document.documentElement.classList.toggle("artifactd-editing", editing);
    if (editing) {
      document.documentElement.setAttribute("aria-busy", "true");
    } else {
      document.documentElement.removeAttribute("aria-busy");
    }
    if (overlayMessage && message) {
      overlayMessage.textContent = message;
    }
  };

  const reload = (message) => {
    if (reloading) return;
    reloading = true;
    setEditing(true, message);
    window.location.reload();
  };

  const applyEvent = (event) => {
    if (["edit_started", "edit_progress", "edit_committing", "edit_error"].includes(event.type)) {
      setEditing(true, event.message || event.error || "Editing artifact…");
      return;
    }
    if (event.type === "artifact_changed") {
      reload("Publishing artifact…");
      return;
    }
    if (event.type === "edit_aborted" || event.type === "edit_expired") {
      setEditing(false);
    }
  };

  const loadStatus = async () => {
    try {
      const response = await fetch("/_artifactd/edit", { cache: "no-store" });
      if (response.ok) {
        const status = await response.json();
        setEditing(true, status.message || status.error || "Editing artifact…");
      } else if (response.status === 404) {
        setEditing(false);
      }
    } catch (_) {
      // The event stream remains the source of truth for edit transitions.
    }
  };

  const events = new EventSource("/_artifactd/events");
  events.onmessage = (message) => {
    try {
      applyEvent(JSON.parse(message.data));
    } catch (_) {
      // Ignore malformed events; EventSource will reconnect automatically.
    }
  };
  loadStatus();
})();
`

const editOverlayCSS = `.artifactd-edit-overlay {
  position: fixed;
  inset: 0;
  z-index: 2147483646;
  display: grid;
  place-items: center;
  gap: .9rem;
  align-content: center;
  color: CanvasText;
  background: rgba(255, 255, 255, .72);
  background: color-mix(in srgb, Canvas 72%, transparent);
  backdrop-filter: blur(12px) saturate(.75);
  opacity: 0;
  pointer-events: none;
  visibility: hidden;
  transition: opacity .18s ease, visibility .18s ease;
  font: 500 .95rem/1.4 system-ui, sans-serif;
}
.artifactd-edit-overlay__content {
  display: grid;
  justify-items: center;
  gap: .75rem;
  padding: 1.25rem 1.5rem;
  border: 1px solid color-mix(in srgb, CanvasText 16%, transparent);
  border-radius: 1rem;
  background: color-mix(in srgb, Canvas 88%, transparent);
  box-shadow: 0 .75rem 2rem #0002;
}
.artifactd-edit-overlay__spinner {
  width: 1.5rem;
  height: 1.5rem;
  border: .18rem solid color-mix(in srgb, CanvasText 20%, transparent);
  border-top-color: currentColor;
  border-radius: 50%;
  animation: artifactd-edit-spin .8s linear infinite;
}
html.artifactd-editing .artifactd-edit-overlay {
  opacity: 1;
  visibility: visible;
  pointer-events: auto;
}
@keyframes artifactd-edit-spin {
  to { transform: rotate(360deg); }
}
@media (prefers-reduced-motion: reduce) {
  .artifactd-edit-overlay { transition: none; }
  .artifactd-edit-overlay__spinner { animation: none; }
}
@media print {
  .artifactd-edit-overlay { display: none; }
}
`

const artifactNavigationCSS = `.artifactd-navigation {
  position: fixed;
  top: 1rem;
  left: 1rem;
  z-index: 2147483647;
  font-family: system-ui, sans-serif;
}
.artifactd-navigation a {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  padding: .55rem .8rem;
  border: 1px solid color-mix(in srgb, currentColor 18%, transparent);
  border-radius: 999px;
  color: inherit;
  background: color-mix(in srgb, Canvas 92%, transparent);
  box-shadow: 0 .25rem 1rem #0002;
  font-size: .875rem;
  line-height: 1;
  text-decoration: none;
  backdrop-filter: blur(12px);
}
.artifactd-navigation a:hover {
  background: Canvas;
}
.artifactd-navigation a:focus-visible {
  outline: 2px solid Highlight;
  outline-offset: 2px;
}
@media print {
  .artifactd-navigation { display: none; }
}
`

type Server struct {
	store      *storage.Store
	publicHost string
	publicURL  func(string) string
	defaultID  string
	data       *runtime.Store
	system     *system.Provider
	filesystem *filesystem.Provider
	edit       *edit.Manager
	previews   *preview.Manager
}

func NewServer(
	store *storage.Store,
	publicHost string,
	publicURL func(string) string,
	defaultID string,
	data *runtime.Store,
	system *system.Provider,
	files *filesystem.Provider,
	editManager *edit.Manager,
) *Server {
	return &Server{
		store:      store,
		publicHost: publicHost,
		publicURL:  publicURL,
		defaultID:  defaultID,
		data:       data,
		system:     system,
		filesystem: files,
		edit:       editManager,
	}
}

// SetPreviewManager enables temporary previews before the server starts.
func (s *Server) SetPreviewManager(manager *preview.Manager) {
	s.previews = manager
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	previewMutation := r.Method == http.MethodPost &&
		(r.URL.Path == "/_artifactd/save" || r.URL.Path == "/_artifactd/render")
	allowed := r.Method == http.MethodGet || r.Method == http.MethodHead || previewMutation
	if !allowed {
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if id, ok := s.artifactIDFromHost(r.Host); ok {
		s.serveArtifactHost(w, r, id)
		return
	}
	if s.isLegacyHost(r.Host) {
		s.serveLegacyPath(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) serveArtifactHost(w http.ResponseWriter, r *http.Request, id string) {
	relative, err := normalizedRequestPath(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	runtimePath := ""
	if strings.HasPrefix(relative, "_artifactd/") {
		runtimePath = strings.TrimPrefix(relative, "_artifactd/")
	}
	if s.previews != nil {
		document, previewErr := s.previews.Get(id)
		if previewErr == nil {
			s.servePreview(w, r, document, relative, runtimePath)
			return
		}
		if !errors.Is(previewErr, preview.ErrNotFound) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	s.serveArtifact(w, r, id, relative, runtimePath)
}

func (s *Server) servePreview(
	w http.ResponseWriter,
	r *http.Request,
	document preview.Document,
	relative,
	runtimePath string,
) {
	if runtimePath == "markdown.css" {
		s.serveMarkdownCSS(w, r)
		return
	}
	if runtimePath == "save" && r.Method == http.MethodPost {
		s.savePreview(w, r, document)
		return
	}
	if runtimePath == "render" && r.Method == http.MethodPost {
		s.renderPreview(w, r, document)
		return
	}
	if runtimePath == "markdown-editor.js" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, markdownEditorScript)
		return
	}
	if runtimePath == "markdown-editor.css" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, markdownEditorStyles)
		return
	}
	if runtimePath == "document.docx" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.exportPreviewDOCX(w, r, document)
		return
	}
	if runtimePath == "navigation.css" {
		s.serveNavigationCSS(w)
		return
	}
	if relative != "" {
		http.NotFound(w, r)
		return
	}
	content, err := markdown.Render(document.Content, document.Name)
	if err != nil {
		http.Error(w, "could not render Markdown", http.StatusInternalServerError)
		return
	}
	content = injectArtifactNavigation(content, s.publicURL(s.defaultID))
	if document.SaveToken != "" {
		nonce := rand.Text()
		// CodeMirror's generated styles use this per-response nonce, not unsafe-inline.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'nonce-"+nonce+
			"'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		content = injectPreviewEditor(content, document, s.publicURL(s.defaultID), nonce)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeContent(w, r, document.Name, document.CreatedAt, bytes.NewReader(content))
}

// serveArtifact is shared by artifact-host and legacy-path routes so that both
// routes select the same published version and response decoration.
func (s *Server) serveArtifact(w http.ResponseWriter, r *http.Request, id, relative, runtimePath string) {
	artifact, version, err := s.store.Current(r.Context(), id)
	if errors.Is(err, registry.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if runtimePath != "" {
		s.serveRuntime(w, r, artifact.ID, version, runtimePath)
		return
	}

	root := version.Path
	if relative == "" {
		relative = version.Entry
	}
	decorate := relative == version.Entry && artifact.ID != s.defaultID
	s.serveFile(w, r, root, relative, decorate)
}

func (s *Server) serveLegacyPath(w http.ResponseWriter, r *http.Request) {
	requestPath, err := normalizedRequestPath(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if requestPath == "" {
		if s.defaultID != "" {
			switch _, _, err := s.store.Current(r.Context(), s.defaultID); {
			case err == nil:
				http.Redirect(w, r, s.publicURL(s.defaultID), http.StatusMovedPermanently)
				return
			case errors.Is(err, registry.ErrNotFound):
			default:
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(requestPath, "/")
	id := parts[0]
	if !artifactHostID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 && !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, s.publicURL(id), http.StatusMovedPermanently)
		return
	}
	relative := ""
	if len(parts) > 1 {
		relative = strings.Join(parts[1:], "/")
	}
	s.serveArtifact(w, r, id, relative, "")
}

func (s *Server) serveRuntime(w http.ResponseWriter, r *http.Request, artifactID string, version model.Version, runtimePath string) {
	parts := strings.Split(runtimePath, "/")
	if len(parts) == 1 && parts[0] == "edit.js" {
		s.serveEditClient(w)
		return
	}
	if len(parts) == 1 && parts[0] == "edit.css" {
		s.serveEditCSS(w)
		return
	}
	if len(parts) == 1 && parts[0] == "navigation.css" {
		s.serveNavigationCSS(w)
		return
	}
	if len(parts) == 1 && parts[0] == "markdown.css" {
		s.serveMarkdownCSS(w, r)
		return
	}
	if len(parts) == 1 && parts[0] == "events" {
		s.serveEvents(w, r, artifactID)
		return
	}
	if len(parts) == 1 && parts[0] == "edit" {
		if s.edit == nil {
			writeRuntimeError(w, http.StatusNotImplemented, "editing is unavailable")
			return
		}
		session, err := s.edit.Status(artifactID)
		if errors.Is(err, edit.ErrNoEdit) {
			writeRuntimeError(w, http.StatusNotFound, "artifact is not being edited")
			return
		}
		if err != nil {
			writeRuntimeError(w, http.StatusInternalServerError, "editing is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, session)
		return
	}
	if len(parts) == 1 && parts[0] == "library" {
		artifacts, err := s.store.List(r.Context(), false)
		if err != nil {
			writeRuntimeError(w, http.StatusInternalServerError, "artifact library unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
		return
	}
	if len(parts) == 1 && parts[0] == "system" {
		if s.system == nil {
			writeRuntimeError(w, http.StatusNotImplemented, "system provider is unavailable")
			return
		}
		snapshot, err := s.system.Snapshot()
		if err != nil {
			writeRuntimeError(w, http.StatusServiceUnavailable, "system metrics are unavailable")
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
		return
	}
	if len(parts) == 2 && parts[0] == "data" {
		if s.data == nil {
			writeRuntimeError(w, http.StatusNotImplemented, "runtime data is unavailable")
			return
		}
		data, err := s.data.Get(artifactID, parts[1])
		if errors.Is(err, runtime.ErrNotFound) {
			writeRuntimeError(w, http.StatusNotFound, "runtime data not found")
			return
		}
		if err != nil {
			writeRuntimeError(w, http.StatusInternalServerError, "runtime data unavailable")
			return
		}
		writeJSON(w, http.StatusOK, data)
		return
	}
	if len(parts) == 1 && parts[0] == "files" {
		if s.filesystem == nil || version.WorkspaceID == "" {
			writeRuntimeError(w, http.StatusNotFound, "artifact has no workspace")
			return
		}
		workspace, err := s.store.Workspace(r.Context(), version.WorkspaceID)
		if err != nil {
			writeRuntimeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		depth, err := queryDepth(r)
		if err != nil {
			writeRuntimeError(w, http.StatusBadRequest, err.Error())
			return
		}
		nodes, err := s.filesystem.List(workspace, r.URL.Query().Get("path"), depth)
		if err != nil {
			writeRuntimeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": workspace.ID, "nodes": nodes})
		return
	}
	writeRuntimeError(w, http.StatusNotFound, "runtime endpoint not found")
}

func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request, artifactID string) {
	if s.edit == nil {
		writeRuntimeError(w, http.StatusNotImplemented, "editing is unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRuntimeError(w, http.StatusInternalServerError, "edit events are unavailable")
		return
	}
	events, unsubscribe, err := s.edit.Subscribe(artifactID)
	if errors.Is(err, edit.ErrTooManyClients) {
		writeRuntimeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "edit events are unavailable")
		return
	}
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				slog.Error("encoding edit event", "artifact_id", artifactID, "error", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) serveEditClient(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, editClientScript); err != nil {
		slog.Error("writing edit client", "error", err)
	}
}

func (s *Server) serveEditCSS(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, editOverlayCSS); err != nil {
		slog.Error("writing edit overlay stylesheet", "error", err)
	}
}

func (s *Server) serveNavigationCSS(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, artifactNavigationCSS); err != nil {
		slog.Error("writing artifact navigation stylesheet", "error", err)
	}
}

func (s *Server) serveMarkdownCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, "markdown.css", time.Time{}, strings.NewReader(markdown.Stylesheet))
}

func (s *Server) serveFile(
	w http.ResponseWriter,
	r *http.Request,
	root,
	relative string,
	decorate bool,
) {
	filePath, err := safeFilePath(root, relative)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	info, err := os.Lstat(filePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	isHTML := strings.EqualFold(filepath.Ext(filePath), ".html")
	if isHTML {
		s.serveHTML(w, r, filePath, decorate)
		return
	}
	if markdown.IsFilename(filePath) {
		s.serveMarkdown(w, r, filePath, decorate)
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()
	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), file)
}

func (s *Server) serveMarkdown(
	w http.ResponseWriter,
	r *http.Request,
	filePath string,
	decorate bool,
) {
	info, err := os.Lstat(filePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if info.Size() > markdown.MaxBytes {
		http.Error(w, "Markdown document is too large", http.StatusRequestEntityTooLarge)
		return
	}
	source, err := os.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	content, err := markdown.Render(source, filepath.Base(filePath))
	if err != nil {
		http.Error(w, "could not render Markdown", http.StatusInternalServerError)
		return
	}
	content = injectEditClient(content)
	if decorate {
		content = injectArtifactNavigation(content, s.publicURL(s.defaultID))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), bytes.NewReader(content))
}

func (s *Server) serveHTML(
	w http.ResponseWriter,
	r *http.Request,
	filePath string,
	decorate bool,
) {
	info, err := os.Lstat(filePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	content = injectEditClient(content)
	if decorate {
		content = injectArtifactNavigation(content, s.publicURL(s.defaultID))
	}
	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), bytes.NewReader(content))
}

func injectEditClient(content []byte) []byte {
	const marker = `data-artifactd-edit`
	if bytes.Contains(content, []byte(marker)) {
		return content
	}
	injected := `<link rel="stylesheet" href="/_artifactd/edit.css">
<div class="artifactd-edit-overlay" data-artifactd-edit-overlay role="status" aria-live="polite">
  <div class="artifactd-edit-overlay__content">
    <div class="artifactd-edit-overlay__spinner" aria-hidden="true"></div>
    <span data-artifactd-edit-overlay-message>Editing artifact…</span>
  </div>
</div>
<script data-artifactd-edit src="/_artifactd/edit.js"></script>`
	return injectBeforeBodyClose(content, injected)
}

func (s *Server) savePreview(w http.ResponseWriter, r *http.Request, document preview.Document) {
	content, ok := readPreviewContent(w, r, document)
	if !ok {
		return
	}
	rendered, err := markdown.RenderBody(content)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not render Markdown"})
		return
	}
	if err := s.previews.Save(document.ID, document.SaveToken, content); err != nil {
		switch {
		case errors.Is(err, preview.ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, preview.ErrUnauthorized):
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		case errors.Is(err, preview.ErrConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, preview.ErrTooLarge):
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save Markdown file"})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "html": string(rendered)})
}

func (s *Server) renderPreview(w http.ResponseWriter, r *http.Request, document preview.Document) {
	content, ok := readPreviewContent(w, r, document)
	if !ok {
		return
	}
	rendered, err := markdown.RenderBody(content)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not render Markdown"})
		return
	}
	// Rendering a draft never changes the preview's saved content or selected file.
	writeJSON(w, http.StatusOK, map[string]string{"html": string(rendered)})
}

func readPreviewContent(w http.ResponseWriter, r *http.Request, document preview.Document) ([]byte, bool) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if !strings.EqualFold(r.Header.Get("Origin"), scheme+"://"+r.Host) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "same-origin request required"})
		return nil, false
	}
	if document.SaveToken == "" || r.Header.Get("Authorization") != "Bearer "+document.SaveToken {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": preview.ErrUnauthorized.Error()})
		return nil, false
	}

	// A JSON-escaped source byte can occupy six bytes (e.g. a control character).
	// Bound both the envelope and decoded document without shrinking the 5 MiB limit.
	r.Body = http.MaxBytesReader(w, r.Body, 6*preview.MaxBytes+1024)
	var request struct {
		Content *string `json:"content"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": preview.ErrTooLarge.Error()})
			return nil, false
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid Markdown content request"})
		return nil, false
	}
	if request.Content == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Markdown content is required"})
		return nil, false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid Markdown content request"})
		return nil, false
	}
	if len(*request.Content) > preview.MaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": preview.ErrTooLarge.Error()})
		return nil, false
	}
	return []byte(*request.Content), true
}

func (s *Server) exportPreviewDOCX(w http.ResponseWriter, r *http.Request, document preview.Document) {
	content, err := docx.Render(document.Content)
	if err != nil {
		http.Error(w, "could not export Markdown document", http.StatusInternalServerError)
		return
	}
	name := strings.TrimSuffix(document.Name, filepath.Ext(document.Name)) + ".docx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, document.CreatedAt, bytes.NewReader(content))
}

func injectPreviewEditor(content []byte, document preview.Document, homeURL, nonce string) []byte {
	header := `<link rel="stylesheet" href="/_artifactd/markdown-editor.css">
<header class="artifactd-md-shell" data-save-token="` + html.EscapeString(document.SaveToken) + `" data-style-nonce="` + html.EscapeString(nonce) + `">
  <div class="artifactd-md-bar">
    <div class="artifactd-md-breadcrumb">
      <a class="artifactd-md-back" href="` + html.EscapeString(homeURL) + `" aria-label="Back to Artifactd library" title="Back to library">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m14 6-6 6 6 6"/></svg>
      </a>
      <span class="artifactd-md-filename" title="` + html.EscapeString(document.Name) + `">` + html.EscapeString(document.Name) + `</span>
    </div>
    <nav class="artifactd-md-actions" aria-label="Document actions">
      <button type="button" class="artifactd-md-toggle" data-action="toggle-view" aria-label="Edit document" aria-pressed="false" aria-controls="artifactd-md-document artifactd-md-source" title="Toggle editing (Ctrl/Cmd+E)">
        <svg class="artifactd-md-edit-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="m15 5 4 4M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15Z"/></svg>
        <svg class="artifactd-md-preview-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v15M3 4l9 2 9-2v14l-9 2-9-2Z"/></svg>
        <span>Edit</span>
      </button>
      <button type="button" class="artifactd-md-save" data-action="save" disabled>Save</button>
      <details class="artifactd-md-export">
        <summary>Export <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m4 6 4 4 4-4"/></svg></summary>
        <div class="artifactd-md-export-panel">
          <span class="artifactd-md-menu-label">Export saved document</span>
          <button type="button" data-action="pdf" aria-label="Export PDF">
            <span class="artifactd-md-format" aria-hidden="true">PDF</span>
            <span><strong>PDF document</strong><small>Print or save a polished copy</small></span>
          </button>
          <a href="/_artifactd/document.docx" download aria-label="Export DOCX">
            <span class="artifactd-md-format" aria-hidden="true">DOCX</span>
            <span><strong>Word document</strong><small>Text &amp; basic formatting</small></span>
          </a>
          <p>Exports use the saved version, not unsaved edits.</p>
        </div>
      </details>
    </nav>
  </div>
</header>`
	editor := `<section class="artifactd-md-editor-wrap" aria-label="Source editor">
  <div class="artifactd-md-editor-label"><label id="artifactd-md-source-label" for="artifactd-md-source">Markdown source</label></div>
  <textarea id="artifactd-md-source" class="artifactd-md-editor" spellcheck="false">` + html.EscapeString(string(document.Content)) + `</textarea>
</section>
</div>
<footer class="artifactd-md-context">
  <div class="artifactd-md-status" role="status" aria-live="polite" data-state="saved">All changes saved</div>
  <span class="artifactd-md-editing-hint">Ctrl/Cmd+S to save · Escape, then Tab to leave the editor</span>
  <span class="artifactd-md-mode">Preview</span>
</footer>
<script src="/_artifactd/markdown-editor.js" defer></script>`
	content = bytes.Replace(content, []byte(`<main class="artifactd-markdown">`),
		[]byte(`<div class="artifactd-md-workspace"><main id="artifactd-md-document" class="artifactd-markdown">`), 1)
	content = injectAfterBodyOpen(content, header)
	return injectBeforeBodyClose(content, editor)
}

func injectAfterBodyOpen(content []byte, injected string) []byte {
	const openingBody = "<body>"
	index := bytes.Index(bytes.ToLower(content), []byte(openingBody))
	if index < 0 {
		return content
	}
	index += len(openingBody)
	result := make([]byte, 0, len(content)+len(injected)+1)
	result = append(result, content[:index]...)
	result = append(result, '\n')
	result = append(result, injected...)
	result = append(result, content[index:]...)
	return result
}

func injectArtifactNavigation(content []byte, homeURL string) []byte {
	const marker = `data-artifactd-navigation`
	if bytes.Contains(content, []byte(marker)) {
		return content
	}

	navigation := fmt.Sprintf(`<link rel="stylesheet" href="/_artifactd/navigation.css">
<nav class="artifactd-navigation" aria-label="Artifactd navigation" %s>
  <a href="%s">← Back to library</a>
</nav>`, marker, html.EscapeString(homeURL))
	return injectBeforeBodyClose(content, navigation)
}

func injectBeforeBodyClose(content []byte, injected string) []byte {
	lower := bytes.ToLower(content)
	closingBody := bytes.LastIndex(lower, []byte("</body>"))
	if closingBody < 0 {
		return append(content, []byte("\n"+injected+"\n")...)
	}
	result := make([]byte, 0, len(content)+len(injected)+1)
	result = append(result, content[:closingBody]...)
	result = append(result, injected...)
	result = append(result, '\n')
	result = append(result, content[closingBody:]...)
	return result
}

func (s *Server) artifactIDFromHost(host string) (string, bool) {
	host = strings.TrimSuffix(hostName(host), ".")
	base := strings.TrimSuffix(hostName(s.publicHost), ".")
	if base == "" {
		return "", false
	}
	suffix := "." + base
	if !strings.HasSuffix(host, suffix) || len(host) <= len(suffix) {
		return "", false
	}
	id := strings.TrimSuffix(host, suffix)
	if len(id) > 63 || !artifactHostID.MatchString(id) {
		return "", false
	}
	return id, true
}

func (s *Server) isLegacyHost(host string) bool {
	configured := strings.TrimSuffix(hostName(s.publicHost), ".")
	candidate := strings.TrimSuffix(hostName(host), ".")
	if configured != "" && candidate == configured {
		return true
	}
	if candidate == "localhost" {
		return true
	}
	ip := net.ParseIP(candidate)
	return ip != nil && ip.IsLoopback()
}

func hostName(host string) string {
	if name, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(name)
	}
	return strings.ToLower(host)
}

func normalizedRequestPath(requestPath string) (string, error) {
	if requestPath == "" {
		requestPath = "/"
	}
	if !strings.HasPrefix(requestPath, "/") || strings.Contains(requestPath, "\x00") || strings.Contains(requestPath, "\\") {
		return "", errors.New("invalid browser path")
	}
	clean := path.Clean(requestPath)
	canonical := clean
	if strings.HasSuffix(requestPath, "/") && clean != "/" {
		canonical += "/"
	}
	if canonical != requestPath {
		return "", errors.New("browser path must be normalized")
	}
	relative := strings.Trim(strings.TrimPrefix(requestPath, "/"), "/")
	if relative == "" {
		return "", nil
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "." || segment == ".." || segment == "" {
			return "", errors.New("invalid browser path")
		}
	}
	return relative, nil
}

func queryDepth(r *http.Request) (int, error) {
	value := r.URL.Query().Get("depth")
	if value == "" {
		return filesystem.DefaultDepth, nil
	}
	depth, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("depth must be an integer")
	}
	return depth, nil
}

func safeFilePath(root, relative string) (string, error) {
	if relative == "" || strings.Contains(relative, "\x00") || strings.Contains(relative, "\\") || filepath.IsAbs(filepath.FromSlash(relative)) || !filepath.IsLocal(filepath.FromSlash(relative)) {
		return "", fmt.Errorf("invalid artifact path")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes version")
	}
	if filepath.ToSlash(clean) != relative {
		return "", fmt.Errorf("artifact path must be normalized")
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving artifact root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("reading artifact root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", fmt.Errorf("artifact root is not a directory")
	}
	candidate := filepath.Join(root, clean)
	resolvedRelative, err := filepath.Rel(root, candidate)
	if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes version")
	}

	// Published versions reject symlinks at copy time. Check again
	// while serving so a damaged or externally modified snapshot cannot turn
	// the browser route into a file read outside its version root.
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("artifact path contains a symlink")
		}
	}
	return candidate, nil
}

func writeRuntimeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("writing browser JSON response", "error", err)
	}
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Vary", "Host")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
}
