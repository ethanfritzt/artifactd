package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"artifactd/internal/create"
	"artifactd/internal/registry"
)

func TestEnsureDefaultIsIdempotent(t *testing.T) {
	root := t.TempDir()
	store, err := Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Logf("closing test store: %v", closeErr)
		}
	}()
	source := filepath.Join(root, "default")
	if err := create.Run(create.Options{Directory: source, ID: "artifactd-home", Name: "Artifactd Home"}); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDefault(t.Context(), source, "artifactd-home"); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDefault(t.Context(), source, "artifactd-home"); err != nil {
		t.Fatal(err)
	}
	artifact, _, err := store.Current(t.Context(), "artifactd-home")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.CurrentVersion != 1 {
		t.Fatalf("default version = %d, want 1", artifact.CurrentVersion)
	}
}

func TestPublishAssociatesWorkspace(t *testing.T) {
	root := t.TempDir()
	store, err := Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Logf("closing test store: %v", closeErr)
		}
	}()
	workspace, err := store.AddWorkspace(t.Context(), "vault", root)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "artifact")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	staging, err := store.NewStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyDirectory(source, staging); err != nil {
		t.Fatal(err)
	}
	result, err := store.PublishStaged(t.Context(), staging, source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact.WorkspaceID != workspace.ID || result.Version.WorkspaceID != workspace.ID {
		t.Fatalf("workspace association = %q/%q", result.Artifact.WorkspaceID, result.Version.WorkspaceID)
	}
	_, version, err := store.Current(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if version.WorkspaceID != "vault" {
		t.Fatalf("current workspace = %q", version.WorkspaceID)
	}
	if _, err := store.WorkspaceForPath(t.Context(), filepath.Join(t.TempDir(), "outside")); !errors.Is(err, registry.ErrWorkspaceNotFound) {
		t.Fatalf("outside workspace error = %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal(err)
	}
}
