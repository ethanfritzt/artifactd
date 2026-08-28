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

	"artifactd/internal/live"
	"artifactd/internal/model"
	"artifactd/internal/providers/filesystem"
	"artifactd/internal/providers/system"
	"artifactd/internal/registry"
	"artifactd/internal/runtime"
	"artifactd/internal/storage"
)

var artifactHostID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const liveClientScript = `(() => {
  const events = new EventSource("/_artifactd/events");
  events.onmessage = (message) => {
    try {
      const event = JSON.parse(message.data);
      if (event.type === "artifact_changed") {
        window.location.reload();
      }
    } catch (_) {
      // Ignore malformed events; the next connection will retry automatically.
    }
  };
})();
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
	live       *live.Manager
}

func NewServer(
	store *storage.Store,
	publicHost string,
	publicURL func(string) string,
	defaultID string,
	data *runtime.Store,
	system *system.Provider,
	files *filesystem.Provider,
	liveManager *live.Manager,
) *Server {
	return &Server{
		store:      store,
		publicHost: publicHost,
		publicURL:  publicURL,
		defaultID:  defaultID,
		data:       data,
		system:     system,
		filesystem: files,
		live:       liveManager,
	}
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if id, ok := s.artifactIDFromHost(r.Host); ok {
		s.serveArtifactHost(w, r, id)
		return
	}
	s.serveLegacyPath(w, r)
}

func (s *Server) serveArtifactHost(w http.ResponseWriter, r *http.Request, id string) {
	artifact, version, err := s.store.Current(r.Context(), id)
	if errors.Is(err, registry.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/_artifactd/") {
		s.serveRuntime(w, r, artifact.ID, version)
		return
	}
	root := version.Path
	entry := version.Entry
	livePreview := s.live != nil
	if s.live != nil {
		if snapshot, release, ok := s.live.Snapshot(artifact.ID); ok {
			defer release()
			root = snapshot.Path
			entry = snapshot.Entry
			livePreview = true
		}
	}
	relative := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if relative == "." || relative == "" {
		relative = entry
	}
	decorate := relative == entry && artifact.ID != s.defaultID
	s.serveFile(w, r, root, relative, livePreview, decorate)
}

func (s *Server) serveLegacyPath(w http.ResponseWriter, r *http.Request) {
	requestPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requestPath == "" || requestPath == "." {
		if s.defaultID != "" {
			if _, _, err := s.store.Current(r.Context(), s.defaultID); err == nil {
				http.Redirect(w, r, s.publicURL(s.defaultID), http.StatusMovedPermanently)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(requestPath, "/")
	id := parts[0]
	artifact, version, err := s.store.Current(r.Context(), id)
	if errors.Is(err, registry.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if len(parts) == 1 && !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, s.publicURL(artifact.ID), http.StatusMovedPermanently)
		return
	}
	root := version.Path
	entry := version.Entry
	livePreview := s.live != nil
	if s.live != nil {
		if snapshot, release, ok := s.live.Snapshot(artifact.ID); ok {
			defer release()
			root = snapshot.Path
			entry = snapshot.Entry
			livePreview = true
		}
	}
	relative := entry
	if len(parts) > 1 {
		relative = strings.Join(parts[1:], "/")
	}
	decorate := relative == entry && artifact.ID != s.defaultID
	s.serveFile(w, r, root, relative, livePreview, decorate)
}

func (s *Server) serveRuntime(w http.ResponseWriter, r *http.Request, artifactID string, version model.Version) {
	runtimePath := strings.TrimPrefix(r.URL.Path, "/_artifactd/")
	parts := strings.Split(runtimePath, "/")
	if len(parts) == 1 && parts[0] == "live.js" {
		s.serveLiveClient(w)
		return
	}
	if len(parts) == 1 && parts[0] == "navigation.css" {
		s.serveNavigationCSS(w)
		return
	}
	if len(parts) == 1 && parts[0] == "events" {
		s.serveEvents(w, r, artifactID)
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
	if s.live == nil {
		writeRuntimeError(w, http.StatusNotImplemented, "live preview is unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRuntimeError(w, http.StatusInternalServerError, "live events are unavailable")
		return
	}
	events, unsubscribe, err := s.live.Subscribe(artifactID)
	if errors.Is(err, live.ErrTooManyClients) {
		writeRuntimeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		writeRuntimeError(w, http.StatusInternalServerError, "live events are unavailable")
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
				slog.Error("encoding live event", "artifact_id", artifactID, "error", err)
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

func (s *Server) serveLiveClient(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, liveClientScript); err != nil {
		slog.Error("writing live client", "error", err)
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

func (s *Server) serveFile(
	w http.ResponseWriter,
	r *http.Request,
	root,
	relative string,
	livePreview,
	decorate bool,
) {
	filePath, err := safeFilePath(root, relative)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	isHTML := strings.EqualFold(filepath.Ext(filePath), ".html")
	if (livePreview || decorate) && isHTML {
		s.serveHTML(w, r, filePath, livePreview, decorate)
		return
	}
	http.ServeFile(w, r, filePath)
}

func (s *Server) serveHTML(
	w http.ResponseWriter,
	r *http.Request,
	filePath string,
	livePreview,
	decorate bool,
) {
	info, err := os.Stat(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if livePreview {
		content = injectLiveClient(content)
	}
	if decorate {
		content = injectArtifactNavigation(content, s.publicURL(s.defaultID))
	}
	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), bytes.NewReader(content))
}

func injectLiveClient(content []byte) []byte {
	const script = `<script data-artifactd-live src="/_artifactd/live.js"></script>`
	if bytes.Contains(content, []byte(`data-artifactd-live`)) {
		return content
	}
	return injectBeforeBodyClose(content, script)
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
	base := strings.TrimSuffix(strings.ToLower(s.publicHost), ".")
	suffix := "." + base
	if !strings.HasSuffix(host, suffix) || len(host) <= len(suffix) {
		return "", false
	}
	id := strings.TrimSuffix(host, suffix)
	if !artifactHostID.MatchString(id) {
		return "", false
	}
	return id, true
}

func hostName(host string) string {
	if name, _, err := net.SplitHostPort(host); err == nil {
		return name
	}
	return host
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
	if relative == "" || strings.Contains(relative, "\\") || filepath.IsAbs(filepath.FromSlash(relative)) || !filepath.IsLocal(filepath.FromSlash(relative)) {
		return "", fmt.Errorf("invalid artifact path")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes version")
	}
	if filepath.ToSlash(clean) != relative {
		return "", fmt.Errorf("artifact path must be normalized")
	}
	return filepath.Join(root, clean), nil
}

func writeRuntimeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("writing browser JSON response", "error", err)
	}
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
}
