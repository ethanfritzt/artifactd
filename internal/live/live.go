package live

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"artifactd/internal/manifest"
	"artifactd/internal/storage"
)

const (
	debounceDelay      = 150 * time.Millisecond
	maxSessions        = 64
	maxSubscribers     = 32
	watchLeaseTimeout  = 30 * time.Second
	leaseCheckInterval = 5 * time.Second
	snapshotRetryWait  = 50 * time.Millisecond
	snapshotRetries    = 3
)

var (
	ErrAlreadyWatching = errors.New("artifact is already being watched")
	ErrNotWatching     = errors.New("artifact is not being watched")
	ErrTooManyClients  = errors.New("too many live event subscribers")
	ErrTooManySessions = errors.New("too many live preview sessions")
)

type Status string

const (
	StatusWatching Status = "watching"
	StatusBuilding Status = "building"
	StatusReady    Status = "ready"
	StatusError    Status = "error"
)

type Info struct {
	ArtifactID string `json:"artifact_id"`
	Directory  string `json:"directory"`
	Status     Status `json:"status"`
	Hash       string `json:"hash"`
	Error      string `json:"error,omitempty"`
	URL        string `json:"url,omitempty"`
}

type Snapshot struct {
	Path  string
	Entry string
}

type Event struct {
	Type       string `json:"type"`
	ArtifactID string `json:"artifact_id"`
	Hash       string `json:"hash,omitempty"`
	Message    string `json:"message,omitempty"`
}

type Manager struct {
	store       *storage.Store
	context     context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	sessions    map[string]*session
	starting    map[string]bool
	subscribers map[string]map[chan Event]struct{}
}

type session struct {
	artifactID string
	directory  string
	cancel     context.CancelFunc
	context    context.Context
	done       chan struct{}
	snapshot   Snapshot
	info       Info
	lastSeen   time.Time
}

func NewManager(store *storage.Store) *Manager {
	managerContext, cancel := context.WithCancel(context.Background())
	return &Manager{
		store:       store,
		context:     managerContext,
		cancel:      cancel,
		sessions:    make(map[string]*session),
		starting:    make(map[string]bool),
		subscribers: make(map[string]map[chan Event]struct{}),
	}
}

func (m *Manager) Start(ctx context.Context, directory string) (Info, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	source, err := canonicalDirectory(directory)
	if err != nil {
		return Info{}, err
	}
	manifestValue, _, err := manifest.ValidateDirectory(source)
	if err != nil {
		return Info{}, err
	}
	if _, _, err := m.store.Current(ctx, manifestValue.ID); err != nil {
		return Info{}, fmt.Errorf("checking published artifact: %w", err)
	}

	m.mu.Lock()
	if m.starting[manifestValue.ID] || m.sessions[manifestValue.ID] != nil {
		m.mu.Unlock()
		return Info{}, ErrAlreadyWatching
	}
	if len(m.sessions)+len(m.starting) >= maxSessions {
		m.mu.Unlock()
		return Info{}, ErrTooManySessions
	}
	m.starting[manifestValue.ID] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.starting, manifestValue.ID)
		m.mu.Unlock()
	}()

	sessionContext, cancel := context.WithCancel(m.context)
	path, currentManifest, hash, err := m.createSnapshotWithRetry(source)
	if err != nil {
		cancel()
		return Info{}, err
	}
	if currentManifest.ID != manifestValue.ID {
		_ = m.store.RemoveLiveSnapshot(path)
		cancel()
		return Info{}, fmt.Errorf("artifact ID changed while starting watch")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		_ = m.store.RemoveLiveSnapshot(path)
		cancel()
		return Info{}, fmt.Errorf("creating artifact watcher: %w", err)
	}
	if err := addDirectories(watcher, source); err != nil {
		_ = watcher.Close()
		_ = m.store.RemoveLiveSnapshot(path)
		cancel()
		return Info{}, err
	}

	s := &session{
		artifactID: manifestValue.ID,
		directory:  source,
		cancel:     cancel,
		context:    sessionContext,
		done:       make(chan struct{}),
		snapshot: Snapshot{
			Path:  path,
			Entry: currentManifest.Entry,
		},
		info: Info{
			ArtifactID: manifestValue.ID,
			Directory:  source,
			Status:     StatusReady,
			Hash:       hash,
		},
		lastSeen: time.Now(),
	}
	m.mu.Lock()
	m.sessions[s.artifactID] = s
	m.mu.Unlock()
	m.broadcast(Event{Type: "artifact_changed", ArtifactID: s.artifactID, Hash: hash})
	go m.run(s, watcher)
	return s.info, nil
}

func (m *Manager) Stop(artifactID string) error {
	m.mu.RLock()
	s := m.sessions[artifactID]
	m.mu.RUnlock()
	if s == nil {
		return ErrNotWatching
	}
	s.cancel()
	<-s.done
	return nil
}

func (m *Manager) Info(artifactID string) (Info, error) {
	m.mu.Lock()
	s := m.sessions[artifactID]
	if s == nil {
		m.mu.Unlock()
		return Info{}, ErrNotWatching
	}
	s.lastSeen = time.Now()
	info := s.info
	m.mu.Unlock()
	return info, nil
}

func (m *Manager) Snapshot(artifactID string) (Snapshot, func(), bool) {
	m.mu.RLock()
	s := m.sessions[artifactID]
	if s == nil {
		m.mu.RUnlock()
		return Snapshot{}, func() {}, false
	}
	return s.snapshot, m.mu.RUnlock, true
}

func (m *Manager) Subscribe(artifactID string) (<-chan Event, func(), error) {
	channel := make(chan Event, 8)
	m.mu.Lock()
	listeners := m.subscribers[artifactID]
	if listeners == nil {
		listeners = make(map[chan Event]struct{})
		m.subscribers[artifactID] = listeners
	}
	if len(listeners) >= maxSubscribers {
		m.mu.Unlock()
		return nil, nil, ErrTooManyClients
	}
	listeners[channel] = struct{}{}
	m.mu.Unlock()

	unsubscribe := func() {
		m.mu.Lock()
		if listeners, ok := m.subscribers[artifactID]; ok {
			if _, exists := listeners[channel]; exists {
				delete(listeners, channel)
				close(channel)
			}
			if len(listeners) == 0 {
				delete(m.subscribers, artifactID)
			}
		}
		m.mu.Unlock()
	}
	return channel, unsubscribe, nil
}

func (m *Manager) Close() {
	m.cancel()
	m.mu.RLock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()
	for _, s := range sessions {
		s.cancel()
	}
	for _, s := range sessions {
		<-s.done
	}
	m.mu.Lock()
	for artifactID, listeners := range m.subscribers {
		for channel := range listeners {
			close(channel)
		}
		delete(m.subscribers, artifactID)
	}
	m.mu.Unlock()
}

func (m *Manager) run(s *session, watcher *fsnotify.Watcher) {
	defer func() {
		if err := watcher.Close(); err != nil {
			slog.Debug("closing artifact watcher", "artifact_id", s.artifactID, "error", err)
		}
		m.mu.Lock()
		removed := false
		if current := m.sessions[s.artifactID]; current == s {
			delete(m.sessions, s.artifactID)
			_ = m.store.RemoveLiveSnapshot(s.snapshot.Path)
			removed = true
		}
		close(s.done)
		m.mu.Unlock()
		if removed {
			m.broadcast(Event{Type: "artifact_changed", ArtifactID: s.artifactID})
		}
	}()

	var timer *time.Timer
	var timerChannel <-chan time.Time
	resetTimer := func() {
		if timer == nil {
			timer = time.NewTimer(debounceDelay)
			timerChannel = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(debounceDelay)
		timerChannel = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	leaseTicker := time.NewTicker(leaseCheckInterval)
	defer leaseTicker.Stop()

	for {
		select {
		case <-s.doneContext():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				m.setError(s, "artifact watcher stopped")
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := addDirectories(watcher, event.Name); err != nil {
						m.setError(s, err.Error())
					}
				}
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				resetTimer()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				m.setError(s, "artifact watcher stopped")
				return
			}
			m.setError(s, fmt.Sprintf("artifact watcher error: %v", err))
		case <-timerChannel:
			timerChannel = nil
			m.refresh(s)
		case <-leaseTicker.C:
			if m.leaseExpired(s) {
				s.cancel()
				return
			}
		}
	}
}

func (m *Manager) leaseExpired(s *session) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[s.artifactID] == s && time.Since(s.lastSeen) > watchLeaseTimeout
}

func (s *session) doneContext() <-chan struct{} {
	return s.context.Done()
}

func (m *Manager) refresh(s *session) {
	m.setStatus(s, StatusBuilding, "")
	path, currentManifest, hash, err := m.createSnapshotWithRetry(s.directory)
	if err != nil {
		m.setError(s, err.Error())
		return
	}
	if currentManifest.ID != s.artifactID {
		_ = m.store.RemoveLiveSnapshot(path)
		m.setError(s, "artifact ID changed while watching")
		return
	}
	m.mu.Lock()
	if m.sessions[s.artifactID] != s {
		m.mu.Unlock()
		_ = m.store.RemoveLiveSnapshot(path)
		return
	}
	oldPath := s.snapshot.Path
	s.snapshot = Snapshot{Path: path, Entry: currentManifest.Entry}
	s.info.Status = StatusReady
	s.info.Hash = hash
	s.info.Error = ""
	_ = m.store.RemoveLiveSnapshot(oldPath)
	m.mu.Unlock()
	m.broadcast(Event{Type: "artifact_changed", ArtifactID: s.artifactID, Hash: hash})
}

func (m *Manager) setStatus(s *session, status Status, message string) {
	m.mu.Lock()
	if m.sessions[s.artifactID] == s {
		s.info.Status = status
		s.info.Error = message
	}
	m.mu.Unlock()
}

func (m *Manager) setError(s *session, message string) {
	m.setStatus(s, StatusError, message)
	m.broadcast(Event{Type: "artifact_error", ArtifactID: s.artifactID, Message: message})
}

func (m *Manager) broadcast(event Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for channel := range m.subscribers[event.ArtifactID] {
		select {
		case channel <- event:
		default:
		}
	}
}

func (m *Manager) createSnapshotWithRetry(directory string) (string, manifest.Manifest, string, error) {
	var lastErr error
	for attempt := 0; attempt < snapshotRetries; attempt++ {
		path, currentManifest, hash, err := m.store.CreateLiveSnapshot(directory)
		if err == nil {
			return path, currentManifest, hash, nil
		}
		lastErr = err
		if attempt+1 < snapshotRetries {
			time.Sleep(snapshotRetryWait)
		}
	}
	return "", manifest.Manifest{}, "", lastErr
}

func canonicalDirectory(directory string) (string, error) {
	if directory == "" {
		return "", errors.New("watch directory is required")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolving watch directory: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("reading watch directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("watch path is not a directory")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolving watch directory symlinks: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func addDirectories(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking watch directory: %w", err)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not allowed in watched artifacts")
		}
		if !entry.IsDir() {
			return nil
		}
		if err := watcher.Add(path); err != nil {
			return fmt.Errorf("watching artifact directory: %w", err)
		}
		return nil
	})
}
