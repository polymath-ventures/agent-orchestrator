package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// sessionsHarnessCheck returns the stored CREATE TABLE text for the sessions
// table, which carries the harness CHECK constraint.
func sessionsHarnessCheck(t *testing.T, db *sql.DB) string {
	t.Helper()
	var schema string
	if err := db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='sessions'",
	).Scan(&schema); err != nil {
		t.Fatalf("read sessions schema: %v", err)
	}
	return schema
}

// TestMigrateAdmitsCodexFuguOnUpgradeFromInitialPlatform reproduces the
// existing-database upgrade path that a fresh-migration test cannot: goose
// tracks applied migrations by version number, so a database already past the
// initial platform migration (0007) never re-runs it. Widening the harness
// allowlist by editing 0007 in place would therefore admit codex-fugu on fresh
// installs but silently leave every existing install rejecting it. This test
// migrates only through 0007, asserts codex-fugu is absent there, then runs the
// remaining migrations as a running daemon would on its next release and
// requires codex-fugu to be admitted — which only a later migration can do.
func TestMigrateAdmitsCodexFuguOnUpgradeFromInitialPlatform(t *testing.T) {
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
	// Simulate an existing install: migrated through the initial platform
	// migration but not beyond, before codex-fugu existed.
	if err := goose.UpTo(db, "migrations", 7); err != nil {
		gooseMu.Unlock()
		t.Fatalf("migrate to 0007: %v", err)
	}
	gooseMu.Unlock()

	if schema := sessionsHarnessCheck(t, db); strings.Contains(schema, "'codex-fugu'") {
		t.Fatalf("precondition failed: 0007 already lists codex-fugu; the upgrade path is not being exercised\n%s", schema)
	}

	// The next release runs the rest of the migrations. codex-fugu must be
	// admitted afterward — an in-place edit of the already-applied 0007 would
	// not be, because goose will not re-run it.
	if err := migrate(db); err != nil {
		t.Fatalf("upgrade migrate: %v", err)
	}

	if schema := sessionsHarnessCheck(t, db); !strings.Contains(schema, "'codex-fugu'") {
		t.Fatalf("codex-fugu not admitted after upgrade from 0007 — the widening migration no-opped on an existing DB; a new migration is required, not an in-place edit of 0007\n%s", schema)
	}
}
