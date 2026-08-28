package registry

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenMigratesExistingArtifactsTable(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "registry.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE artifacts (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		runtime TEXT NOT NULL,
		current_version INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	) STRICT`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	registry, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := registry.Close(); err != nil {
			t.Logf("closing registry: %v", err)
		}
	}()

	rows, err := registry.db.Query("PRAGMA table_info(artifacts)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Logf("closing schema rows: %v", closeErr)
		}
	}()
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "archived_at" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("archived_at column was not migrated")
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	registry, err := Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := registry.Close(); err != nil {
			t.Logf("closing registry: %v", err)
		}
	}()

	_, err = registry.db.Exec(`
		INSERT INTO artifacts (id, name, description, runtime, current_version, created_at, updated_at)
		VALUES ('demo', 'Demo', '', 'web', 0, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	if err := registry.Archive(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	active, err := registry.List(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active artifacts = %+v, want none", active)
	}
	all, err := registry.List(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ArchivedAt == nil {
		t.Fatalf("all artifacts = %+v, want archived demo", all)
	}

	if err := registry.Unarchive(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	active, err = registry.List(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ArchivedAt != nil {
		t.Fatalf("active artifacts after unarchive = %+v", active)
	}
	if err := registry.Archive(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing archive error = %v, want %v", err, ErrNotFound)
	}
	if err := registry.Archive(t.Context(), "artifactd-home"); !errors.Is(err, ErrProtectedArtifact) {
		t.Fatalf("system archive error = %v, want %v", err, ErrProtectedArtifact)
	}
}
