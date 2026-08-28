package create

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/react
var reactTemplates embed.FS

const (
	StaticTemplate = "static"
	ReactTemplate  = "react"
	GraphFeature   = "graph"
)

func runReact(options Options) error {
	if err := prepareDirectory(options); err != nil {
		return err
	}
	if err := copyEmbeddedTemplate(options); err != nil {
		return err
	}
	if err := writeManifest(filepath.Join(options.Directory, "public", "artifact.json"), options); err != nil {
		return err
	}
	if hasFeature(options.Features, GraphFeature) {
		if err := addGraphDependency(filepath.Join(options.Directory, "package.json")); err != nil {
			return err
		}
	}
	return nil
}

func prepareDirectory(options Options) error {
	if err := os.MkdirAll(options.Directory, 0o750); err != nil {
		return fmt.Errorf("creating artifact directory: %w", err)
	}
	info, err := os.Stat(options.Directory)
	if err != nil {
		return fmt.Errorf("checking artifact directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("artifact path is not a directory")
	}
	entries, err := os.ReadDir(options.Directory)
	if err != nil {
		return fmt.Errorf("checking artifact directory: %w", err)
	}
	if len(entries) > 0 && !options.Force {
		return fmt.Errorf("artifact directory is not empty: %s", options.Directory)
	}
	return nil
}

func copyEmbeddedTemplate(options Options) error {
	paths := []string{"templates/react/base"}
	if hasFeature(options.Features, GraphFeature) {
		paths = append(paths, "templates/react/graph")
	}
	for _, templatePath := range paths {
		if err := fs.WalkDir(reactTemplates, templatePath, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("reading React template: %w", err)
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(templatePath, path)
			if err != nil {
				return fmt.Errorf("getting React template path: %w", err)
			}
			target := filepath.Join(options.Directory, relative)
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return fmt.Errorf("creating React template directory: %w", err)
			}
			content, err := fs.ReadFile(reactTemplates, path)
			if err != nil {
				return fmt.Errorf("reading React template file: %w", err)
			}
			if filepath.ToSlash(relative) == "index.html" {
				content = []byte(strings.ReplaceAll(string(content), "__ARTIFACT_NAME__", html.EscapeString(options.Name)))
			}
			if err := os.WriteFile(target, content, 0o640); err != nil {
				return fmt.Errorf("writing React template file: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeManifest(path string, options Options) error {
	content, err := manifestJSON(options)
	if err != nil {
		return fmt.Errorf("encoding artifact manifest: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating artifact manifest directory: %w", err)
	}
	if err := os.WriteFile(path, content, 0o640); err != nil {
		return fmt.Errorf("writing artifact manifest: %w", err)
	}
	return nil
}

func addGraphDependency(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading React package manifest: %w", err)
	}
	var packageJSON map[string]any
	if err := json.Unmarshal(content, &packageJSON); err != nil {
		return fmt.Errorf("decoding React package manifest: %w", err)
	}
	dependencies, ok := packageJSON["dependencies"].(map[string]any)
	if !ok {
		return fmt.Errorf("react package manifest has no dependencies")
	}
	dependencies["cytoscape"] = "3.34.2"
	content, err = json.MarshalIndent(packageJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding React package manifest: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o640); err != nil {
		return fmt.Errorf("writing React package manifest: %w", err)
	}
	return nil
}

func hasFeature(features []string, wanted string) bool {
	for _, feature := range features {
		if feature == wanted {
			return true
		}
	}
	return false
}
