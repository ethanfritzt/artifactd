package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"artifactd/internal/config"
	"artifactd/internal/manifest"
)

// resolveArtifactDirectory accepts both a source path and an artifact ID. Existing
// paths win so legacy directory-based workflows remain unchanged.
func resolveArtifactDirectory(argument, dataDir string) (string, error) {
	if argument == "" {
		return "", fmt.Errorf("artifact directory or ID is required")
	}
	if info, err := os.Stat(argument); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("artifact path is not a directory: %s", argument)
		}
		return filepath.Clean(argument), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("checking artifact path: %w", err)
	}

	if !manifest.ValidID(argument) {
		return filepath.Clean(argument), nil
	}
	managed, err := config.ManagedSourcePath(dataDir, argument)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(managed); err == nil && info.IsDir() {
		if _, manifestErr := os.Stat(filepath.Join(managed, "artifact.json")); manifestErr == nil {
			return managed, nil
		}
		return managed, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("checking managed artifact source: %w", err)
	}
	return managed, nil
}
