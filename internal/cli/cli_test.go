package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateUsesManagedSourceByDefault(t *testing.T) {
	dataDir := t.TempDir()
	workingDir := t.TempDir()
	oldWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWorkingDir) })

	command := NewCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--data-dir", dataDir, "create", "demo"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(dataDir, "sources", "demo")
	if got := filepath.Clean(strings.TrimSpace(output.String())); got != want {
		t.Fatalf("create output = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "artifact.json")); err != nil {
		t.Fatalf("managed artifact missing: %v", err)
	}
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("working directory was modified: %v", entries)
	}
}

func TestCreateSupportsExplicitLocalPath(t *testing.T) {
	dataDir := t.TempDir()
	localPath := filepath.Join(t.TempDir(), "demo")

	command := NewCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{
		"--data-dir", dataDir,
		"create", "--path", localPath, "--id", "demo",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(localPath, "artifact.json")); err != nil {
		t.Fatalf("local artifact missing: %v", err)
	}
	managed := filepath.Join(dataDir, "sources", "demo")
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("managed source exists, error = %v", err)
	}
}
