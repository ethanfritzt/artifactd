package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
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
