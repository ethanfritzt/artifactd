package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"artifactd/internal/create"
	"artifactd/internal/manifest"
	"artifactd/internal/model"
	"artifactd/internal/registry"
)

func TestPublishStagedCreatesImmutableVersions(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("closing test store: %v", err)
		}
	}()

	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}

	first := publishSource(t, store, source)
	if first.Version.Number != 1 {
		t.Fatalf("first version = %d, want 1", first.Version.Number)
	}
	firstPath := filepath.Join(root, filepath.FromSlash(first.Version.Path))
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("first version missing: %v", err)
	}

	if err := os.WriteFile(filepath.Join(source, "app.js"), []byte("console.log('version 2')"), 0o640); err != nil {
		t.Fatal(err)
	}
	second := publishSource(t, store, source)
	if second.Version.Number != 2 {
		t.Fatalf("second version = %d, want 2", second.Version.Number)
	}

	current, version, err := store.Current(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentVersion != 2 || version.Number != 2 {
		t.Fatalf("current version = %d/%d, want 2/2", current.CurrentVersion, version.Number)
	}
	content, err := os.ReadFile(filepath.Join(version.Path, "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "console.log('version 2')" {
		t.Fatalf("current content = %q", content)
	}
	oldContent, err := os.ReadFile(filepath.Join(firstPath, "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(oldContent) == string(content) {
		t.Fatal("version 1 was mutated")
	}
}

func TestStageDirectoryEnforcesFileAndByteLimits(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	t.Run("file count", func(t *testing.T) {
		source := t.TempDir()
		for i := 0; i <= manifest.MaxArtifactFiles; i++ {
			if err := os.WriteFile(filepath.Join(source, fmt.Sprintf("file-%d", i)), []byte("x"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.StageDirectory(source); err == nil {
			t.Fatal("StageDirectory() accepted too many files")
		}
		assertNoStagingDirectories(t, store.root)
	})

	t.Run("byte size", func(t *testing.T) {
		source := t.TempDir()
		file := filepath.Join(source, "large.bin")
		if err := os.WriteFile(file, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(file, manifest.MaxArtifactBytes+1); err != nil {
			t.Fatal(err)
		}
		if _, err := store.StageDirectory(source); err == nil {
			t.Fatal("StageDirectory() accepted an oversized artifact")
		}
		assertNoStagingDirectories(t, store.root)
	})
}

func TestOpenRemovesOrphanedVersionDirectory(t *testing.T) {
	root := t.TempDir()
	orphan := filepath.Join(root, "artifacts", "demo", "versions", "99")
	if err := os.MkdirAll(orphan, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "partial"), []byte("incomplete"), 0o640); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphaned version remains: %v", err)
	}
}

func TestOpenRemovesAbandonedStaging(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "staging", "publish-old")
	if err := os.MkdirAll(stale, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "partial"), []byte("incomplete"), 0o640); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	assertNoStagingDirectories(t, root)
}

func TestOpenRejectsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("Open() accepted a symlink storage root")
	}
	if _, err := os.Stat(filepath.Join(target, "registry.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink target was modified: %v", err)
	}
}

func TestRemoveStagingRejectsPathsOutsideStorage(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	outside := filepath.Join(t.TempDir(), "do-not-delete")
	if err := os.WriteFile(outside, []byte("sentinel"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveStaging(outside); err == nil {
		t.Fatal("RemoveStaging() accepted an outside path")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside path was changed: %v", err)
	}
}

func TestResolveRejectsSymlinkComponents(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(store.root, "artifacts", "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := store.resolve("artifacts/alias/file"); err == nil {
		t.Fatal("resolve() accepted a symlink component")
	}
}

func assertNoStagingDirectories(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "staging"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging directories remain: %+v", entries)
	}
}

func TestArchivePreservesArtifactAndFiltersList(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("closing test store: %v", err)
		}
	}()

	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	result := publishSource(t, store, source)
	path := filepath.Join(root, filepath.FromSlash(result.Version.Path))

	if err := store.Archive(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	artifact, _, err := store.Current(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ArchivedAt == nil {
		t.Fatal("archived artifact has no archive timestamp")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("archived version missing: %v", err)
	}
	active, err := store.List(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active artifacts = %+v, want none", active)
	}
	all, err := store.List(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ArchivedAt == nil {
		t.Fatalf("all artifacts = %+v, want archived demo", all)
	}

	if err := store.Unarchive(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	artifact, _, err = store.Current(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ArchivedAt != nil {
		t.Fatal("unarchived artifact still has archive timestamp")
	}
	active, err = store.List(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "demo" {
		t.Fatalf("active artifacts after unarchive = %+v", active)
	}
}

func TestArchiveRejectsSystemArtifact(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("closing test store: %v", err)
		}
	}()

	if err := store.Archive(t.Context(), model.SystemArtifactID); !errors.Is(err, registry.ErrProtectedArtifact) {
		t.Fatalf("archive system artifact error = %v, want %v", err, registry.ErrProtectedArtifact)
	}
}

func TestRestoreSelectsImmutableVersion(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("closing test store: %v", err)
		}
	}()

	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	publishSource(t, store, source)
	if err := os.WriteFile(filepath.Join(source, "app.js"), []byte("version 2"), 0o640); err != nil {
		t.Fatal(err)
	}
	publishSource(t, store, source)

	versions, err := store.ListVersions(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Number != 2 || versions[1].Number != 1 {
		t.Fatalf("versions = %+v", versions)
	}
	if err := store.Restore(t.Context(), "demo", 1); err != nil {
		t.Fatal(err)
	}
	artifact, version, err := store.Current(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.CurrentVersion != 1 || version.Number != 1 {
		t.Fatalf("current version = %d/%d, want 1/1", artifact.CurrentVersion, version.Number)
	}
	content, err := os.ReadFile(filepath.Join(version.Path, "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) == "version 2" {
		t.Fatal("restore selected the newer version")
	}
}

func publishSource(t *testing.T, store *Store, source string) registry.PublishResult {
	t.Helper()
	staging, err := store.NewStaging()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyDirectory(source, staging); err != nil {
		t.Fatal(err)
	}
	result, err := store.PublishStaged(t.Context(), staging, "")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
		if err != nil {
			if closeErr := input.Close(); closeErr != nil {
				return errors.Join(err, closeErr)
			}
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return outputCloseErr
	})
}
