package filesystem

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"artifactd/internal/model"
)

const (
	DefaultDepth = 3
	MaxDepth     = 8
	MaxNodes     = 10000
	MaxReadBytes = 1 << 20
)

type Provider struct{}

type Node struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size,omitempty"`
	Modified time.Time `json:"modified,omitempty"`
}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) List(workspace model.Workspace, relative string, depth int) ([]Node, error) {
	if depth == 0 {
		depth = DefaultDepth
	}
	if depth < 1 || depth > MaxDepth {
		return nil, fmt.Errorf("depth must be between 1 and %d", MaxDepth)
	}
	root, err := safePath(workspace.Root, relative)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("reading workspace path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("workspace path is a symlink")
	}
	if !info.IsDir() {
		return nil, errors.New("workspace path is not a directory")
	}
	nodes := make([]Node, 0)
	if err := listDirectory(root, relative, depth, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func (p *Provider) Read(workspace model.Workspace, relative string) ([]byte, error) {
	path, err := safePath(workspace.Root, relative)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("reading workspace file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("workspace path is not a regular file")
	}
	if info.Size() > MaxReadBytes {
		return nil, fmt.Errorf("workspace file is larger than %d bytes", MaxReadBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening workspace file: %w", err)
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, MaxReadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading workspace file: %w", err)
	}
	if len(content) > MaxReadBytes {
		return nil, fmt.Errorf("workspace file is larger than %d bytes", MaxReadBytes)
	}
	return content, nil
}

func listDirectory(directory, relative string, depth int, nodes *[]Node) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("reading workspace directory: %w", err)
	}
	for _, entry := range entries {
		if len(*nodes) >= MaxNodes {
			return fmt.Errorf("workspace listing exceeds %d nodes", MaxNodes)
		}
		entryRelative := entry.Name()
		if relative != "" && relative != "." {
			entryRelative = filepath.Join(relative, entry.Name())
		}
		entryRelative = filepath.ToSlash(entryRelative)
		path := filepath.Join(directory, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("reading workspace entry: %w", err)
		}
		node := Node{Path: entryRelative, Name: entry.Name(), Modified: info.ModTime().UTC()}
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			node.Kind = "symlink"
		case info.IsDir():
			node.Kind = "directory"
		case info.Mode().IsRegular():
			node.Kind = "file"
			node.Size = info.Size()
		default:
			node.Kind = "special"
		}
		*nodes = append(*nodes, node)
		if node.Kind == "directory" && depth > 1 {
			if err := listDirectory(path, entryRelative, depth-1, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

func safePath(root, relative string) (string, error) {
	if relative == "" {
		relative = "."
	}
	if strings.Contains(relative, "\\") || filepath.IsAbs(filepath.FromSlash(relative)) || !filepath.IsLocal(filepath.FromSlash(relative)) {
		return "", errors.New("workspace path must be local and relative")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if filepath.ToSlash(clean) != relative && relative != "." {
		return "", errors.New("workspace path must be normalized")
	}
	candidate := filepath.Join(root, clean)
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving workspace root: %w", err)
	}
	resolvedCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolving workspace path: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedCandidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("workspace path escapes workspace root")
	}
	return resolvedCandidate, nil
}
