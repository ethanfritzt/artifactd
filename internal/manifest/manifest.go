package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	CurrentVersion        = 1
	CurrentRuntimeVersion = 1
	DefaultRuntimeID      = "web-static"
	MaxManifestBytes      = 1 << 20
	MaxArtifactFiles      = 1000
	MaxArtifactBytes      = 50 << 20
	MaxNameLength         = 200
	MaxDescriptionSize    = 2000
)

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Manifest is the normalized representation of an artifact specification.
// Legacy flat manifests and the structured specification both decode into it.
type Manifest struct {
	ArtifactVersion int
	ID              string
	Name            string
	Description     string
	Entry           string
	Code            CodeSpec
	Runtime         RuntimeSpec
	Capabilities    []json.RawMessage
}

// CodeSpec describes the files in an artifact package.
type CodeSpec struct {
	Format string `json:"format"`
	Entry  string `json:"entry"`
}

// RuntimeSpec identifies an allowlisted Artifactd runtime.
type RuntimeSpec struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

func (r *RuntimeSpec) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return errors.New("runtime must not be empty")
	}
	if raw[0] == '"' {
		var legacyID string
		if err := json.Unmarshal(raw, &legacyID); err != nil {
			return err
		}
		r.ID = normalizeRuntimeID(legacyID)
		r.Version = CurrentRuntimeVersion
		return nil
	}

	var value struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if value.ID == "" {
		return errors.New("runtime id is required")
	}
	if value.Version == 0 {
		value.Version = CurrentRuntimeVersion
	}
	*r = RuntimeSpec{ID: normalizeRuntimeID(value.ID), Version: value.Version}
	return nil
}

type artifactMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type manifestDocument struct {
	ArtifactVersion int               `json:"artifactVersion"`
	SpecVersion     int               `json:"specVersion"`
	Artifact        *artifactMetadata `json:"artifact"`
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Entry           string            `json:"entry"`
	Code            *CodeSpec         `json:"code"`
	Runtime         json.RawMessage   `json:"runtime"`
	Capabilities    []json.RawMessage `json:"capabilities"`
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func ValidateDirectory(dir string) (Manifest, []byte, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("reading artifact directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Manifest{}, nil, &ValidationError{Message: "artifact path is not a directory"}
	}

	manifestPath := filepath.Join(dir, "artifact.json")
	raw, err := readManifest(manifestPath)
	if err != nil {
		return Manifest{}, nil, err
	}
	m, err := decode(raw)
	if err != nil {
		return Manifest{}, nil, err
	}
	if err := validateManifest(m); err != nil {
		return Manifest{}, nil, err
	}

	entryPath, err := safeJoin(dir, m.Entry)
	if err != nil {
		return Manifest{}, nil, &ValidationError{Message: fmt.Sprintf("invalid entry path: %v", err)}
	}
	entryInfo, err := os.Lstat(entryPath)
	if err != nil {
		return Manifest{}, nil, &ValidationError{Message: "entry file does not exist"}
	}
	if !entryInfo.Mode().IsRegular() {
		return Manifest{}, nil, &ValidationError{Message: "entry file must be regular"}
	}

	if err := validateTree(dir); err != nil {
		return Manifest{}, nil, err
	}
	return m, raw, nil
}

func FromBytes(raw []byte) (Manifest, error) {
	m, err := decode(raw)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateManifest(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func runtime(m Manifest) string {
	if m.Runtime.ID == "" {
		return DefaultRuntimeID
	}
	return normalizeRuntimeID(m.Runtime.ID)
}

func readManifest(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, &ValidationError{Message: "artifact.json is required"}
	}
	if !info.Mode().IsRegular() {
		return nil, &ValidationError{Message: "artifact.json must be a regular file"}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening artifact.json: %w", err)
	}
	limited := io.LimitReader(f, MaxManifestBytes+1)
	raw, readErr := io.ReadAll(limited)
	closeErr := f.Close()
	if readErr != nil {
		return nil, fmt.Errorf("reading artifact.json: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("closing artifact.json: %w", closeErr)
	}
	if len(raw) > MaxManifestBytes {
		return nil, &ValidationError{Message: "artifact.json is too large"}
	}
	return raw, nil
}

func decode(raw []byte) (Manifest, error) {
	var document manifestDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Manifest{}, &ValidationError{Message: fmt.Sprintf("invalid artifact.json: %v", err)}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Manifest{}, &ValidationError{Message: "artifact.json must contain one JSON object"}
	}

	version := document.ArtifactVersion
	if version == 0 {
		version = document.SpecVersion
	}
	if document.ArtifactVersion != 0 && document.SpecVersion != 0 && document.ArtifactVersion != document.SpecVersion {
		return Manifest{}, &ValidationError{Message: "artifactVersion and specVersion must match"}
	}

	m := Manifest{
		ArtifactVersion: version,
		ID:              document.ID,
		Name:            document.Name,
		Description:     document.Description,
		Entry:           document.Entry,
		Code: CodeSpec{
			Format: "files",
		},
		Runtime:      RuntimeSpec{ID: DefaultRuntimeID, Version: CurrentRuntimeVersion},
		Capabilities: document.Capabilities,
	}
	if document.Artifact != nil {
		if document.ID != "" || document.Name != "" || document.Description != "" {
			return Manifest{}, &ValidationError{Message: "artifact metadata must not be duplicated at the top level"}
		}
		m.ID = document.Artifact.ID
		m.Name = document.Artifact.Name
		m.Description = document.Artifact.Description
	}
	if document.Code != nil {
		m.Code = *document.Code
		if m.Code.Format == "" {
			m.Code.Format = "files"
		}
		if m.Code.Entry != "" {
			if m.Entry != "" && m.Entry != m.Code.Entry {
				return Manifest{}, &ValidationError{Message: "entry and code.entry must match"}
			}
			m.Entry = m.Code.Entry
		}
	}
	if len(document.Runtime) > 0 && string(document.Runtime) != "null" {
		if err := json.Unmarshal(document.Runtime, &m.Runtime); err != nil {
			return Manifest{}, &ValidationError{Message: fmt.Sprintf("invalid runtime: %v", err)}
		}
	}
	if m.Code.Entry == "" {
		m.Code.Entry = m.Entry
	}
	return m, nil
}

func validateManifest(m Manifest) error {
	if m.ArtifactVersion != CurrentVersion {
		return &ValidationError{Message: fmt.Sprintf("artifactVersion or specVersion must be %d", CurrentVersion)}
	}
	if !idPattern.MatchString(m.ID) || len(m.ID) > 63 {
		return &ValidationError{Message: "id must contain lowercase letters, numbers, and single hyphens"}
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > MaxNameLength {
		return &ValidationError{Message: "name is required and must be at most 200 characters"}
	}
	if len(m.Description) > MaxDescriptionSize {
		return &ValidationError{Message: "description is too long"}
	}
	if m.Entry == "" {
		return &ValidationError{Message: "entry is required"}
	}
	if m.Code.Format != "" && m.Code.Format != "files" {
		return &ValidationError{Message: "code.format must be files"}
	}
	if m.Runtime.ID != DefaultRuntimeID {
		return &ValidationError{Message: "runtime must be web-static"}
	}
	if m.Runtime.Version != CurrentRuntimeVersion {
		return &ValidationError{Message: fmt.Sprintf("runtime version must be %d", CurrentRuntimeVersion)}
	}
	if len(m.Capabilities) > 0 {
		return &ValidationError{Message: "capabilities are not supported yet"}
	}
	return nil
}

func validateTree(dir string) error {
	var files int
	var bytesTotal int64
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking artifact: %w", err)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return &ValidationError{Message: fmt.Sprintf("symlinks are not allowed: %s", path)}
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("reading artifact file info: %w", err)
		}
		if !info.Mode().IsRegular() {
			return &ValidationError{Message: fmt.Sprintf("special files are not allowed: %s", path)}
		}
		files++
		if files > MaxArtifactFiles {
			return &ValidationError{Message: "artifact contains too many files"}
		}
		size := info.Size()
		if size < 0 || size > MaxArtifactBytes-bytesTotal {
			return &ValidationError{Message: "artifact is too large"}
		}
		bytesTotal += size
		return nil
	})
}

func safeJoin(root, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || filepath.IsAbs(name) || strings.Contains(name, "\\") || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", errors.New("path must be local and relative")
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes artifact root")
	}
	if filepath.ToSlash(clean) != name {
		return "", errors.New("path must be normalized")
	}
	return filepath.Join(root, clean), nil
}

func normalizeRuntimeID(id string) string {
	if id == "" || id == "web" {
		return DefaultRuntimeID
	}
	return id
}

func ValidID(id string) bool {
	return idPattern.MatchString(id) && len(id) <= 63
}

func Runtime(m Manifest) string {
	return runtime(m)
}
