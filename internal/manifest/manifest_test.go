package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDirectory(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		entry    string
		wantErr  bool
	}{
		{
			name:     "valid artifact",
			manifest: `{"artifactVersion":1,"id":"demo","name":"Demo","entry":"index.html","runtime":"web"}`,
			entry:    "index.html",
		},
		{
			name:     "missing id",
			manifest: `{"artifactVersion":1,"name":"Demo","entry":"index.html"}`,
			entry:    "index.html",
			wantErr:  true,
		},
		{
			name:     "entry escapes root",
			manifest: `{"artifactVersion":1,"id":"demo","name":"Demo","entry":"../index.html"}`,
			entry:    "index.html",
			wantErr:  true,
		},
		{
			name:     "unknown field",
			manifest: `{"artifactVersion":1,"id":"demo","name":"Demo","entry":"index.html","extra":true}`,
			entry:    "index.html",
			wantErr:  true,
		},
		{
			name:     "structured specification",
			manifest: `{"specVersion":1,"artifact":{"id":"demo","name":"Demo"},"code":{"format":"files","entry":"index.html"},"runtime":{"id":"web-static","version":1}}`,
			entry:    "index.html",
		},
		{
			name:     "unsupported runtime",
			manifest: `{"specVersion":1,"artifact":{"id":"demo","name":"Demo"},"code":{"entry":"index.html"},"runtime":{"id":"node","version":1}}`,
			entry:    "index.html",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "artifact.json"), []byte(tt.manifest), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tt.entry), []byte("<html></html>"), 0o640); err != nil {
				t.Fatal(err)
			}
			parsed, _, err := ValidateDirectory(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDirectory() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && parsed.Entry != tt.entry {
				t.Fatalf("entry = %q, want %q", parsed.Entry, tt.entry)
			}
		})
	}
}

func TestValidateDirectoryRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "artifact.json"), []byte(`{"artifactVersion":1,"id":"demo","name":"Demo","entry":"index.html"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(target, []byte("outside"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "index.html")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := ValidateDirectory(dir); err == nil {
		t.Fatal("ValidateDirectory() accepted a symlink")
	}
}
