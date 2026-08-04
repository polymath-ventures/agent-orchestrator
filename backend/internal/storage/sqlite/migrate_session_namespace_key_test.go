package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestMigrateSessionNamespaceKeyLeavesLegacyResourcesUnchanged(t *testing.T) {
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
	if err := goose.UpTo(db, "migrations", 59); err != nil {
		gooseMu.Unlock()
		t.Fatalf("migrate to 0059: %v", err)
	}
	gooseMu.Unlock()

	now := "2026-08-04T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO projects (id, path, registered_at) VALUES ('mer', '/tmp/mer', ?)`, now); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (id, project_id, num, activity_last_at, branch, workspace_path, runtime_handle_id, created_at, updated_at)
		 VALUES ('mer-7-0123456789abcdef', 'mer', 7, ?, 'ao/mer-7/root', '/ws/mer-7', 'mer-7', ?, ?)`, now, now, now,
	); err != nil {
		t.Fatalf("seed legacy session: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("upgrade migrate: %v", err)
	}

	var key, branch, workspace, handle string
	if err := db.QueryRow(
		`SELECT namespace_key, branch, workspace_path, runtime_handle_id FROM sessions WHERE num = 7`,
	).Scan(&key, &branch, &workspace, &handle); err != nil {
		t.Fatalf("read upgraded session: %v", err)
	}
	if key != "" {
		t.Fatalf("legacy namespace key = %q, want empty", key)
	}
	if branch != "ao/mer-7/root" || workspace != "/ws/mer-7" || handle != "mer-7" {
		t.Fatalf("legacy resources changed: branch=%q workspace=%q handle=%q", branch, workspace, handle)
	}
}
