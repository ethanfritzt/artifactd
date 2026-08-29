package live

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
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

func TestManagerRejectsArchivedArtifact(t *testing.T) {
	store, source := publishedArtifact(t)
	if err := store.Archive(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store)
	defer manager.Close()

	if _, err := manager.Start(t.Context(), source); !errors.Is(err, ErrArchivedArtifact) {
		t.Fatalf("archived start error = %v, want ErrArchivedArtifact", err)
	}
}

func TestManagerStopsArchivedSessionOnChange(t *testing.T) {
	store, source := publishedArtifact(t)
	manager := NewManager(store)
	if _, err := manager.Start(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if err := store.Archive(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "app.js"), []byte("console.log(\"archived\");\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := manager.Info("demo"); errors.Is(err, ErrNotWatching) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("archived session remained active")
}

func TestSnapshotRetryHonorsCancellation(t *testing.T) {
	store, _ := publishedArtifact(t)
	manager := NewManager(store)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	started := time.Now()
	_, _, _, err := manager.createSnapshotWithRetry(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retry error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("canceled retry took %s", elapsed)
	}
}

func TestManagerBoundsSubscribersAcrossArtifacts(t *testing.T) {
	manager := NewManager(nil)
	unsubscribers := make([]func(), 0, maxSubscribers)
	for i := 0; i < maxSubscribers; i++ {
		_, unsubscribe, err := manager.Subscribe(filepath.Join("artifact", string(rune('a'+i))))
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		unsubscribers = append(unsubscribers, unsubscribe)
	}
	if _, _, err := manager.Subscribe("one-too-many"); !errors.Is(err, ErrTooManyClients) {
		t.Fatalf("overflow subscribe error = %v, want ErrTooManyClients", err)
	}
	for _, unsubscribe := range unsubscribers {
		unsubscribe()
	}
	if _, unsubscribe, err := manager.Subscribe("after-cleanup"); err != nil {
		t.Fatalf("subscribe after cleanup: %v", err)
	} else {
		unsubscribe()
	}
	manager.Close()
}

func TestManagerCloseIsIdempotentAndClosesSubscribers(t *testing.T) {
	manager := NewManager(nil)
	events, _, err := manager.Subscribe("demo")
	if err != nil {
		t.Fatal(err)
	}
	var closeGroup sync.WaitGroup
	for i := 0; i < 8; i++ {
		closeGroup.Add(1)
		go func() {
			defer closeGroup.Done()
			manager.Close()
		}()
	}
	closeGroup.Wait()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("subscriber channel is still open")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel was not closed")
	}
	if _, _, err := manager.Subscribe("after-close"); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("subscribe after close error = %v, want ErrManagerClosed", err)
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
