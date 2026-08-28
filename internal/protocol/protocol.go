package protocol

import (
	"encoding/json"
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

type WatchRequest struct {
	Directory string `json:"directory"`
}

type LiveResponse struct {
	ArtifactID string `json:"artifact_id"`
	Directory  string `json:"directory"`
	Status     string `json:"status"`
	Hash       string `json:"hash"`
	Error      string `json:"error,omitempty"`
	URL        string `json:"url,omitempty"`
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
