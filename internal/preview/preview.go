package preview

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"artifactd/internal/markdown"
)

const (
	Lifetime     = time.Hour
	MaxBytes     = markdown.MaxBytes
	maxDocuments = 32
)

var (
	ErrNotFound     = errors.New("preview not found")
	ErrTooLarge     = errors.New("markdown document is too large")
	ErrUnauthorized = errors.New("preview save is not authorized")
	ErrConflict     = errors.New("markdown file changed since preview opened")
)

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

type Document struct {
	ID        string
	Name      string
	Content   []byte
	SaveToken string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type editableSource struct {
	path   string
	token  string
	digest [sha256.Size]byte
}

type Manager struct {
	mu        sync.Mutex
	documents map[string]Document
	sources   map[string]editableSource
	now       func() time.Time
}

func NewManager() *Manager {
	return &Manager{
		documents: map[string]Document{},
		sources:   map[string]editableSource{},
		now:       time.Now,
	}
}

func (m *Manager) Create(name string, content []byte) (Document, error) {
	if err := ValidateName(name); err != nil {
		return Document{}, err
	}
	if len(content) > MaxBytes {
		return Document{}, ErrTooLarge
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.removeExpired(now)
	if len(m.documents) >= maxDocuments {
		m.removeOldest()
	}
	id, err := m.newID()
	if err != nil {
		return Document{}, err
	}
	document := Document{
		ID:        id,
		Name:      name,
		Content:   append([]byte(nil), content...),
		CreatedAt: now,
		ExpiresAt: now.Add(Lifetime),
	}
	m.documents[id] = document
	return document, nil
}

// CreateEditable creates a temporary preview with a narrowly scoped save capability.
func (m *Manager) CreateEditable(name, path string, content []byte) (Document, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Document{}, fmt.Errorf("resolving Markdown source path: %w", err)
	}
	if filepath.Clean(absolute) != absolute {
		return Document{}, errors.New("markdown source path must be normalized")
	}
	current, err := readRegularFile(absolute)
	if err != nil {
		return Document{}, err
	}
	if !equalBytes(current, content) {
		return Document{}, ErrConflict
	}
	document, err := m.Create(name, content)
	if err != nil {
		return Document{}, err
	}
	token, err := randomToken()
	if err != nil {
		m.mu.Lock()
		delete(m.documents, document.ID)
		m.mu.Unlock()
		return Document{}, err
	}
	document.SaveToken = token
	m.mu.Lock()
	m.documents[document.ID] = document
	m.sources[document.ID] = editableSource{path: absolute, token: token, digest: sha256.Sum256(content)}
	m.mu.Unlock()
	return document, nil
}

// Save writes content only when the preview capability is valid and the source is unchanged.
func (m *Manager) Save(id, token string, content []byte) error {
	if len(content) > MaxBytes {
		return ErrTooLarge
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	document, ok := m.documents[id]
	if !ok || !m.now().Before(document.ExpiresAt) {
		delete(m.documents, id)
		delete(m.sources, id)
		return ErrNotFound
	}
	source, ok := m.sources[id]
	if !ok || !tokensEqual(source.token, token) {
		return ErrUnauthorized
	}
	current, err := readRegularFile(source.path)
	if err != nil {
		return err
	}
	if sha256.Sum256(current) != source.digest {
		return ErrConflict
	}
	if err := writeAtomic(source.path, source.digest, content); err != nil {
		return err
	}
	source.digest = sha256.Sum256(content)
	document.Content = append([]byte(nil), content...)
	m.sources[id] = source
	m.documents[id] = document
	return nil
}

func (m *Manager) Get(id string) (Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	document, ok := m.documents[id]
	if !ok {
		return Document{}, ErrNotFound
	}
	if !m.now().Before(document.ExpiresAt) {
		delete(m.documents, id)
		delete(m.sources, id)
		return Document{}, ErrNotFound
	}
	return document, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.documents = map[string]Document{}
	m.sources = map[string]editableSource{}
}

func LoadFile(path string) (string, []byte, error) {
	if path == "" {
		return "", nil, errors.New("markdown file is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", nil, fmt.Errorf("resolving markdown file: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", nil, fmt.Errorf("reading markdown file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil, errors.New("markdown path must be a regular file, not a symlink")
	}
	name := filepath.Base(absolute)
	if err := ValidateName(name); err != nil {
		return "", nil, err
	}
	if info.Size() > MaxBytes {
		return "", nil, ErrTooLarge
	}
	file, err := os.Open(absolute)
	if err != nil {
		return "", nil, fmt.Errorf("opening markdown file: %w", err)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil {
		return "", nil, errors.Join(
			fmt.Errorf("reading opened markdown file: %w", statErr),
			file.Close(),
		)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return "", nil, errors.Join(
			errors.New("markdown file changed while it was being opened"),
			file.Close(),
		)
	}
	content, readErr := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return "", nil, fmt.Errorf("reading markdown file: %w", readErr)
	}
	if closeErr != nil {
		return "", nil, fmt.Errorf("closing markdown file: %w", closeErr)
	}
	if len(content) > MaxBytes {
		return "", nil, ErrTooLarge
	}
	return name, content, nil
}

func ValidateName(name string) error {
	isBaseName := filepath.Base(name) == name
	isSafe := name != "" && !strings.ContainsRune(name, '\x00') && !strings.ContainsAny(name, `/\\`)
	if !isBaseName || !isSafe || !markdown.IsFilename(name) {
		return &ValidationError{Message: "preview file must be a Markdown file named with .md or .markdown"}
	}
	return nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("checking Markdown source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("markdown source must be a regular file, not a symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening Markdown source: %w", err)
	}
	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.Join(errors.New("markdown source changed while opening"), statErr, file.Close())
	}
	content, readErr := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if len(content) > MaxBytes {
		return nil, ErrTooLarge
	}
	return content, nil
}

func writeAtomic(path string, expected [sha256.Size]byte, content []byte) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("checking Markdown source before save: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("markdown source must be a regular file, not a symlink")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".artifactd-markdown-*")
	if err != nil {
		return fmt.Errorf("creating temporary Markdown file: %w", err)
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return errors.Join(fmt.Errorf("setting Markdown permissions: %w", err), temporary.Close())
	}
	if _, err := temporary.Write(content); err != nil {
		return errors.Join(fmt.Errorf("writing Markdown source: %w", err), temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(fmt.Errorf("syncing Markdown source: %w", err), temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing temporary Markdown source: %w", err)
	}
	latest, err := os.Lstat(path)
	if err != nil || latest.Mode()&os.ModeSymlink != 0 || !latest.Mode().IsRegular() || !os.SameFile(info, latest) {
		return errors.Join(errors.New("markdown source changed before save"), err)
	}
	current, err := readRegularFile(path)
	if err != nil {
		return err
	}
	if sha256.Sum256(current) != expected {
		return ErrConflict
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("saving Markdown source: %w", err)
	}
	return nil
}

func randomToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("generating preview save token: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

func tokensEqual(left, right string) bool {
	if len(left) != len(right) || right == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare(left, right) == 1
}

func (m *Manager) newID() (string, error) {
	for range 4 {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", fmt.Errorf("generating preview ID: %w", err)
		}
		id := "preview-" + hex.EncodeToString(token[:])
		if _, exists := m.documents[id]; !exists {
			return id, nil
		}
	}
	return "", errors.New("could not generate a unique preview ID")
}

func (m *Manager) removeExpired(now time.Time) {
	for id, document := range m.documents {
		if !now.Before(document.ExpiresAt) {
			delete(m.documents, id)
			delete(m.sources, id)
		}
	}
}

func (m *Manager) removeOldest() {
	var oldest Document
	for _, document := range m.documents {
		if oldest.ID == "" || document.CreatedAt.Before(oldest.CreatedAt) {
			oldest = document
		}
	}
	if oldest.ID != "" {
		delete(m.documents, oldest.ID)
		delete(m.sources, oldest.ID)
	}
}
