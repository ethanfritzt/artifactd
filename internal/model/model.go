package model

import "time"

const SystemArtifactID = "artifactd-home"

// Artifact is the current metadata for one logical artifact.
type Artifact struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description,omitempty"`
	Runtime        string     `json:"runtime"`
	WorkspaceID    string     `json:"workspace_id,omitempty"`
	CurrentVersion int        `json:"current_version"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Version describes one immutable published artifact version.
type Version struct {
	ArtifactID  string    `json:"artifact_id"`
	Number      int       `json:"version"`
	Path        string    `json:"-"`
	Manifest    []byte    `json:"-"`
	Entry       string    `json:"entry"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Workspace is a trusted local root used by artifact runtime providers.
type Workspace struct {
	ID        string    `json:"id"`
	Root      string    `json:"root"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
