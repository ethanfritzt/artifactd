package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSourcePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifactd")
	path, err := ManagedSourcePath(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "sources", "demo")
	if path != want {
		t.Fatalf("managed source path = %q, want %q", path, want)
	}
}

func TestManagedSourcePathRejectsInvalidID(t *testing.T) {
	_, err := ManagedSourcePath(t.TempDir(), "../demo")
	if err == nil || !strings.Contains(err.Error(), "invalid artifact ID") {
		t.Fatalf("ManagedSourcePath() error = %v, want invalid ID error", err)
	}
}
