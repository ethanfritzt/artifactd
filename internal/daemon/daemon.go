package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"artifactd/internal/config"
	"artifactd/internal/ipc"
	"artifactd/internal/live"
	"artifactd/internal/providers/filesystem"
	"artifactd/internal/providers/system"
	"artifactd/internal/runtime"
	"artifactd/internal/storage"
	"artifactd/internal/web"
)

const (
	shutdownTimeout   = 10 * time.Second
	defaultArtifactID = "artifactd-home"
)

type Daemon struct {
	config config.Config
	store  *storage.Store
}

func New(cfg config.Config) (*Daemon, error) {
	store, err := storage.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("opening artifact storage: %w", err)
	}
	return &Daemon{config: cfg, store: store}, nil
}

func (d *Daemon) Close() error {
	if err := d.store.Close(); err != nil {
		return fmt.Errorf("closing artifact storage: %w", err)
	}
	return nil
}

func (d *Daemon) Run(ctx context.Context) error {
	if defaultArtifact := findDefaultArtifact(d.config); defaultArtifact != "" {
		if err := d.store.EnsureDefault(ctx, defaultArtifact, defaultArtifactID); err != nil {
			slog.Error("seeding default artifact", "error", err)
		}
	} else {
		slog.Warn("default artifact source not found", "id", defaultArtifactID)
	}

	publicURL := func(id string) string {
		return fmt.Sprintf("http://%s.%s:%d/", id, d.config.PublicHost, d.config.Port)
	}
	dataStore := runtime.NewStore()
	liveManager := live.NewManager(d.store)
	defer liveManager.Close()
	controlServer := &http.Server{
		Handler:           ipc.NewServer(d.store, publicURL, dataStore, liveManager).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	browserServer := &http.Server{
		Addr: net.JoinHostPort(d.config.Host, fmt.Sprint(d.config.Port)),
		Handler: web.NewServer(
			d.store,
			d.config.PublicHost,
			publicURL,
			defaultArtifactID,
			dataStore,
			system.NewProvider(),
			filesystem.NewProvider(),
			liveManager,
		).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		// SSE live-preview connections are intentionally long-lived.
		WriteTimeout:   0,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	browserListener, err := net.Listen("tcp", browserServer.Addr)
	if err != nil {
		return fmt.Errorf("listening for browser requests: %w", err)
	}
	controlListener, err := listenUnix(d.config.SocketPath)
	if err != nil {
		if closeErr := browserListener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			return errors.Join(err, fmt.Errorf("closing browser listener: %w", closeErr))
		}
		return err
	}
	defer func() {
		if err := os.Remove(d.config.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Error("removing control socket", "error", err)
		}
	}()

	errs := make(chan error, 2)
	go serveHTTP("control", controlServer, controlListener, errs)
	go serveHTTP("browser", browserServer, browserListener, errs)

	slog.Info("artifactd started", "socket", d.config.SocketPath, "address", browserServer.Addr)
	select {
	case <-ctx.Done():
		d.shutdown(controlServer, browserServer)
		return nil
	case err := <-errs:
		d.shutdown(controlServer, browserServer)
		return err
	}
}

func findDefaultArtifact(cfg config.Config) string {
	if cfg.DefaultArtifact != "" {
		return cfg.DefaultArtifact
	}
	candidates := []string{
		filepath.Join(cfg.DataDir, "default"),
		filepath.Join("default"),
	}
	if executable, err := os.Executable(); err == nil {
		executableDefault := filepath.Join(filepath.Dir(executable), "default")
		candidates = append(candidates, executableDefault)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func (d *Daemon) shutdown(controlServer, browserServer *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := controlServer.Shutdown(ctx); err != nil {
		slog.Error("shutting down control server", "error", err)
	}
	if err := browserServer.Shutdown(ctx); err != nil {
		slog.Error("shutting down browser server", "error", err)
	}
}

func serveHTTP(name string, server *http.Server, listener net.Listener, errs chan<- error) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s server failed: %w", name, err)
	}
}

func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("creating socket directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("socket path exists and is not a socket: %s", path)
		}
		active, err := unixSocketActive(path)
		if err != nil {
			return nil, err
		}
		if active {
			return nil, fmt.Errorf("control socket is already in use: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("removing stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("checking socket path: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listening on Unix socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		closeErr := listener.Close()
		removeErr := os.Remove(path)
		return nil, errors.Join(
			fmt.Errorf("restricting Unix socket permissions: %w", err),
			closeError(closeErr),
			removeError(removeErr),
		)
	}
	return listener, nil
}

func unixSocketActive(path string) (bool, error) {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err == nil {
		if closeErr := conn.Close(); closeErr != nil {
			return false, fmt.Errorf("closing control socket probe: %w", closeErr)
		}
		return true, nil
	}
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return false, nil
	}
	return false, fmt.Errorf("checking control socket: %w", err)
}

func closeError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("closing Unix socket: %w", err)
}

func removeError(err error) error {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("removing Unix socket: %w", err)
}
