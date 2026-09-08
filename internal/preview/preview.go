package preview

import (
	"crypto/rand"
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
	ErrNotFound = errors.New("preview not found")
	ErrTooLarge = errors.New("markdown document is too large")
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
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Manager struct {
	mu        sync.Mutex
	documents map[string]Document
	now       func() time.Time
}

func NewManager() *Manager {
	return &Manager{
		documents: map[string]Document{},
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

func (m *Manager) Get(id string) (Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	document, ok := m.documents[id]
	if !ok {
		return Document{}, ErrNotFound
	}
	if !m.now().Before(document.ExpiresAt) {
		delete(m.documents, id)
		return Document{}, ErrNotFound
	}
	return document, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.documents = map[string]Document{}
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
	}
}
