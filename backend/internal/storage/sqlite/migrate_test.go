package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

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
