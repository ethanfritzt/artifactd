package protocol

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"artifactd/internal/model"
)

type HealthResponse struct {
	Status string `json:"status"`
}

type ListResponse struct {
	Artifacts []model.Artifact `json:"artifacts"`
}

type WorkspaceRequest struct {
	ID   string `json:"id"`
	Root string `json:"root"`
}

type WorkspaceResponse struct {
	Workspace model.Workspace `json:"workspace"`
}

type WorkspacesResponse struct {
	Workspaces []model.Workspace `json:"workspaces"`
}

type PublishResponse struct {
	Artifact model.Artifact `json:"artifact"`
	Version  int            `json:"version"`
	URL      string         `json:"url"`
}

type PreviewResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type EditBeginRequest struct {
	Directory string `json:"directory"`
	Message   string `json:"message,omitempty"`
}

type EditProgressRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type EditSessionResponse struct {
	SessionID   string    `json:"session_id"`
	ArtifactID  string    `json:"artifact_id"`
	Directory   string    `json:"directory"`
	BaseVersion int       `json:"base_version"`
	Status      string    `json:"status"`
	Message     string    `json:"message,omitempty"`
	Error       string    `json:"error,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	URL         string    `json:"url,omitempty"`
}

type ArtifactResponse struct {
	Artifact model.Artifact `json:"artifact"`
	Version  int            `json:"version"`
	Entry    string         `json:"entry"`
}

type VersionsResponse struct {
	Versions []model.Version `json:"versions"`
}

type RestoreRequest struct {
	Version int `json:"version"`
}

type DataResponse struct {
	ArtifactID string          `json:"artifact_id"`
	Source     string          `json:"source"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Data       json.RawMessage `json:"data"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

var identifierPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidateArtifactID validates an ID used in a URL path or request body.
func ValidateArtifactID(id string) error {
	if !identifierPattern.MatchString(id) || len(id) > 63 {
		return fmt.Errorf("invalid artifact ID")
	}
	return nil
}

// ValidateDataSource validates the source segment used by runtime data APIs.
func ValidateDataSource(source string) error {
	if !identifierPattern.MatchString(source) || len(source) > 63 {
		return fmt.Errorf("invalid data source")
	}
	return nil
}

// ValidateAbsolutePath validates paths sent over the local control protocol.
// Paths are intentionally required to be absolute and normalized so that the
// daemon never has to interpret a caller-relative path.
func ValidateAbsolutePath(value string) error {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("path must be absolute")
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("path must be absolute")
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("path must be normalized")
	}
	return nil
}
