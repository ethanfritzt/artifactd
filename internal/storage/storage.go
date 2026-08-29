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
	root, err := canonicalRoot(root)
	if err != nil {
		return nil, err
	}
	if err := ensureDirectory(root, root); err != nil {
		return nil, fmt.Errorf("creating storage root: %w", err)
	}

	// Live snapshots and publish staging are disposable. Cleaning both on
	// startup bounds abandoned data after a process crash. The managed-path
	// checks make this cleanup safe even if the data directory was tampered
	// with between runs.
	liveRoot := filepath.Join(root, "live")
	stagingRoot := filepath.Join(root, "staging")
	for _, dir := range []string{liveRoot, stagingRoot} {
		if err := removeManagedDirectory(root, dir); err != nil {
			return nil, fmt.Errorf("cleaning storage directory: %w", err)
		}
	}
	for _, dir := range []string{
		filepath.Join(root, "sources"),
		filepath.Join(root, "artifacts"),
		stagingRoot,
		liveRoot,
		filepath.Join(liveRoot, ".staging"),
	} {
		if err := ensureDirectory(root, dir); err != nil {
			return nil, fmt.Errorf("creating storage directory: %w", err)
		}
	}
	reg, err := registry.Open(filepath.Join(root, "registry.db"))
	if err != nil {
		return nil, err
	}
	store := &Store{root: root, registry: reg}
	if err := store.reconcileVersions(); err != nil {
		if closeErr := reg.Close(); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return store, nil
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
	var files int
	var bytesTotal int64
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
		files++
		if files > manifest.MaxArtifactFiles {
			return fmt.Errorf("staging artifact: too many files (limit %d)", manifest.MaxArtifactFiles)
		}
		remaining := int64(manifest.MaxArtifactBytes) - bytesTotal
		if info.Size() < 0 || info.Size() > remaining {
			return fmt.Errorf("staging artifact: size exceeds %d bytes", manifest.MaxArtifactBytes)
		}
		bytesTotal += info.Size()
		if err := copyRegularFile(path, target, remaining); err != nil {
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

func copyRegularFile(source, target string, limit int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("creating staged directory: %w", err)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("opening source file: %w", err)
	}
	defer func() { _ = input.Close() }()
	if info, statErr := input.Stat(); statErr != nil {
		return fmt.Errorf("reading source file: %w", statErr)
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("source file is not regular: %s", source)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("creating staged file: %w", err)
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("copying source file: %w", copyErr)
	}
	if written > limit {
		_ = os.Remove(target)
		return fmt.Errorf("source file exceeds staging limit of %d bytes", limit)
	}
	if closeErr != nil {
		_ = os.Remove(target)
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
	if err := s.validateStagingPath(staging, filepath.Join(s.root, "live", ".staging")); err != nil {
		return "", err
	}
	liveRoot := filepath.Join(s.root, "live")
	snapshots := filepath.Join(liveRoot, artifactID, "snapshots")
	if err := ensureDirectory(s.root, snapshots); err != nil {
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
	if err != nil || relative == "." || relative == "" || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("live snapshot is outside storage")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 3 || !manifest.ValidID(parts[0]) || parts[1] != "snapshots" || !strings.HasPrefix(parts[2], "snapshot-") {
		return fmt.Errorf("invalid live snapshot path")
	}
	if err := rejectSymlinkComponents(s.root, absolute); err != nil {
		return err
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
	for _, root := range []string{
		filepath.Join(s.root, "staging"),
		filepath.Join(s.root, "live", ".staging"),
	} {
		if err := s.validateStagingPath(path, root); err == nil {
			if err := rejectSymlinkComponents(s.root, path); err != nil {
				return err
			}
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("removing staging directory: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("staging path is outside storage")
}

func (s *Store) PublishStaged(ctx context.Context, staging, sourcePath string) (registry.PublishResult, error) {
	if err := s.validateStagingPath(staging, filepath.Join(s.root, "staging")); err != nil {
		return registry.PublishResult{}, err
	}
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

	result, err := s.registry.Publish(ctx, m, raw, hash, workspaceID, func(version int, relativePath string) (func() error, error) {
		finalPath, err := s.resolve(relativePath)
		if err != nil {
			return nil, err
		}
		if err := ensureDirectory(s.root, filepath.Dir(finalPath)); err != nil {
			return nil, fmt.Errorf("creating version directory: %w", err)
		}
		if _, err := os.Lstat(finalPath); err == nil {
			return nil, fmt.Errorf("version %d already exists", version)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("checking version %d path: %w", version, err)
		}
		if err := os.Rename(staging, finalPath); err != nil {
			return nil, fmt.Errorf("moving staged version %d: %w", version, err)
		}
		return func() error {
			if err := rejectSymlinkComponents(s.root, finalPath); err != nil {
				return err
			}
			if err := os.RemoveAll(finalPath); err != nil {
				return fmt.Errorf("removing installed version: %w", err)
			}
			return nil
		}, nil
	})
	if err != nil {
		return registry.PublishResult{}, err
	}
	return result, nil
}

func (s *Store) reconcileVersions() error {
	artifacts, err := s.registry.List(context.Background(), true)
	if err != nil {
		return fmt.Errorf("listing versions for storage reconciliation: %w", err)
	}
	expected := make(map[string]struct{})
	for _, artifact := range artifacts {
		versions, versionsErr := s.registry.ListVersions(context.Background(), artifact.ID)
		if errors.Is(versionsErr, registry.ErrNotFound) && artifact.CurrentVersion == 0 {
			continue
		}
		if versionsErr != nil {
			return fmt.Errorf("listing versions for %s: %w", artifact.ID, versionsErr)
		}
		for _, version := range versions {
			path, resolveErr := s.resolve(version.Path)
			if resolveErr != nil {
				return fmt.Errorf("resolving version %s/%d: %w", artifact.ID, version.Number, resolveErr)
			}
			expected[filepath.Clean(path)] = struct{}{}
		}
	}

	artifactsRoot := filepath.Join(s.root, "artifacts")
	err = filepath.WalkDir(artifactsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walking stored artifacts: %w", walkErr)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("stored artifacts contain symlink: %s", path)
		}
		if !entry.IsDir() || path == artifactsRoot {
			return nil
		}
		relative, relErr := filepath.Rel(artifactsRoot, path)
		if relErr != nil {
			return fmt.Errorf("getting stored artifact path: %w", relErr)
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) != 3 || parts[1] != "versions" || !manifest.ValidID(parts[0]) {
			return nil
		}
		if _, ok := expected[filepath.Clean(path)]; ok {
			return filepath.SkipDir
		}
		if err := removeManagedDirectory(s.root, path); err != nil {
			return fmt.Errorf("removing orphaned version: %w", err)
		}
		return filepath.SkipDir
	})
	if err != nil {
		return err
	}
	return nil
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
	if relativePath == "" || strings.ContainsRune(relativePath, '\x00') || filepath.IsAbs(relativePath) || strings.Contains(relativePath, "\\") {
		return "", fmt.Errorf("storage path must be local and relative")
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.ToSlash(clean) != relativePath {
		return "", fmt.Errorf("storage path escapes root")
	}
	path := filepath.Join(s.root, clean)
	if err := rejectSymlinkComponents(s.root, path); err != nil {
		return "", err
	}
	return path, nil
}

func canonicalRoot(root string) (string, error) {
	if root == "" {
		return "", errors.New("storage root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving storage root: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if absolute == filepath.Dir(absolute) {
		return "", errors.New("storage root must not be the filesystem root")
	}
	info, err := os.Lstat(absolute)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("storage root must not be a symlink")
		}
		if !info.IsDir() {
			return "", errors.New("storage root is not a directory")
		}
		return filepath.EvalSymlinks(absolute)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("reading storage root: %w", err)
	}
	return absolute, nil
}

func ensureDirectory(root, path string) error {
	if err := rejectSymlinkComponents(root, path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return err
	}
	if err := rejectSymlinkComponents(root, path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o750); err != nil {
		return err
	}
	return nil
}

func removeManagedDirectory(root, path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("managed directory is not a directory: %s", path)
	}
	if err := rejectSymlinkComponents(root, path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func rejectSymlinkComponents(root, path string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving storage root: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving storage path: %w", err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("storage path escapes root")
	}
	current := root
	if err := rejectSymlink(current); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// A new storage root may not exist yet. Its missing descendants
		// cannot contain a symlink, so checking the nearest existing parent
		// is sufficient.
		for {
			parent := filepath.Dir(current)
			if parent == current {
				return nil
			}
			current = parent
			if parentErr := rejectSymlink(current); parentErr == nil {
				return nil
			} else if !errors.Is(parentErr, os.ErrNotExist) {
				return parentErr
			}
		}
	}
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		if err := rejectSymlink(current); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("storage path contains symlink: %s", path)
	}
	return nil
}

func (s *Store) validateStagingPath(path, root string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving staging path: %w", err)
	}
	stagingRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving staging root: %w", err)
	}
	relative, err := filepath.Rel(stagingRoot, absolute)
	if err != nil || relative == "." || relative == "" || strings.Contains(relative, string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("staging path is invalid")
	}
	if !strings.HasPrefix(filepath.Base(relative), "publish-") {
		return fmt.Errorf("staging path is invalid")
	}
	if err := rejectSymlinkComponents(s.root, absolute); err != nil {
		return err
	}
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading staging path: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("staging path is not a directory")
	}
	return nil
}

func contentHash(root string) (string, error) {
	hasher := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking staged artifact: %w", err)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("reading staged file info: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special files are not allowed: %s", path)
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
