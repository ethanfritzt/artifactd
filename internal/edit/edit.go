package edit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"artifactd/internal/manifest"
	"artifactd/internal/storage"
)

const (
	maxSessions        = 64
	maxSubscribers     = 32
	leaseTimeout       = 5 * time.Minute
	leaseCheckInterval = 5 * time.Second
	maxMessageLength   = 512
)

var (
	ErrAlreadyEditing   = errors.New("artifact is already being edited")
	ErrNoEdit           = errors.New("artifact is not being edited")
	ErrSessionNotFound  = errors.New("edit session not found")
	ErrVersionChanged   = errors.New("artifact changed since edit began")
	ErrTooManyClients   = errors.New("too many edit event subscribers")
	ErrTooManySessions  = errors.New("too many edit sessions")
	ErrArchivedArtifact = errors.New("archived artifacts cannot be edited")
	ErrManagerClosed    = errors.New("edit manager is closed")
)

type Status string

const (
	StatusEditing    Status = "editing"
	StatusCommitting Status = "committing"
	StatusError      Status = "error"
)

type Session struct {
	SessionID   string    `json:"session_id"`
	ArtifactID  string    `json:"artifact_id"`
	Directory   string    `json:"directory"`
	BaseVersion int       `json:"base_version"`
	Status      Status    `json:"status"`
	Message     string    `json:"message,omitempty"`
	Error       string    `json:"error,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Event struct {
	Type       string `json:"type"`
	ArtifactID string `json:"artifact_id"`
	SessionID  string `json:"session_id,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
	Version    int    `json:"version,omitempty"`
}

type Manager struct {
	store           *storage.Store
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
	sessions        map[string]*session
	subscribers     map[string]map[chan Event]struct{}
	subscriberCount int
	closed          bool
	closeOnce       sync.Once
}

type session struct {
	value      Session
	lastSeen   time.Time
	committing bool
}

func NewManager(store *storage.Store) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		store:       store,
		ctx:         ctx,
		cancel:      cancel,
		sessions:    make(map[string]*session),
		subscribers: make(map[string]map[chan Event]struct{}),
	}
	go manager.expireSessions()
	return manager
}

func (m *Manager) Begin(ctx context.Context, directory, message string) (Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	directory, err := canonicalDirectory(directory)
	if err != nil {
		return Session{}, err
	}
	message, err = validateMessage(message)
	if err != nil {
		return Session{}, err
	}
	artifactManifest, _, err := manifest.ValidateDirectory(directory)
	if err != nil {
		return Session{}, err
	}
	artifact, version, err := m.store.Current(ctx, artifactManifest.ID)
	if err != nil {
		return Session{}, fmt.Errorf("checking published artifact: %w", err)
	}
	if artifact.ArchivedAt != nil {
		return Session{}, fmt.Errorf("%w: %s", ErrArchivedArtifact, artifactManifest.ID)
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}

	sessionID, err := newSessionID()
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	value := Session{
		SessionID:   sessionID,
		ArtifactID:  artifactManifest.ID,
		Directory:   directory,
		BaseVersion: version.Number,
		Status:      StatusEditing,
		Message:     message,
		ExpiresAt:   now.Add(leaseTimeout),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Session{}, ErrManagerClosed
	}
	if len(m.sessions) >= maxSessions {
		return Session{}, ErrTooManySessions
	}
	if _, exists := m.sessions[value.ArtifactID]; exists {
		return Session{}, ErrAlreadyEditing
	}
	m.sessions[value.ArtifactID] = &session{value: value, lastSeen: now}
	m.broadcastLocked(Event{
		Type:       "edit_started",
		ArtifactID: value.ArtifactID,
		SessionID:  value.SessionID,
		Message:    value.Message,
	})
	return value, nil
}

func (m *Manager) Progress(artifactID, sessionID, message string) (Session, error) {
	message, err := validateMessage(message)
	if err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.sessionLocked(artifactID, sessionID)
	if err != nil {
		return Session{}, err
	}
	if s.committing {
		return Session{}, errors.New("edit is already being committed")
	}
	now := time.Now().UTC()
	s.lastSeen = now
	s.value.ExpiresAt = now.Add(leaseTimeout)
	s.value.Message = message
	s.value.Error = ""
	m.broadcastLocked(Event{
		Type:       "edit_progress",
		ArtifactID: artifactID,
		SessionID:  sessionID,
		Message:    message,
	})
	return s.value, nil
}

func (m *Manager) Commit(
	ctx context.Context,
	artifactID,
	sessionID,
	directory string,
	baseVersion int,
	publish func() error,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	s, err := m.sessionLocked(artifactID, sessionID)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if s.value.Directory != directory {
		m.mu.Unlock()
		return errors.New("edit directory does not match session")
	}
	if baseVersion != s.value.BaseVersion {
		m.mu.Unlock()
		return fmt.Errorf("%w: base version mismatch", ErrVersionChanged)
	}
	if s.committing {
		m.mu.Unlock()
		return errors.New("edit is already being committed")
	}
	s.committing = true
	s.value.Status = StatusCommitting
	s.value.Error = ""
	m.broadcastLocked(Event{
		Type:       "edit_committing",
		ArtifactID: artifactID,
		SessionID:  sessionID,
		Message:    "Publishing artifact…",
	})
	m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		m.commitError(artifactID, s, err)
		return err
	}
	if err := publish(); err != nil {
		m.commitError(artifactID, s, err)
		return err
	}

	m.mu.Lock()
	if current := m.sessions[artifactID]; current == s {
		delete(m.sessions, artifactID)
		m.broadcastLocked(Event{
			Type:       "artifact_changed",
			ArtifactID: artifactID,
			SessionID:  sessionID,
		})
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) commitError(artifactID string, s *session, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[artifactID] != s {
		return
	}
	s.committing = false
	s.lastSeen = time.Now().UTC()
	s.value.ExpiresAt = s.lastSeen.Add(leaseTimeout)
	s.value.Status = StatusError
	s.value.Error = err.Error()
	m.broadcastLocked(Event{
		Type:       "edit_error",
		ArtifactID: artifactID,
		SessionID:  s.value.SessionID,
		Error:      err.Error(),
	})
}

func (m *Manager) Abort(artifactID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.sessionLocked(artifactID, sessionID)
	if err != nil {
		return err
	}
	if s.committing {
		return errors.New("edit is already being committed")
	}
	delete(m.sessions, artifactID)
	m.broadcastLocked(Event{
		Type:       "edit_aborted",
		ArtifactID: artifactID,
		SessionID:  sessionID,
	})
	return nil
}

func (m *Manager) AbortArtifact(artifactID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, exists := m.sessions[artifactID]
	if !exists {
		return ErrNoEdit
	}
	if s.committing {
		return errors.New("edit is already being committed")
	}
	delete(m.sessions, artifactID)
	m.broadcastLocked(Event{
		Type:       "edit_aborted",
		ArtifactID: artifactID,
		SessionID:  s.value.SessionID,
		Message:    message,
	})
	return nil
}

func (m *Manager) Status(artifactID string) (Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, exists := m.sessions[artifactID]
	if !exists {
		return Session{}, ErrNoEdit
	}
	return s.value, nil
}

func (m *Manager) Subscribe(artifactID string) (<-chan Event, func(), error) {
	channel := make(chan Event, 8)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, ErrManagerClosed
	}
	listeners := m.subscribers[artifactID]
	if len(listeners) >= maxSubscribers || m.subscriberCount >= maxSubscribers {
		return nil, nil, ErrTooManyClients
	}
	if listeners == nil {
		listeners = make(map[chan Event]struct{})
		m.subscribers[artifactID] = listeners
	}
	listeners[channel] = struct{}{}
	m.subscriberCount++

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if listeners, ok := m.subscribers[artifactID]; ok {
				if _, exists := listeners[channel]; exists {
					delete(listeners, channel)
					m.subscriberCount--
					close(channel)
				}
				if len(listeners) == 0 {
					delete(m.subscribers, artifactID)
				}
			}
		})
	}
	return channel, unsubscribe, nil
}

func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.sessions = make(map[string]*session)
		for artifactID, listeners := range m.subscribers {
			for channel := range listeners {
				close(channel)
			}
			delete(m.subscribers, artifactID)
		}
		m.subscriberCount = 0
		m.mu.Unlock()
		m.cancel()
	})
}

func (m *Manager) expireSessions() {
	ticker := time.NewTicker(leaseCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-ticker.C:
			m.expireBefore(now.UTC())
		}
	}
}

func (m *Manager) expireBefore(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for artifactID, s := range m.sessions {
		if s.committing || now.Sub(s.lastSeen) <= leaseTimeout {
			continue
		}
		delete(m.sessions, artifactID)
		m.broadcastLocked(Event{
			Type:       "edit_expired",
			ArtifactID: artifactID,
			SessionID:  s.value.SessionID,
			Message:    "Edit session expired",
		})
	}
}

func (m *Manager) sessionLocked(artifactID, sessionID string) (*session, error) {
	s, exists := m.sessions[artifactID]
	if !exists {
		return nil, ErrNoEdit
	}
	if s.value.SessionID != sessionID {
		return nil, ErrSessionNotFound
	}
	if time.Now().UTC().After(s.value.ExpiresAt) {
		delete(m.sessions, artifactID)
		m.broadcastLocked(Event{
			Type:       "edit_expired",
			ArtifactID: artifactID,
			SessionID:  sessionID,
			Message:    "Edit session expired",
		})
		return nil, ErrSessionNotFound
	}
	return s, nil
}

func (m *Manager) broadcastLocked(event Event) {
	for channel := range m.subscribers[event.ArtifactID] {
		select {
		case channel <- event:
		default:
		}
	}
}

func newSessionID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("creating edit session ID: %w", err)
	}
	return "edit-" + hex.EncodeToString(bytes[:]), nil
}

func validateMessage(message string) (string, error) {
	if len(message) > maxMessageLength {
		return "", fmt.Errorf("edit message exceeds %d characters", maxMessageLength)
	}
	return message, nil
}

func canonicalDirectory(directory string) (string, error) {
	if directory == "" {
		return "", errors.New("edit directory is required")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolving edit directory: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("reading edit directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("edit path is not a directory")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolving edit directory symlinks: %w", err)
	}
	return filepath.Clean(resolved), nil
}
