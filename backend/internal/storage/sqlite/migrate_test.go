package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TestMigrateAllowsEveryShippedHarness guards against the collapsed-migration
// silent-no-op concern: a hand-written replace() that fails to widen the
// sessions.harness CHECK (because the target substring drifted) leaves the
// schema accepting only the original harnesses while migrate() still reports
// success. This test opens a fresh DB, runs the migrations, and asserts the
// live sessions schema admits every harness the domain ships, building the
// expected set from the domain constants so it can't silently drift.
func TestMigrateAllowsEveryShippedHarness(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var schema string
	if err := db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='sessions'",
	).Scan(&schema); err != nil {
		t.Fatalf("read sessions schema: %v", err)
	}

	harnesses := []domain.AgentHarness{
		domain.HarnessClaudeCode,
		domain.HarnessCodex,
		domain.HarnessCodexFugu,
		domain.HarnessAider,
		domain.HarnessOpenCode,
		domain.HarnessGrok,
		domain.HarnessDroid,
		domain.HarnessAmp,
		domain.HarnessAgy,
		domain.HarnessCrush,
		domain.HarnessCursor,
		domain.HarnessQwen,
		domain.HarnessCopilot,
		domain.HarnessGoose,
		domain.HarnessAuggie,
		domain.HarnessContinue,
		domain.HarnessDevin,
		domain.HarnessCline,
		domain.HarnessKimi,
		domain.HarnessKiro,
		domain.HarnessKilocode,
		domain.HarnessVibe,
		domain.HarnessPi,
		domain.HarnessAutohand,
	}

	for _, h := range harnesses {
		if !strings.Contains(schema, "'"+string(h)+"'") {
			t.Errorf("sessions.harness CHECK is missing harness %q — the migration that widens it silently no-opped; schema:\n%s", h, schema)
		}
	}
}

func TestMigrateAllowsPrimeSessionKind(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var schema string
	if err := db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='sessions'",
	).Scan(&schema); err != nil {
		t.Fatalf("read sessions schema: %v", err)
	}
	if !strings.Contains(schema, "'"+string(domain.KindPrime)+"'") {
		t.Fatalf("sessions.kind CHECK is missing prime; schema:\n%s", schema)
	}

	if _, err := db.Exec(
		`INSERT INTO projects (id, path, registered_at) VALUES ('ao', '/tmp/ao', CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (id, project_id, num, kind, activity_last_at, created_at, updated_at)
		 VALUES ('ao-prime', 'ao', 1, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		string(domain.KindPrime),
	); err != nil {
		t.Fatalf("insert prime session: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (id, project_id, num, kind, activity_last_at, created_at, updated_at)
		 VALUES ('ao-prime-2', 'ao', 2, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		string(domain.KindPrime),
	); err == nil {
		t.Fatal("insert second active prime succeeded, want singleton constraint")
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (id, project_id, num, kind, is_terminated, activity_last_at, created_at, updated_at)
		 VALUES ('ao-prime-terminated', 'ao', 3, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		string(domain.KindPrime),
	); err != nil {
		t.Fatalf("insert terminated prime: %v", err)
	}
}

func TestMigrateAllowsModelHealthNotifications(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var schema string
	if err := db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='notifications'",
	).Scan(&schema); err != nil {
		t.Fatalf("read notifications schema: %v", err)
	}
	for _, typ := range []domain.NotificationType{domain.NotificationModelUnreachable, domain.NotificationModelRecovered} {
		if !strings.Contains(schema, "'"+string(typ)+"'") {
			t.Fatalf("notifications.type CHECK is missing %s; schema:\n%s", typ, schema)
		}
	}
}

func TestMigrateAllowsPrimeRestartNotification(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var schema string
	if err := db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='notifications'",
	).Scan(&schema); err != nil {
		t.Fatalf("read notifications schema: %v", err)
	}
	if !strings.Contains(schema, "'"+string(domain.NotificationPrimeRestartCapped)+"'") {
		t.Fatalf("notifications.type CHECK is missing prime restart cap; schema:\n%s", schema)
	}
}

// TestMigrateReadsPreModelSessionRowsAsEmpty guards the additive contract of
// the sessions.model column: a row written without a model — which is every row
// that existed before migration 0029 added the column — must read back as the
// empty string, not NULL, so the read path needs no nullable handling and no
// existing row is rewritten.
func TestMigrateReadsPreModelSessionRowsAsEmpty(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO projects (id, path, registered_at) VALUES ('mer', '/tmp/mer', CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	// The column list deliberately omits model: this is exactly the shape of a
	// row inserted by the pre-0029 InsertSession statement.
	if _, err := db.Exec(
		`INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at)
		 VALUES ('mer-1', 'mer', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("insert pre-model session row: %v", err)
	}

	var model string
	if err := db.QueryRow(`SELECT model FROM sessions WHERE id = 'mer-1'`).Scan(&model); err != nil {
		t.Fatalf("read model of pre-existing row: %v", err)
	}
	if model != "" {
		t.Fatalf("pre-existing session model = %q, want empty", model)
	}
}

// TestMigrateReadsPreMixSelectedSessionRowsAsFalse guards the additive contract
// of the sessions.mix_selected column: a row written without it — which is
// every row that existed before migration 0029 added the column — must read
// back false, so no existing session is retroactively counted against a worker
// mix bucket's share and no backfill is needed.
func TestMigrateReadsPreMixSelectedSessionRowsAsFalse(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO projects (id, path, registered_at) VALUES ('mer', '/tmp/mer', CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	// The column list deliberately omits mix_selected: this is exactly the shape
	// of a row inserted by the pre-0029 InsertSession statement.
	if _, err := db.Exec(
		`INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at)
		 VALUES ('mer-1', 'mer', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("insert pre-mix-selected session row: %v", err)
	}

	var mixSelected bool
	if err := db.QueryRow(`SELECT mix_selected FROM sessions WHERE id = 'mer-1'`).Scan(&mixSelected); err != nil {
		t.Fatalf("read mix_selected of pre-existing row: %v", err)
	}
	if mixSelected {
		t.Fatal("pre-existing session mix_selected = true, want false")
	}
}

func TestMigrateReadsPreEffortSessionRowsAsEmpty(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, path, registered_at) VALUES ('mer', '/tmp/mer', CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at)
		 VALUES ('mer-1', 'mer', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("insert pre-effort session row: %v", err)
	}

	var effort string
	if err := db.QueryRow(`SELECT effort FROM sessions WHERE id = 'mer-1'`).Scan(&effort); err != nil {
		t.Fatalf("read effort of pre-existing row: %v", err)
	}
	if effort != "" {
		t.Fatalf("pre-existing session effort = %q, want empty", effort)
	}
}

func TestOpenReadOnlyDoesNotCreateDatabase(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing")
	if _, err := OpenReadOnly(context.Background(), dataDir); err == nil {
		t.Fatal("OpenReadOnly succeeded for missing database")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir stat err = %v, want not exist", err)
	}
}

func TestOpenReadOnlyDoesNotMigrate(t *testing.T) {
	dataDir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    repo_origin_url TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    registered_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);
INSERT INTO projects (id, path, registered_at) VALUES ('alpha', '/repos/alpha', ?);
`, time.Unix(100, 0).UTC()); err != nil {
		_ = db.Close()
		t.Fatalf("seed old schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	store, err := OpenReadOnly(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no such column") {
		t.Fatalf("ListProjects err = %v, want old-schema column failure", err)
	}

	checkDB, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open check db: %v", err)
	}
	defer func() { _ = checkDB.Close() }()

	var schema string
	if err := checkDB.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='projects'",
	).Scan(&schema); err != nil {
		t.Fatalf("read projects schema: %v", err)
	}
	if strings.Contains(schema, "config") || strings.Contains(schema, "kind") {
		t.Fatalf("OpenReadOnly migrated projects schema:\n%s", schema)
	}
}
