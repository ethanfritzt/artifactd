package daemon

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListenUnixRejectsActiveSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifactd.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("creating Unix listener: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("closing Unix listener: %v", err)
		}
	})

	_, err = listenUnix(path)
	if err == nil {
		t.Fatal("listenUnix() succeeded for an active socket")
	}
	if !strings.Contains(err.Error(), "control socket is already in use") {
		t.Fatalf("listenUnix() error = %q, want active socket error", err)
	}

	if _, err := net.Dial("unix", path); err != nil {
		t.Fatalf("active socket was removed or closed: %v", err)
	}
}

func TestListenUnixReplacesStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifactd.sock")
	staleListener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("creating stale Unix listener: %v", err)
	}
	if err := staleListener.Close(); err != nil {
		t.Fatalf("closing stale Unix listener: %v", err)
	}

	listener, err := listenUnix(path)
	if err != nil {
		t.Fatalf("listenUnix() = %v, want success for stale socket", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("closing Unix listener: %v", err)
		}
	})

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("new socket is missing: %v", err)
	}
}
