package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"artifactd/internal/manifest"
	"artifactd/internal/model"
	"artifactd/internal/preview"
	"artifactd/internal/protocol"
)

const requestTimeout = 30 * time.Second

var ErrDaemonUnavailable = errors.New("artifactd unavailable")

// TransportError identifies a failure to communicate with artifactd. Its
// wrapped error remains available to callers with errors.Is/errors.As.
type TransportError struct {
	Operation string
	Err       error
}

func (e *TransportError) Error() string {
	if e.Operation == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %v", e.Operation, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// IsDaemonUnavailable reports whether a request could not find or connect to
// the local daemon, without relying on the platform-specific error text.
func IsDaemonUnavailable(err error) bool {
	return errors.Is(err, ErrDaemonUnavailable)
}

type DaemonError struct {
	StatusCode int
	Status     string
	Message    string
}

func (e *DaemonError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("artifactd returned %s", e.Status)
	}
	return fmt.Sprintf("artifactd returned %s: %s", e.Status, e.Message)
}

type Client struct {
	httpClient *http.Client
}

func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialUnixWithStartupRetry(ctx, socketPath)
		},
	}
	return &Client{httpClient: &http.Client{Transport: transport}}
}

func dialUnixWithStartupRetry(ctx context.Context, socketPath string) (net.Conn, error) {
	dialer := &net.Dialer{}
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for {
		conn, err := dialer.DialContext(ctx, "unix", socketPath)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if !isDaemonUnavailable(err) {
			return nil, &TransportError{Operation: "connecting to artifactd", Err: err}
		}
		if time.Now().After(deadline) {
			return nil, &TransportError{
				Operation: "connecting to artifactd",
				Err:       errors.Join(ErrDaemonUnavailable, lastErr),
			}
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, &TransportError{Operation: "connecting to artifactd", Err: ctx.Err()}
		case <-timer.C:
		}
	}
}

func (c *Client) Health(ctx context.Context) error {
	var response protocol.HealthResponse
	return c.doJSON(ctx, http.MethodGet, "/v1/health", nil, &response)
}

func (c *Client) List(ctx context.Context, includeArchived bool) ([]model.Artifact, error) {
	var response protocol.ListResponse
	path := "/v1/artifacts"
	if includeArchived {
		path += "?include_archived=true"
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	if response.Artifacts == nil {
		response.Artifacts = []model.Artifact{}
	}
	return response.Artifacts, nil
}

func (c *Client) Archive(ctx context.Context, artifactID string) (protocol.ArtifactResponse, error) {
	requestPath, err := artifactPath(artifactID, "/archive")
	if err != nil {
		return protocol.ArtifactResponse{}, err
	}
	var response protocol.ArtifactResponse
	if err := c.doJSON(ctx, http.MethodPost, requestPath, nil, &response); err != nil {
		return protocol.ArtifactResponse{}, err
	}
	return response, nil
}

func (c *Client) Unarchive(ctx context.Context, artifactID string) (protocol.ArtifactResponse, error) {
	requestPath, err := artifactPath(artifactID, "/archive")
	if err != nil {
		return protocol.ArtifactResponse{}, err
	}
	var response protocol.ArtifactResponse
	if err := c.doJSON(ctx, http.MethodDelete, requestPath, nil, &response); err != nil {
		return protocol.ArtifactResponse{}, err
	}
	return response, nil
}

func (c *Client) Publish(ctx context.Context, directory string) (protocol.PublishResponse, error) {
	if directory == "" {
		return protocol.PublishResponse{}, fmt.Errorf("artifact directory is required")
	}
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return protocol.PublishResponse{}, fmt.Errorf("resolving artifact directory: %w", err)
	}
	absoluteDirectory = filepath.Clean(absoluteDirectory)
	if err := protocol.ValidateAbsolutePath(absoluteDirectory); err != nil {
		return protocol.PublishResponse{}, err
	}
	if _, _, err := manifest.ValidateDirectory(absoluteDirectory); err != nil {
		return protocol.PublishResponse{}, err
	}

	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	contentType := multipartWriter.FormDataContentType()
	writerDone := make(chan error, 1)
	go func() {
		err := multipartWriter.WriteField("source_path", absoluteDirectory)
		if err == nil {
			err = writeMultipart(multipartWriter, absoluteDirectory)
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
		} else {
			_ = pipeWriter.Close()
		}
		writerDone <- err
	}()

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "http://artifactd/v1/artifacts/publish", pipeReader)
	if err != nil {
		_ = pipeReader.Close()
		<-writerDone
		return protocol.PublishResponse{}, fmt.Errorf("creating publish request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		_ = pipeReader.Close()
		writerErr := <-writerDone
		if writerErr != nil {
			return protocol.PublishResponse{}, errors.Join(fmt.Errorf("publishing artifact: %w", err), writerErr)
		}
		return protocol.PublishResponse{}, fmt.Errorf("publishing artifact: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			slog.Debug("closing daemon response", "error", closeErr)
		}
	}()
	writerErr := <-writerDone
	if writerErr != nil {
		return protocol.PublishResponse{}, writerErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return protocol.PublishResponse{}, decodeError(response.Body, response.Status, response.StatusCode)
	}
	var result protocol.PublishResponse
	if err := decodeResponse(response.Body, &result); err != nil {
		return protocol.PublishResponse{}, fmt.Errorf("decoding publish response: %w", err)
	}
	return result, nil
}

func (c *Client) Preview(ctx context.Context, path string) (protocol.PreviewResponse, error) {
	name, content, err := preview.LoadFile(path)
	if err != nil {
		return protocol.PreviewResponse{}, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return protocol.PreviewResponse{}, fmt.Errorf("creating preview upload: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return protocol.PreviewResponse{}, fmt.Errorf("writing preview upload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return protocol.PreviewResponse{}, fmt.Errorf("closing preview upload: %w", err)
	}

	var response protocol.PreviewResponse
	if err := c.doJSONWithContentType(
		ctx,
		http.MethodPost,
		"/v1/previews",
		&body,
		writer.FormDataContentType(),
		&response,
	); err != nil {
		var daemonErr *DaemonError
		if errors.As(err, &daemonErr) && daemonErr.StatusCode == http.StatusNotFound {
			return protocol.PreviewResponse{}, fmt.Errorf("creating Markdown preview: artifactd is running an older version; restart artifactd: %w", err)
		}
		return protocol.PreviewResponse{}, err
	}
	return response, nil
}

func (c *Client) AddWorkspace(ctx context.Context, id, root string) (model.Workspace, error) {
	if err := protocol.ValidateArtifactID(id); err != nil {
		return model.Workspace{}, err
	}
	if root == "" {
		return model.Workspace{}, fmt.Errorf("workspace root is required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return model.Workspace{}, fmt.Errorf("resolving workspace root: %w", err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	if err := protocol.ValidateAbsolutePath(absoluteRoot); err != nil {
		return model.Workspace{}, err
	}
	body, err := json.Marshal(protocol.WorkspaceRequest{ID: id, Root: absoluteRoot})
	if err != nil {
		return model.Workspace{}, fmt.Errorf("encoding workspace request: %w", err)
	}
	var response protocol.WorkspaceResponse
	if err := c.doJSONWithContentType(ctx, http.MethodPost, "/v1/workspaces", bytes.NewReader(body), "application/json", &response); err != nil {
		return model.Workspace{}, err
	}
	return response.Workspace, nil
}

func (c *Client) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var response protocol.WorkspacesResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/workspaces", nil, &response); err != nil {
		return nil, err
	}
	return response.Workspaces, nil
}

func (c *Client) BeginEdit(ctx context.Context, directory, message string) (protocol.EditSessionResponse, error) {
	absoluteDirectory, err := absoluteDirectory(directory)
	if err != nil {
		return protocol.EditSessionResponse{}, err
	}
	artifactID, err := manifestFromDirectory(absoluteDirectory)
	if err != nil {
		return protocol.EditSessionResponse{}, err
	}
	body, err := json.Marshal(protocol.EditBeginRequest{Directory: absoluteDirectory, Message: message})
	if err != nil {
		return protocol.EditSessionResponse{}, fmt.Errorf("encoding edit request: %w", err)
	}
	requestPath, err := artifactPath(artifactID, "/edit")
	if err != nil {
		return protocol.EditSessionResponse{}, err
	}
	var response protocol.EditSessionResponse
	if err := c.doJSONWithContentType(ctx, http.MethodPost, requestPath, bytes.NewReader(body), "application/json", &response); err != nil {
		return protocol.EditSessionResponse{}, err
	}
	return response, nil
}

func (c *Client) EditProgress(ctx context.Context, artifactID, sessionID, message string) (protocol.EditSessionResponse, error) {
	body, err := json.Marshal(protocol.EditProgressRequest{SessionID: sessionID, Message: message})
	if err != nil {
		return protocol.EditSessionResponse{}, fmt.Errorf("encoding edit progress: %w", err)
	}
	requestPath, err := artifactPath(artifactID, "/edit/progress")
	if err != nil {
		return protocol.EditSessionResponse{}, err
	}
	var response protocol.EditSessionResponse
	if err := c.doJSONWithContentType(ctx, http.MethodPost, requestPath, bytes.NewReader(body), "application/json", &response); err != nil {
		return protocol.EditSessionResponse{}, err
	}
	return response, nil
}

func (c *Client) EditStatus(ctx context.Context, artifactID string) (protocol.EditSessionResponse, error) {
	requestPath, err := artifactPath(artifactID, "/edit")
	if err != nil {
		return protocol.EditSessionResponse{}, err
	}
	var response protocol.EditSessionResponse
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &response); err != nil {
		return protocol.EditSessionResponse{}, err
	}
	return response, nil
}

func (c *Client) EditAbort(ctx context.Context, artifactID, sessionID string) error {
	requestPath, err := artifactPath(artifactID, "/edit")
	if err != nil {
		return err
	}
	requestPath += "?session_id=" + url.QueryEscape(sessionID)
	return c.doJSON(ctx, http.MethodDelete, requestPath, nil, nil)
}

func (c *Client) EditCommit(ctx context.Context, artifactID, sessionID string, baseVersion int, directory string) (protocol.PublishResponse, error) {
	absoluteDirectory, err := absoluteDirectory(directory)
	if err != nil {
		return protocol.PublishResponse{}, err
	}
	if _, err := manifestFromDirectory(absoluteDirectory); err != nil {
		return protocol.PublishResponse{}, err
	}

	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	contentType := multipartWriter.FormDataContentType()
	writerDone := make(chan error, 1)
	go func() {
		err := multipartWriter.WriteField("session_id", sessionID)
		if err == nil {
			err = multipartWriter.WriteField("base_version", fmt.Sprint(baseVersion))
		}
		if err == nil {
			err = multipartWriter.WriteField("source_path", absoluteDirectory)
		}
		if err == nil {
			err = writeMultipart(multipartWriter, absoluteDirectory)
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
		} else {
			_ = pipeWriter.Close()
		}
		writerDone <- err
	}()

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	requestPath, err := artifactPath(artifactID, "/edit/commit")
	if err != nil {
		_ = pipeReader.Close()
		<-writerDone
		return protocol.PublishResponse{}, err
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "http://artifactd"+requestPath, pipeReader)
	if err != nil {
		_ = pipeReader.Close()
		<-writerDone
		return protocol.PublishResponse{}, fmt.Errorf("creating edit commit request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		_ = pipeReader.Close()
		writerErr := <-writerDone
		if writerErr != nil {
			return protocol.PublishResponse{}, errors.Join(fmt.Errorf("committing edit: %w", err), writerErr)
		}
		return protocol.PublishResponse{}, fmt.Errorf("committing edit: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			slog.Debug("closing daemon response", "error", closeErr)
		}
	}()
	writerErr := <-writerDone
	if writerErr != nil {
		return protocol.PublishResponse{}, writerErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return protocol.PublishResponse{}, decodeError(response.Body, response.Status, response.StatusCode)
	}
	var result protocol.PublishResponse
	if err := decodeResponse(response.Body, &result); err != nil {
		return protocol.PublishResponse{}, fmt.Errorf("decoding edit commit response: %w", err)
	}
	return result, nil
}

func absoluteDirectory(directory string) (string, error) {
	if directory == "" {
		return "", fmt.Errorf("artifact directory is required")
	}
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolving artifact directory: %w", err)
	}
	absoluteDirectory = filepath.Clean(absoluteDirectory)
	if err := protocol.ValidateAbsolutePath(absoluteDirectory); err != nil {
		return "", err
	}
	resolvedDirectory, err := filepath.EvalSymlinks(absoluteDirectory)
	if err != nil {
		return "", fmt.Errorf("resolving artifact directory symlinks: %w", err)
	}
	return filepath.Clean(resolvedDirectory), nil
}

func (c *Client) Versions(ctx context.Context, artifactID string) ([]model.Version, error) {
	requestPath, err := artifactPath(artifactID, "/versions")
	if err != nil {
		return nil, err
	}
	var response protocol.VersionsResponse
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &response); err != nil {
		return nil, err
	}
	return response.Versions, nil
}

func (c *Client) Restore(ctx context.Context, artifactID string, version int) (protocol.ArtifactResponse, error) {
	if version < 1 {
		return protocol.ArtifactResponse{}, fmt.Errorf("version must be a positive integer")
	}
	body, err := json.Marshal(protocol.RestoreRequest{Version: version})
	if err != nil {
		return protocol.ArtifactResponse{}, fmt.Errorf("encoding restore request: %w", err)
	}
	var response protocol.ArtifactResponse
	requestPath, err := artifactPath(artifactID, "/restore")
	if err != nil {
		return protocol.ArtifactResponse{}, err
	}
	if err := c.doJSONWithContentType(ctx, http.MethodPost, requestPath, bytes.NewReader(body), "application/json", &response); err != nil {
		return protocol.ArtifactResponse{}, err
	}
	return response, nil
}

func (c *Client) PushData(ctx context.Context, artifactID, source string, data []byte) (protocol.DataResponse, error) {
	if err := protocol.ValidateArtifactID(artifactID); err != nil {
		return protocol.DataResponse{}, err
	}
	if err := protocol.ValidateDataSource(source); err != nil {
		return protocol.DataResponse{}, err
	}
	if len(data) > maxDataBody {
		return protocol.DataResponse{}, fmt.Errorf("runtime data is too large")
	}
	if !json.Valid(data) {
		return protocol.DataResponse{}, fmt.Errorf("data must be valid JSON")
	}
	requestPath, err := artifactPath(artifactID, "/data/"+url.PathEscape(source))
	if err != nil {
		return protocol.DataResponse{}, err
	}
	var response protocol.DataResponse
	if err := c.doJSONWithContentType(ctx, http.MethodPost, requestPath, bytes.NewReader(data), "application/json", &response); err != nil {
		return protocol.DataResponse{}, err
	}
	return response, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body io.Reader, result any) error {
	return c.doJSONWithContentType(ctx, method, path, body, "", result)
}

func (c *Client) doJSONWithContentType(ctx context.Context, method, path string, body io.Reader, contentType string, result any) error {
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, "http://artifactd"+path, body)
	if err != nil {
		return fmt.Errorf("creating daemon request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return &TransportError{Operation: "requesting artifactd", Err: err}
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			slog.Debug("closing daemon response", "error", closeErr)
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeError(response.Body, response.Status, response.StatusCode)
	}
	if result == nil {
		return nil
	}
	if err := decodeResponse(response.Body, result); err != nil {
		return fmt.Errorf("decoding daemon response: %w", err)
	}
	return nil
}

func artifactPath(artifactID, suffix string) (string, error) {
	if err := protocol.ValidateArtifactID(artifactID); err != nil {
		return "", err
	}
	return "/v1/artifacts/" + url.PathEscape(artifactID) + suffix, nil
}

func isDaemonUnavailable(err error) bool {
	return errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ENOTSOCK)
}

func manifestFromDirectory(directory string) (string, error) {
	path, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolving artifact directory: %w", err)
	}
	value, _, err := manifest.ValidateDirectory(path)
	if err != nil {
		return "", err
	}
	return value.ID, nil
}

func writeMultipart(writer *multipart.Writer, directory string) error {
	return walkFiles(directory, func(path, relative string) error {
		header := make(textproto.MIMEHeader)
		disposition := mime.FormatMediaType("form-data", map[string]string{
			"name":     "file",
			"filename": filepath.Base(relative),
		})
		header.Set("Content-Disposition", disposition)
		header.Set("Content-Type", "application/octet-stream")
		header.Set("X-Artifact-Path", relative)
		part, err := writer.CreatePart(header)
		if err != nil {
			return fmt.Errorf("creating upload part: %w", err)
		}
		return streamFile(part, path)
	})
}

func decodeResponse(body io.Reader, result any) error {
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(result); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("response contains multiple JSON values")
		}
		return err
	}
	return nil
}

func decodeError(body io.Reader, status string, statusCodes ...int) error {
	var response protocol.ErrorResponse
	_ = json.NewDecoder(body).Decode(&response)
	statusCode := 0
	if len(statusCodes) > 0 {
		statusCode = statusCodes[0]
	}
	return &DaemonError{StatusCode: statusCode, Status: status, Message: response.Error}
}

func walkFiles(directory string, visit func(string, string) error) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("reading artifact directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("artifact path is not a directory")
	}
	return filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking artifact: %w", err)
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("reading artifact file info: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special files are not allowed: %s", path)
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return fmt.Errorf("getting artifact path: %w", err)
		}
		return visit(path, filepath.ToSlash(relative))
	})
}

func streamFile(writer io.Writer, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reading artifact file info: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("special files are not allowed: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening artifact file: %w", err)
	}
	if _, err := io.Copy(writer, file); err != nil {
		_ = file.Close()
		return fmt.Errorf("uploading artifact file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing artifact file: %w", err)
	}
	return nil
}
