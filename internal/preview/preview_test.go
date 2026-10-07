package preview

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerCreatesAndExpiresDocument(t *testing.T) {
	now := time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC)
	manager := NewManager()
	manager.now = func() time.Time { return now }

	document, err := manager.Create("README.md", []byte("# Preview"))
	if err != nil {
		t.Fatal(err)
	}
	if document.ID == "" || document.Name != "README.md" {
		t.Fatalf("document = %+v", document)
	}
	if got, err := manager.Get(document.ID); err != nil || string(got.Content) != "# Preview" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}

	now = now.Add(Lifetime)
	if _, err := manager.Get(document.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired Get() error = %v, want ErrNotFound", err)
	}
}

func TestManagerRejectsOversizedDocument(t *testing.T) {
	manager := NewManager()
	if _, err := manager.Create("large.md", make([]byte, MaxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Create() error = %v, want ErrTooLarge", err)
	}
}

func TestEditablePreviewSaveIsScopedAndDetectsConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	original := []byte("# Original")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	document, err := manager.CreateEditable("notes.md", path, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(document.ID, "wrong-token", []byte("# Changed")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Save() error = %v, want ErrUnauthorized", err)
	}
	updated := []byte("# Updated")
	if err := manager.Save(document.ID, document.SaveToken, updated); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(updated) {
		t.Fatalf("saved source = %q, %v", got, err)
	}
	previewDocument, err := manager.Get(document.ID)
	if err != nil || string(previewDocument.Content) != string(updated) {
		t.Fatalf("updated preview = %q, %v", previewDocument.Content, err)
	}
	if err := os.WriteFile(path, []byte("# External edit"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(document.ID, document.SaveToken, []byte("# Lost update")); !errors.Is(err, ErrConflict) {
		t.Fatalf("Save() error = %v, want ErrConflict", err)
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("# Notes"), 0o640); err != nil {
		t.Fatal(err)
	}
	name, content, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if name != "notes.md" || string(content) != "# Notes" {
		t.Fatalf("LoadFile() = %q, %q", name, content)
	}
}

func TestLoadFileRejectsNonMarkdownAndSymlink(t *testing.T) {
	directory := t.TempDir()
	textPath := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(textPath, []byte("notes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadFile(textPath); err == nil {
		t.Fatal("LoadFile() accepted a non-Markdown file")
	}

	markdownPath := filepath.Join(directory, "notes.md")
	if err := os.WriteFile(markdownPath, []byte("notes"), 0o640); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(directory, "link.md")
	if err := os.Symlink(markdownPath, linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := LoadFile(linkPath); err == nil {
		t.Fatal("LoadFile() accepted a symlink")
	}
}
