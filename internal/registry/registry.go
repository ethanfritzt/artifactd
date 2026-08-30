package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"artifactd/internal/manifest"
	"artifactd/internal/model"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound          = errors.New("artifact not found")
	ErrWorkspaceNotFound = errors.New("workspace not found")
	ErrProtectedArtifact = errors.New("system artifact cannot be archived")
	ErrVersionConflict   = errors.New("artifact version changed")
)

var workspaceIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Registry struct {
	db      *sql.DB
	writeMu sync.Mutex
}

type PublishResult struct {
	Artifact model.Artifact
	Version  model.Version
}

// InstallVersion installs a version at relativePath before the registry
// transaction commits. The returned cleanup function must remove that
// installation when the transaction fails; it may be nil when nothing was
// installed. This ordering keeps the database from committing a path that
// does not exist, while cleanup closes the rollback side of the filesystem
// transaction.
type InstallVersion func(version int, relativePath string) (cleanup func() error, err error)

func Open(path string) (*Registry, error) {
	if path == "" {
		return nil, errors.New("registry path is required")
	}
	if err := rejectSymlinkParents(path); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("registry path must not be a symlink")
		}
		if info.IsDir() {
			return nil, errors.New("registry path is a directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading registry path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening registry: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := configure(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("closing registry: %w", closeErr))
		}
		return nil, err
	}
	if err := migrate(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("closing registry: %w", closeErr))
		}
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, errors.Join(
				fmt.Errorf("restricting registry permissions: %w", err),
				fmt.Errorf("closing registry: %w", closeErr),
			)
		}
		return nil, fmt.Errorf("restricting registry permissions: %w", err)
	}
	return &Registry{db: db}, nil
}

func (r *Registry) Close() error {
	if err := r.db.Close(); err != nil {
		return fmt.Errorf("closing registry: %w", err)
	}
	return nil
}

func rejectSymlinkParents(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving registry path: %w", err)
	}
	root := filepath.VolumeName(absolute) + string(filepath.Separator)
	relative, err := filepath.Rel(root, filepath.Dir(absolute))
	if err != nil {
		return fmt.Errorf("checking registry directory: %w", err)
	}
	current := root
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, lstatErr := os.Lstat(current)
		if errors.Is(lstatErr, os.ErrNotExist) {
			return nil
		}
		if lstatErr != nil {
			return fmt.Errorf("reading registry directory: %w", lstatErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("registry directory contains symlink: %s", current)
		}
	}
	return nil
}

func configure(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("configuring SQLite: %s: %w", pragma, err)
		}
	}
	return nil
}

func migrate(db *sql.DB) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting database migration: %w", err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rolling back database migration: %w", rollbackErr))
			}
		}
	}()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS workspaces (
			id TEXT PRIMARY KEY,
			root TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		) STRICT`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			runtime TEXT NOT NULL,
			current_version INTEGER NOT NULL,
			archived_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		) STRICT`,
		`CREATE TABLE IF NOT EXISTS versions (
			artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
			version INTEGER NOT NULL,
			storage_path TEXT NOT NULL,
			manifest TEXT NOT NULL,
			content_hash TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			PRIMARY KEY (artifact_id, version)
		) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_versions_artifact_created
			ON versions (artifact_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS version_workspaces (
			artifact_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			workspace_id TEXT NOT NULL REFERENCES workspaces(id),
			PRIMARY KEY (artifact_id, version),
			FOREIGN KEY (artifact_id, version) REFERENCES versions(artifact_id, version) ON DELETE CASCADE
		) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_version_workspaces_workspace
			ON version_workspaces (workspace_id)`,
		// schema_migrations was never consulted and could not describe a
		// partially applied migration. Remove it as part of the one-time
		// schema cleanup rather than retaining dead state.
		`DROP TABLE IF EXISTS schema_migrations`,
	}
	for _, statement := range statements {
		if _, execErr := tx.Exec(statement); execErr != nil {
			return fmt.Errorf("running database migration: %w", execErr)
		}
	}
	if err := ensureArchivedAtColumn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing database migration: %w", err)
	}
	return nil
}

func ensureArchivedAtColumn(tx *sql.Tx) error {
	rows, err := tx.Query("PRAGMA table_info(artifacts)")
	if err != nil {
		return fmt.Errorf("checking artifact schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scanning artifact schema: %w", err)
		}
		if name == "archived_at" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading artifact schema: %w", err)
	}
	if _, err := tx.Exec("ALTER TABLE artifacts ADD COLUMN archived_at TEXT"); err != nil {
		return fmt.Errorf("adding artifact archive column: %w", err)
	}
	return nil
}

func (r *Registry) List(ctx context.Context, includeArchived bool) ([]model.Artifact, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.name, a.description, a.runtime, a.current_version,
		       a.archived_at, COALESCE(vw.workspace_id, ''), a.created_at, a.updated_at
		FROM artifacts a
		LEFT JOIN version_workspaces vw
		  ON vw.artifact_id = a.id AND vw.version = a.current_version
		WHERE ? = 1 OR a.archived_at IS NULL
		ORDER BY a.name, a.id`, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("listing artifacts: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			slog.Error("closing artifact rows", "error", closeErr)
		}
	}()

	artifacts := make([]model.Artifact, 0)
	for rows.Next() {
		var artifact model.Artifact
		var archivedAt, createdAt, updatedAt sql.NullString
		if err := rows.Scan(
			&artifact.ID,
			&artifact.Name,
			&artifact.Description,
			&artifact.Runtime,
			&artifact.CurrentVersion,
			&archivedAt,
			&artifact.WorkspaceID,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning artifact: %w", err)
		}
		if archivedAt.Valid {
			parsed, parseErr := parseTime(archivedAt.String)
			if parseErr != nil {
				return nil, parseErr
			}
			artifact.ArchivedAt = &parsed
		}
		artifact.CreatedAt, err = parseTime(createdAt.String)
		if err != nil {
			return nil, err
		}
		artifact.UpdatedAt, err = parseTime(updatedAt.String)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating artifacts: %w", err)
	}
	return artifacts, nil
}

func (r *Registry) Archive(ctx context.Context, id string) error {
	if id == model.SystemArtifactID {
		return ErrProtectedArtifact
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE artifacts
		SET archived_at = COALESCE(archived_at, ?), updated_at = ?
		WHERE id = ?`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("archiving artifact: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("checking archived artifact: %w", err)
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Registry) Unarchive(ctx context.Context, id string) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := r.db.ExecContext(ctx, `
		UPDATE artifacts
		SET archived_at = NULL, updated_at = ?
		WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("unarchiving artifact: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("checking unarchived artifact: %w", err)
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Registry) Current(ctx context.Context, id string) (model.Artifact, model.Version, error) {
	var artifact model.Artifact
	var version model.Version
	var createdAt, updatedAt, versionCreatedAt string
	var archivedAt sql.NullString
	var manifestRaw string
	var storagePath string

	err := r.db.QueryRowContext(ctx, `
		SELECT a.id, a.name, a.description, a.runtime, a.current_version,
		       a.archived_at, COALESCE(vw.workspace_id, ''), a.created_at, a.updated_at,
		       v.storage_path, v.manifest, v.created_at
		FROM artifacts a
		JOIN versions v ON v.artifact_id = a.id AND v.version = a.current_version
		LEFT JOIN version_workspaces vw
		  ON vw.artifact_id = a.id AND vw.version = a.current_version
		WHERE a.id = ?`, id).Scan(
		&artifact.ID,
		&artifact.Name,
		&artifact.Description,
		&artifact.Runtime,
		&artifact.CurrentVersion,
		&archivedAt,
		&artifact.WorkspaceID,
		&createdAt,
		&updatedAt,
		&storagePath,
		&manifestRaw,
		&versionCreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Artifact{}, model.Version{}, ErrNotFound
	}
	if err != nil {
		return model.Artifact{}, model.Version{}, fmt.Errorf("reading current artifact: %w", err)
	}

	if archivedAt.Valid {
		parsed, parseErr := parseTime(archivedAt.String)
		if parseErr != nil {
			return model.Artifact{}, model.Version{}, parseErr
		}
		artifact.ArchivedAt = &parsed
	}
	artifact.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return model.Artifact{}, model.Version{}, err
	}
	artifact.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return model.Artifact{}, model.Version{}, err
	}
	version.ArtifactID = artifact.ID
	version.Number = artifact.CurrentVersion
	version.WorkspaceID = artifact.WorkspaceID
	version.Path = storagePath
	version.Manifest = []byte(manifestRaw)
	version.CreatedAt, err = parseTime(versionCreatedAt)
	if err != nil {
		return model.Artifact{}, model.Version{}, err
	}
	storedManifest, err := manifest.FromBytes(version.Manifest)
	if err != nil {
		return model.Artifact{}, model.Version{}, fmt.Errorf("decoding stored manifest: %w", err)
	}
	version.Entry = storedManifest.Entry
	return artifact, version, nil
}

func (r *Registry) Publish(
	ctx context.Context,
	m manifest.Manifest,
	rawManifest []byte,
	contentHash string,
	workspaceID string,
	install InstallVersion,
) (PublishResult, error) {
	return r.publish(ctx, m, rawManifest, contentHash, workspaceID, 0, install)
}

// PublishIfCurrent publishes only when the artifact still has expectedVersion.
// The check occurs inside the same SQLite transaction that selects the next
// version, so an edit cannot overwrite a concurrent publish.
func (r *Registry) PublishIfCurrent(
	ctx context.Context,
	m manifest.Manifest,
	rawManifest []byte,
	contentHash string,
	workspaceID string,
	expectedVersion int,
	install InstallVersion,
) (PublishResult, error) {
	return r.publish(ctx, m, rawManifest, contentHash, workspaceID, expectedVersion, install)
}

func (r *Registry) publish(
	ctx context.Context,
	m manifest.Manifest,
	rawManifest []byte,
	contentHash string,
	workspaceID string,
	expectedVersion int,
	install InstallVersion,
) (result PublishResult, err error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	if workspaceID != "" {
		if _, err := r.workspace(ctx, workspaceID); err != nil {
			return PublishResult{}, err
		}
	}
	if _, err := r.db.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return PublishResult{}, fmt.Errorf("starting publish transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := r.rollback(context.Background()); rollbackErr != nil {
			err = errors.Join(err, rollbackErr)
		}
	}()

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	var currentVersion int
	var archivedAt sql.NullString
	var createdAt string
	queryErr := r.db.QueryRowContext(ctx,
		"SELECT current_version, archived_at, created_at FROM artifacts WHERE id = ?", m.ID,
	).Scan(&currentVersion, &archivedAt, &createdAt)
	if errors.Is(queryErr, sql.ErrNoRows) {
		currentVersion = 0
		createdAt = nowText
		if _, err := r.db.ExecContext(ctx, `
			INSERT INTO artifacts (id, name, description, runtime, current_version, created_at, updated_at)
			VALUES (?, ?, ?, ?, 0, ?, ?)`,
			m.ID, m.Name, m.Description, manifest.Runtime(m), nowText, nowText,
		); err != nil {
			return PublishResult{}, fmt.Errorf("creating artifact record: %w", err)
		}
	} else if queryErr != nil {
		return PublishResult{}, fmt.Errorf("reading artifact version: %w", queryErr)
	}

	if expectedVersion > 0 && currentVersion != expectedVersion {
		return PublishResult{}, ErrVersionConflict
	}

	versionNumber := currentVersion + 1
	relativePath := filepath.ToSlash(filepath.Join("artifacts", m.ID, "versions", fmt.Sprint(versionNumber)))
	cleanup, installErr := install(versionNumber, relativePath)
	if installErr != nil {
		if cleanup != nil {
			if cleanupErr := cleanup(); cleanupErr != nil {
				installErr = errors.Join(installErr, fmt.Errorf("cleaning failed installation: %w", cleanupErr))
			}
		}
		return PublishResult{}, fmt.Errorf("installing artifact version: %w", installErr)
	}
	defer func() {
		if committed || cleanup == nil {
			return
		}
		if cleanupErr := cleanup(); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("cleaning installed artifact version: %w", cleanupErr))
		}
	}()
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO versions (artifact_id, version, storage_path, manifest, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, versionNumber, relativePath, string(rawManifest), contentHash, nowText,
	); err != nil {
		return PublishResult{}, fmt.Errorf("recording artifact version: %w", err)
	}
	if workspaceID != "" {
		if _, err := r.db.ExecContext(ctx, `
			INSERT INTO version_workspaces (artifact_id, version, workspace_id)
			VALUES (?, ?, ?)`, m.ID, versionNumber, workspaceID); err != nil {
			return PublishResult{}, fmt.Errorf("recording artifact workspace: %w", err)
		}
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE artifacts
		SET name = ?, description = ?, runtime = ?, current_version = ?, updated_at = ?
		WHERE id = ?`,
		m.Name, m.Description, manifest.Runtime(m), versionNumber, nowText, m.ID,
	); err != nil {
		return PublishResult{}, fmt.Errorf("updating artifact record: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, "COMMIT"); err != nil {
		// A COMMIT error can be ambiguous to SQLite clients. Do not remove
		// the installed files if the version is already durable.
		var recorded int
		checkErr := r.db.QueryRowContext(context.Background(),
			"SELECT 1 FROM versions WHERE artifact_id = ? AND version = ?", m.ID, versionNumber).
			Scan(&recorded)
		if checkErr == nil {
			committed = true
		} else if !errors.Is(checkErr, sql.ErrNoRows) {
			err = errors.Join(err, fmt.Errorf("checking committed publish: %w", checkErr))
		}
		return PublishResult{}, fmt.Errorf("committing publish: %w", err)
	}
	committed = true

	created, err := parseTime(createdAt)
	if err != nil {
		return PublishResult{}, err
	}
	var archived *time.Time
	if archivedAt.Valid {
		parsed, parseErr := parseTime(archivedAt.String)
		if parseErr != nil {
			return PublishResult{}, parseErr
		}
		archived = &parsed
	}
	return PublishResult{
		Artifact: model.Artifact{
			ID:             m.ID,
			Name:           m.Name,
			Description:    m.Description,
			Runtime:        manifest.Runtime(m),
			WorkspaceID:    workspaceID,
			CurrentVersion: versionNumber,
			ArchivedAt:     archived,
			CreatedAt:      created,
			UpdatedAt:      now,
		},
		Version: model.Version{
			ArtifactID:  m.ID,
			Number:      versionNumber,
			Path:        relativePath,
			Manifest:    rawManifest,
			Entry:       m.Entry,
			WorkspaceID: workspaceID,
			CreatedAt:   now,
		},
	}, nil
}

func (r *Registry) ListVersions(ctx context.Context, id string) ([]model.Version, error) {
	if _, _, err := r.Current(ctx, id); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT v.version, v.storage_path, v.manifest, v.created_at,
		       COALESCE(vw.workspace_id, '')
		FROM versions v
		LEFT JOIN version_workspaces vw
		  ON vw.artifact_id = v.artifact_id AND vw.version = v.version
		WHERE v.artifact_id = ?
		ORDER BY v.version DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("listing artifact versions: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			slog.Error("closing artifact version rows", "error", closeErr)
		}
	}()

	versions := make([]model.Version, 0)
	for rows.Next() {
		var version model.Version
		var manifestRaw, createdAt string
		if err := rows.Scan(&version.Number, &version.Path, &manifestRaw, &createdAt, &version.WorkspaceID); err != nil {
			return nil, fmt.Errorf("scanning artifact version: %w", err)
		}
		version.ArtifactID = id
		version.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		storedManifest, err := manifest.FromBytes([]byte(manifestRaw))
		if err != nil {
			return nil, fmt.Errorf("decoding artifact version manifest: %w", err)
		}
		version.Entry = storedManifest.Entry
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating artifact versions: %w", err)
	}
	return versions, nil
}

func (r *Registry) Restore(ctx context.Context, id string, version int) error {
	if version < 1 {
		return fmt.Errorf("version must be positive")
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if _, err := r.db.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("starting restore transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = r.rollback(context.Background())
		}
	}()

	var manifestRaw string
	if err := r.db.QueryRowContext(ctx, `
		SELECT manifest FROM versions WHERE artifact_id = ? AND version = ?`, id, version).
		Scan(&manifestRaw); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("reading version to restore: %w", err)
	}
	storedManifest, err := manifest.FromBytes([]byte(manifestRaw))
	if err != nil {
		return fmt.Errorf("validating version to restore: %w", err)
	}
	now := time.Now().UTC()
	if _, err := r.db.ExecContext(ctx, `
		UPDATE artifacts
		SET name = ?, description = ?, runtime = ?, current_version = ?, updated_at = ?
		WHERE id = ?`,
		storedManifest.Name,
		storedManifest.Description,
		manifest.Runtime(storedManifest),
		version,
		now.Format(time.RFC3339Nano),
		id,
	); err != nil {
		return fmt.Errorf("restoring artifact version: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("committing restore: %w", err)
	}
	committed = true
	return nil
}

func (r *Registry) AddWorkspace(ctx context.Context, id, root string) (model.Workspace, error) {
	if !workspaceIDPattern.MatchString(id) || len(id) > 63 {
		return model.Workspace{}, fmt.Errorf("workspace ID must contain lowercase letters, numbers, and single hyphens")
	}
	if root == "" || !filepath.IsAbs(root) {
		return model.Workspace{}, fmt.Errorf("workspace root must be an absolute path")
	}
	root = filepath.Clean(root)
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, root, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET root = excluded.root, updated_at = excluded.updated_at`,
		id, root, nowText, nowText)
	if err != nil {
		return model.Workspace{}, fmt.Errorf("saving workspace: %w", err)
	}
	return r.workspace(ctx, id)
}

func (r *Registry) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, root, created_at, updated_at
		FROM workspaces
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			slog.Error("closing workspace rows", "error", closeErr)
		}
	}()
	workspaces := make([]model.Workspace, 0)
	for rows.Next() {
		var workspace model.Workspace
		var createdAt, updatedAt string
		if err := rows.Scan(&workspace.ID, &workspace.Root, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scanning workspace: %w", err)
		}
		workspace.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		workspace.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating workspaces: %w", err)
	}
	return workspaces, nil
}

func (r *Registry) workspace(ctx context.Context, id string) (model.Workspace, error) {
	var workspace model.Workspace
	var createdAt, updatedAt string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, root, created_at, updated_at FROM workspaces WHERE id = ?`, id).
		Scan(&workspace.ID, &workspace.Root, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Workspace{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return model.Workspace{}, fmt.Errorf("reading workspace: %w", err)
	}
	workspace.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return model.Workspace{}, err
	}
	workspace.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return model.Workspace{}, err
	}
	return workspace, nil
}

func (r *Registry) Workspace(ctx context.Context, id string) (model.Workspace, error) {
	return r.workspace(ctx, id)
}

func (r *Registry) WorkspaceForPath(ctx context.Context, candidate string) (model.Workspace, error) {
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return model.Workspace{}, fmt.Errorf("resolving workspace path: %w", err)
	}
	workspaces, err := r.ListWorkspaces(ctx)
	if err != nil {
		return model.Workspace{}, err
	}
	var best model.Workspace
	for _, workspace := range workspaces {
		relative, relErr := filepath.Rel(workspace.Root, candidate)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		if best.ID == "" || len(workspace.Root) > len(best.Root) {
			best = workspace
		}
	}
	if best.ID == "" {
		return model.Workspace{}, ErrWorkspaceNotFound
	}
	return best, nil
}

func (r *Registry) rollback(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, "ROLLBACK"); err != nil {
		return fmt.Errorf("rolling back publish: %w", err)
	}
	return nil
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing registry timestamp: %w", err)
	}
	return parsed, nil
}
