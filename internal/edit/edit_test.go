package edit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"artifactd/internal/create"
	"artifactd/internal/storage"
)

func TestManagerEditLifecycle(t *testing.T) {
	store, source := publishedArtifact(t)
	manager := NewManager(store)
	defer manager.Close()

	events, unsubscribe, err := manager.Subscribe("demo")
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()

	session, err := manager.Begin(t.Context(), source, "Building dashboard")
	if err != nil {
		t.Fatal(err)
	}
	if session.BaseVersion != 1 || session.Status != StatusEditing {
		t.Fatalf("session = %+v", session)
	}
	if _, err := manager.Progress("demo", session.SessionID, "Finishing styles"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Commit(t.Context(), "demo", session.SessionID, source, session.BaseVersion, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	wantEvents := []string{"edit_started", "edit_progress", "edit_committing", "artifact_changed"}
	for _, want := range wantEvents {
		event := <-events
		if event.Type != want {
			t.Fatalf("event type = %q, want %q", event.Type, want)
		}
	}
	if _, err := manager.Status("demo"); !errors.Is(err, ErrNoEdit) {
		t.Fatalf("status error = %v, want ErrNoEdit", err)
	}
}

func TestManagerRejectsConcurrentAndStaleEdits(t *testing.T) {
	store, source := publishedArtifact(t)
	manager := NewManager(store)
	defer manager.Close()

	session, err := manager.Begin(t.Context(), source, "Editing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Begin(t.Context(), source, "Another edit"); !errors.Is(err, ErrAlreadyEditing) {
		t.Fatalf("second begin error = %v, want ErrAlreadyEditing", err)
	}
	if err := manager.Commit(t.Context(), "demo", session.SessionID, source, session.BaseVersion+1, func() error { return nil }); !errors.Is(err, ErrVersionChanged) {
		t.Fatalf("stale commit error = %v, want ErrVersionChanged", err)
	}
}

func TestManagerKeepsSessionAfterCommitFailure(t *testing.T) {
	store, source := publishedArtifact(t)
	manager := NewManager(store)
	defer manager.Close()

	session, err := manager.Begin(t.Context(), source, "Editing")
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("invalid artifact")
	if err := manager.Commit(t.Context(), "demo", session.SessionID, source, session.BaseVersion, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("commit error = %v, want %v", err, failure)
	}
	status, err := manager.Status("demo")
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != StatusError || status.Error != failure.Error() {
		t.Fatalf("status = %+v", status)
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
	if _, err := os.Stat(source); err != nil {
		t.Fatal(err)
	}
	return store, source
}
