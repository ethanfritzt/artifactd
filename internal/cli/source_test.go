package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveArtifactDirectory(t *testing.T) {
	dataDir := t.TempDir()
	managed := filepath.Join(dataDir, "sources", "demo")
	if err := os.MkdirAll(managed, 0o750); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		argument string
		want     string
		prepare  func() error
	}{
		{
			name:     "managed static source",
			argument: "demo",
			want:     managed,
		},
		{
			name:     "existing directory",
			argument: filepath.Join(dataDir, "local"),
			want:     filepath.Join(dataDir, "local"),
			prepare: func() error {
				return os.Mkdir(filepath.Join(dataDir, "local"), 0o750)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.prepare != nil {
				if err := tt.prepare(); err != nil {
					t.Fatal(err)
				}
			}
			got, err := resolveArtifactDirectory(tt.argument, dataDir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("resolved directory = %q, want %q", got, tt.want)
			}
		})
	}
}
