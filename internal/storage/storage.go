package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"artifactd/internal/manifest"
	"artifactd/internal/model"
	"artifactd/internal/registry"
)

type Store struct {
	root     string
	registry *registry.Registry
}

func Open(root string) (*Store, error) {
	liveRoot := filepath.Join(root, "live")
	if err := os.RemoveAll(liveRoot); err != nil {
		return nil, fmt.Errorf("cleaning live snapshots: %w", err)
	}
	for _, dir := range []string{
		root,
		filepath.Join(root, "sources"),
		filepath.Join(root, "artifacts"),
		filepath.Join(root, "staging"),
		liveRoot,
		filepath.Join(liveRoot, ".staging"),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("creating storage directory: %w", err)
		}
		if err := os.Chmod(dir, 0o750); err != nil {
			return nil, fmt.Errorf("restricting storage directory permissions: %w", err)
		}
	}
	reg, err := registry.Open(filepath.Join(root, "registry.db"))
	if err != nil {
		return nil, err
	}
	return &Store{root: root, registry: reg}, nil
}

func (s *Store) Close() error {
	if err := s.registry.Close(); err != nil {
		return fmt.Errorf("closing storage: %w", err)
	}
	return nil
}

func (s *Store) StageDirectory(source string) (string, error) {
	return s.stageDirectory(source, filepath.Join(s.root, "staging"))
}

func (s *Store) StageLiveDirectory(source string) (string, error) {
	return s.stageDirectory(source, filepath.Join(s.root, "live", ".staging"))
}

func (s *Store) stageDirectory(source, stagingRoot string) (string, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return "", fmt.Errorf("reading source directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("source path is not a directory")
	}
	staging, err := s.newStaging(stagingRoot)
	if err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = s.RemoveStaging(staging)
		}
	}()
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walking source directory: %w", walkErr)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("getting source path: %w", err)
		}
		target := filepath.Join(staging, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("reading source file info: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special files are not allowed: %s", path)
		}
		if err := copyRegularFile(path, target); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	cleanup = false
	return staging, nil
}

func (s *Store) EnsureDefault(ctx context.Context, source, expectedID string) error {
	_, _, err := s.Current(ctx, expectedID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return err
	}
	m, _, err := manifest.ValidateDirectory(source)
	if err != nil {
		return fmt.Errorf("validating default artifact: %w", err)
	}
	if m.ID != expectedID {
		return fmt.Errorf("default artifact ID is %q, want %q", m.ID, expectedID)
	}
	staging, err := s.StageDirectory(source)
	if err != nil {
		return fmt.Errorf("staging default artifact: %w", err)
	}
	if _, err := s.PublishStaged(ctx, staging, ""); err != nil {
		if cleanupErr := s.RemoveStaging(staging); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
		return err
	}
	return nil
}

func copyRegularFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("creating staged directory: %w", err)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("opening source file: %w", err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("creating staged file: %w", err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return fmt.Errorf("copying source file: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing staged file: %w", closeErr)
	}
	return nil
}

func (s *Store) CreateLiveSnapshot(source string) (string, manifest.Manifest, string, error) {
	staging, err := s.StageLiveDirectory(source)
	if err != nil {
		return "", manifest.Manifest{}, "", err
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = s.RemoveStaging(staging)
		}
	}()

	currentManifest, _, err := manifest.ValidateDirectory(staging)
	if err != nil {
		return "", manifest.Manifest{}, "", err
	}
	hash, err := contentHash(staging)
	if err != nil {
		return "", manifest.Manifest{}, "", fmt.Errorf("hashing live artifact: %w", err)
	}
	path, err := s.CommitLiveSnapshot(staging, currentManifest.ID)
	if err != nil {
		return "", manifest.Manifest{}, "", err
	}
	keepStaging = true
	return path, currentManifest, hash, nil
}

func (s *Store) CommitLiveSnapshot(staging, artifactID string) (string, error) {
	if !manifest.ValidID(artifactID) {
		return "", fmt.Errorf("invalid live artifact ID")
	}
	liveRoot := filepath.Join(s.root, "live")
	snapshots := filepath.Join(liveRoot, artifactID, "snapshots")
	if err := os.MkdirAll(snapshots, 0o750); err != nil {
		return "", fmt.Errorf("creating live snapshot directory: %w", err)
	}
	finalPath, err := os.MkdirTemp(snapshots, "snapshot-")
	if err != nil {
		return "", fmt.Errorf("allocating live snapshot path: %w", err)
	}
	if err := os.Remove(finalPath); err != nil {
		return "", fmt.Errorf("preparing live snapshot path: %w", err)
	}
	if err := os.Rename(staging, finalPath); err != nil {
		return "", fmt.Errorf("moving live snapshot: %w", err)
	}
	return finalPath, nil
}

func (s *Store) RemoveLiveSnapshot(snapshot string) error {
	liveRoot := filepath.Join(s.root, "live")
	absolute, err := filepath.Abs(snapshot)
	if err != nil {
		return fmt.Errorf("resolving live snapshot: %w", err)
	}
	relative, err := filepath.Rel(liveRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("live snapshot is outside storage")
	}
	if relative == "." || relative == "" {
		return fmt.Errorf("cannot remove live snapshot root")
	}
	if err := os.RemoveAll(absolute); err != nil {
		return fmt.Errorf("removing live snapshot: %w", err)
	}
	return nil
}

func (s *Store) NewStaging() (string, error) {
	return s.newStaging(filepath.Join(s.root, "staging"))
}

func (s *Store) newStaging(root string) (string, error) {
	path, err := os.MkdirTemp(root, "publish-*")
	if err != nil {
		return "", fmt.Errorf("creating staging directory: %w", err)
	}
	if err := os.Chmod(path, 0o750); err != nil {
		if removeErr := os.RemoveAll(path); removeErr != nil {
			return "", errors.Join(
				fmt.Errorf("restricting staging permissions: %w", err),
				fmt.Errorf("removing failed staging directory: %w", removeErr),
			)
		}
		return "", fmt.Errorf("restricting staging permissions: %w", err)
	}
	return path, nil
}

func (s *Store) RemoveStaging(path string) error {
	if path == "" {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("removing staging directory: %w", err)
	}
	return nil
}

func (s *Store) PublishStaged(ctx context.Context, staging, sourcePath string) (registry.PublishResult, error) {
	m, raw, err := manifest.ValidateDirectory(staging)
	if err != nil {
		return registry.PublishResult{}, err
	}
	hash, err := contentHash(staging)
	if err != nil {
		return registry.PublishResult{}, err
	}
	workspaceID := ""
	if sourcePath != "" {
		workspace, workspaceErr := s.registry.WorkspaceForPath(ctx, sourcePath)
		if workspaceErr != nil && !errors.Is(workspaceErr, registry.ErrWorkspaceNotFound) {
			return registry.PublishResult{}, workspaceErr
		}
		if workspaceErr == nil {
			workspaceID = workspace.ID
		}
	}

	result, err := s.registry.Publish(ctx, m, raw, hash, workspaceID, func(version int, relativePath string) error {
		finalPath, err := s.resolve(relativePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o750); err != nil {
			return fmt.Errorf("creating version directory: %w", err)
		}
		if err := os.Rename(staging, finalPath); err != nil {
			return fmt.Errorf("moving staged version %d: %w", version, err)
		}
		return nil
	})
	if err != nil {
		return registry.PublishResult{}, err
	}
	return result, nil
}

func (s *Store) List(ctx context.Context, includeArchived bool) ([]model.Artifact, error) {
	return s.registry.List(ctx, includeArchived)
}

func (s *Store) Archive(ctx context.Context, id string) error {
	return s.registry.Archive(ctx, id)
}

func (s *Store) Unarchive(ctx context.Context, id string) error {
	return s.registry.Unarchive(ctx, id)
}

func (s *Store) ListVersions(ctx context.Context, id string) ([]model.Version, error) {
	versions, err := s.registry.ListVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	for index := range versions {
		versions[index].Path, err = s.resolve(versions[index].Path)
		if err != nil {
			return nil, err
		}
	}
	return versions, nil
}

func (s *Store) Restore(ctx context.Context, id string, version int) error {
	return s.registry.Restore(ctx, id, version)
}

func (s *Store) AddWorkspace(ctx context.Context, id, root string) (model.Workspace, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return model.Workspace{}, fmt.Errorf("resolving workspace root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return model.Workspace{}, fmt.Errorf("reading workspace root: %w", err)
	}
	if !info.IsDir() {
		return model.Workspace{}, fmt.Errorf("workspace root is not a directory")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return model.Workspace{}, fmt.Errorf("resolving workspace root symlinks: %w", err)
	}
	return s.registry.AddWorkspace(ctx, id, resolved)
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	return s.registry.ListWorkspaces(ctx)
}

func (s *Store) WorkspaceForPath(ctx context.Context, path string) (model.Workspace, error) {
	return s.registry.WorkspaceForPath(ctx, path)
}

func (s *Store) Workspace(ctx context.Context, id string) (model.Workspace, error) {
	return s.registry.Workspace(ctx, id)
}

func (s *Store) Current(ctx context.Context, id string) (model.Artifact, model.Version, error) {
	artifact, version, err := s.registry.Current(ctx, id)
	if err != nil {
		return model.Artifact{}, model.Version{}, err
	}
	version.Path, err = s.resolve(version.Path)
	if err != nil {
		return model.Artifact{}, model.Version{}, err
	}
	return artifact, version, nil
}

func (s *Store) resolve(relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("storage path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage path escapes root")
	}
	path := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return "", fmt.Errorf("checking storage path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage path escapes root")
	}
	return path, nil
}

func contentHash(root string) (string, error) {
	hasher := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking staged artifact: %w", err)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("getting staged path: %w", err)
		}
		if _, err := io.WriteString(hasher, filepath.ToSlash(rel)+"\x00"); err != nil {
			return fmt.Errorf("hashing staged path: %w", err)
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening staged file: %w", err)
		}
		if _, err := io.Copy(hasher, file); err != nil {
			if closeErr := file.Close(); closeErr != nil {
				return errors.Join(
					fmt.Errorf("hashing staged file: %w", err),
					fmt.Errorf("closing staged file: %w", closeErr),
				)
			}
			return fmt.Errorf("hashing staged file: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("closing staged file: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
