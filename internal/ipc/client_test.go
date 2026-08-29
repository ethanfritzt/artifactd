package ipc

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTransportErrorReportsUnavailableDaemon(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "missing.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := NewClient(socket).List(ctx, false)
	if err == nil {
		t.Fatal("List returned nil, want connection error")
	}
	if !IsDaemonUnavailable(err) || !errors.Is(err, ErrDaemonUnavailable) {
		t.Fatalf("error = %v, want daemon-unavailable error", err)
	}
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("error = %T, want TransportError", err)
	}
}

func TestPublishRejectsEmptyDirectory(t *testing.T) {
	if _, err := NewClient(filepath.Join(t.TempDir(), "artifactd.sock")).Publish(context.Background(), ""); err == nil {
		t.Fatal("Publish(\"\") returned nil")
	}
}
