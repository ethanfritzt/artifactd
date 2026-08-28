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
	"os"
	"path/filepath"
	"syscall"
	"time"

	"artifactd/internal/manifest"
	"artifactd/internal/model"
	"artifactd/internal/protocol"
)

const requestTimeout = 30 * time.Second

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
		if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
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
	var response protocol.ArtifactResponse
	requestPath := "/v1/artifacts/" + artifactID + "/archive"
	if err := c.doJSON(ctx, http.MethodPost, requestPath, nil, &response); err != nil {
		return protocol.ArtifactResponse{}, err
	}
	return response, nil
}

func (c *Client) Unarchive(ctx context.Context, artifactID string) (protocol.ArtifactResponse, error) {
	var response protocol.ArtifactResponse
	requestPath := "/v1/artifacts/" + artifactID + "/archive"
	if err := c.doJSON(ctx, http.MethodDelete, requestPath, nil, &response); err != nil {
		return protocol.ArtifactResponse{}, err
	}
	return response, nil
}

func (c *Client) Publish(ctx context.Context, directory string) (protocol.PublishResponse, error) {
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	contentType := multipartWriter.FormDataContentType()
	writerDone := make(chan error, 1)
	go func() {
		absoluteDirectory, absErr := filepath.Abs(directory)
		if absErr == nil {
			absErr = multipartWriter.WriteField("source_path", absoluteDirectory)
		}
		err := absErr
		if err == nil {
			err = writeMultipart(multipartWriter, directory)
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
		return protocol.PublishResponse{}, decodeError(response.Body, response.Status)
	}
	var result protocol.PublishResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return protocol.PublishResponse{}, fmt.Errorf("decoding publish response: %w", err)
	}
	return result, nil
}

func (c *Client) AddWorkspace(ctx context.Context, id, root string) (model.Workspace, error) {
	body, err := json.Marshal(protocol.WorkspaceRequest{ID: id, Root: root})
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

func (c *Client) Watch(ctx context.Context, directory string) (protocol.LiveResponse, error) {
	body, err := json.Marshal(protocol.WatchRequest{Directory: directory})
	if err != nil {
		return protocol.LiveResponse{}, fmt.Errorf("encoding watch request: %w", err)
	}
	var response protocol.LiveResponse
	artifact, err := manifestFromDirectory(directory)
	if err != nil {
		return protocol.LiveResponse{}, err
	}
	requestPath := "/v1/artifacts/" + artifact + "/watch"
	if err := c.doJSONWithContentType(ctx, http.MethodPost, requestPath, bytes.NewReader(body), "application/json", &response); err != nil {
		return protocol.LiveResponse{}, err
	}
	return response, nil
}

func (c *Client) Unwatch(ctx context.Context, artifactID string) error {
	requestPath := "/v1/artifacts/" + artifactID + "/watch"
	return c.doJSON(ctx, http.MethodDelete, requestPath, nil, nil)
}

func (c *Client) Live(ctx context.Context, artifactID string) (protocol.LiveResponse, error) {
	var response protocol.LiveResponse
	requestPath := "/v1/artifacts/" + artifactID + "/live"
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &response); err != nil {
		return protocol.LiveResponse{}, err
	}
	return response, nil
}

func (c *Client) Versions(ctx context.Context, artifactID string) ([]model.Version, error) {
	var response protocol.VersionsResponse
	requestPath := "/v1/artifacts/" + artifactID + "/versions"
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &response); err != nil {
		return nil, err
	}
	return response.Versions, nil
}

func (c *Client) Restore(ctx context.Context, artifactID string, version int) (protocol.ArtifactResponse, error) {
	body, err := json.Marshal(protocol.RestoreRequest{Version: version})
	if err != nil {
		return protocol.ArtifactResponse{}, fmt.Errorf("encoding restore request: %w", err)
	}
	var response protocol.ArtifactResponse
	requestPath := "/v1/artifacts/" + artifactID + "/restore"
	if err := c.doJSONWithContentType(ctx, http.MethodPost, requestPath, bytes.NewReader(body), "application/json", &response); err != nil {
		return protocol.ArtifactResponse{}, err
	}
	return response, nil
}

func (c *Client) PushData(ctx context.Context, artifactID, source string, data []byte) (protocol.DataResponse, error) {
	var response protocol.DataResponse
	requestPath := "/v1/artifacts/" + artifactID + "/data/" + source
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
		return fmt.Errorf("connecting to artifactd: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			slog.Debug("closing daemon response", "error", closeErr)
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeError(response.Body, response.Status)
	}
	if result == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decoding daemon response: %w", err)
	}
	return nil
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

func decodeError(body io.Reader, status string) error {
	var response protocol.ErrorResponse
	if err := json.NewDecoder(body).Decode(&response); err == nil && response.Error != "" {
		return fmt.Errorf("daemon returned %s: %s", status, response.Error)
	}
	return fmt.Errorf("daemon returned %s", status)
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
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return fmt.Errorf("getting artifact path: %w", err)
		}
		return visit(path, filepath.ToSlash(relative))
	})
}

func streamFile(writer io.Writer, path string) error {
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
