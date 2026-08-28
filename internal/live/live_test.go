package live

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"artifactd/internal/create"
	"artifactd/internal/storage"
)

func TestManagerRefreshesValidChanges(t *testing.T) {
	store, source := publishedArtifact(t)
	manager := NewManager(store)
	info, err := manager.Start(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != StatusReady {
		t.Fatalf("status = %q, want %q", info.Status, StatusReady)
	}

	events, unsubscribe, err := manager.Subscribe("demo")
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	defer manager.Close()

	updated := filepath.Join(source, "app.js")
	if err := os.WriteFile(updated, []byte("console.log(\"updated\");\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Type != "artifact_changed" {
			t.Fatalf("event type = %q, want artifact_changed", event.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for artifact change")
	}

	snapshot, release, ok := manager.Snapshot("demo")
	if !ok {
		t.Fatal("live snapshot is not active")
	}
	content, err := os.ReadFile(filepath.Join(snapshot.Path, "app.js"))
	release()
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "console.log(\"updated\");\n" {
		t.Fatalf("live content = %q", content)
	}
}

func TestManagerKeepsLastValidSnapshot(t *testing.T) {
	store, source := publishedArtifact(t)
	manager := NewManager(store)
	if _, err := manager.Start(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	events, unsubscribe, err := manager.Subscribe("demo")
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()

	manifestPath := filepath.Join(source, "artifact.json")
	if err := os.WriteFile(manifestPath, []byte("{"), 0o640); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Type != "artifact_error" {
			t.Fatalf("event type = %q, want artifact_error", event.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for artifact error")
	}

	info, err := manager.Info("demo")
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != StatusError {
		t.Fatalf("status = %q, want %q", info.Status, StatusError)
	}
	snapshot, release, ok := manager.Snapshot("demo")
	if !ok {
		t.Fatal("last valid snapshot was removed")
	}
	_, err = os.Stat(filepath.Join(snapshot.Path, "index.html"))
	release()
	if err != nil {
		t.Fatalf("last valid snapshot is unavailable: %v", err)
	}
}

func TestManagerRejectsSecondWatch(t *testing.T) {
	store, source := publishedArtifact(t)
	manager := NewManager(store)
	if _, err := manager.Start(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if _, err := manager.Start(t.Context(), source); err != ErrAlreadyWatching {
		t.Fatalf("second start error = %v, want %v", err, ErrAlreadyWatching)
	}
}

func publishedArtifact(t *testing.T) (*storage.Store, string) {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("closing store: %v", err)
		}
	})
	source := filepath.Join(t.TempDir(), "demo")
	if err := create.Run(create.Options{Directory: source, ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	staging, err := store.StageDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishStaged(t.Context(), staging, ""); err != nil {
		t.Fatal(err)
	}
	return store, source
}
