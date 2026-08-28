package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"artifactd/internal/model"
)

func TestProviderListScopesToWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "notes"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "today.md"), []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	provider := NewProvider()
	nodes, err := provider.List(model.Workspace{ID: "vault", Root: root}, "notes", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Path != "notes/today.md" || nodes[0].Kind != "file" {
		t.Fatalf("nodes = %+v", nodes)
	}
	if _, err := provider.List(model.Workspace{ID: "vault", Root: root}, "../", 2); err == nil {
		t.Fatal("List() accepted a path outside the workspace")
	}
}

func TestProviderDoesNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "outside.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	provider := NewProvider()
	nodes, err := provider.List(model.Workspace{ID: "vault", Root: root}, ".", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Kind != "symlink" {
		t.Fatalf("nodes = %+v", nodes)
	}
}
