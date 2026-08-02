package sqlite

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/pressly/goose/v3"
)

var generationTokenPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func sessionIDGeneration(t *testing.T, db *sql.DB) string {
	t.Helper()
	var generation string
	if err := db.QueryRow(
		"SELECT session_id_generation FROM daemon_settings WHERE id = 1",
	).Scan(&generation); err != nil {
		t.Fatalf("read session_id_generation: %v", err)
	}
	return generation
}

func TestMigrateMintsSessionIDGenerationOnFreshDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if generation := sessionIDGeneration(t, db); !generationTokenPattern.MatchString(generation) {
		t.Fatalf("generation = %q, want 32 lowercase hex chars", generation)
	}
}

func TestMigrateMintsDistinctGenerationsPerDatabase(t *testing.T) {
	newGeneration := func() string {
		db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := migrate(db); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		return sessionIDGeneration(t, db)
	}
	if a, b := newGeneration(), newGeneration(); a == b {
		t.Fatalf("two databases minted the same generation token %q", a)
	}
}

func TestMigrateGenerationStableAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "ao.db") + pragmas

	first, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrate(first); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	before := sessionIDGeneration(t, first)
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := migrate(second); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if after := sessionIDGeneration(t, second); after != before {
		t.Fatalf("generation changed across reopen: before %q, after %q", before, after)
	}
}

func TestMigrateGenerationDoesNotRewriteExistingSessionID(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		gooseMu.Unlock()
		t.Fatalf("set goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 58); err != nil {
		gooseMu.Unlock()
		t.Fatalf("migrate to 0058: %v", err)
	}
	gooseMu.Unlock()

	now := "2026-08-02T00:00:00Z"
	if _, err := db.Exec(
		`INSERT INTO projects (id, path, registered_at) VALUES ('mer', '/tmp/mer', ?)`, now,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at)
		 VALUES ('mer-7', 'mer', 7, ?, ?, ?)`, now, now, now,
	); err != nil {
		t.Fatalf("seed legacy session: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("upgrade migrate: %v", err)
	}

	var id string
	if err := db.QueryRow(`SELECT id FROM sessions WHERE num = 7`).Scan(&id); err != nil {
		t.Fatalf("read legacy session: %v", err)
	}
	if id != "mer-7" {
		t.Fatalf("legacy session id = %q, want unchanged mer-7", id)
	}
	if generation := sessionIDGeneration(t, db); !generationTokenPattern.MatchString(generation) {
		t.Fatalf("generation after upgrade = %q, want minted token", generation)
	}
}
