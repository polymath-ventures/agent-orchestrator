package sqlite

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

var syncUpstreamMigrationLedger = map[int64]string{
	61: "0061_notification_resolution.sql",
	62: "0062_review_run_unique_per_harness.sql",
	63: "0063_add_session_pinned.sql",
	64: "0064_backfill_review_run_batch_id.sql",
	65: "0065_agent_model_catalog.sql",
	66: "0066_model_usage.sql",
	67: "0067_allow_muse_harness.sql",
	68: "0068_chat_session_mode.sql",
	69: "0069_app_settings.sql",
	70: "0070_conversation_turn_settings.sql",
	71: "0071_conversation_compaction.sql",
	72: "0072_command_output_and_diffs.sql",
	73: "0073_conversation_usage.sql",
	74: "0074_conversation_history_ops.sql",
	75: "0075_conversation_provider_state.sql",
	76: "0076_activity_kinds_mcp_and_auto_review.sql",
	77: "0077_conversation_user_input.sql",
	78: "0078_conversation_delivery_content_and_cost.sql",
	79: "0079_cancelled_conversation_activities.sql",
	80: "0080_session_interface_transitions.sql",
	81: "0081_session_interface_transition_delivery.sql",
	82: "0082_allow_prime_agent_harness.sql",
	83: "0083_reconcile_kimchi_prime_agent_harnesses.sql",
	84: "0084_add_session_auto_inject_review.sql",
	85: "0085_agent_switching.sql",
	86: "0086_workspace_repo_default_branch.sql",
	87: "0087_conversation_branches.sql",
	88: "0088_add_auto_inject_ci_toggle.sql",
	89: "0089_review_agent_session_id.sql",
	90: "0090_review_per_harness.sql",
	91: "0091_browser_capability_verifier.sql",
	92: "0092_session_initial_context.sql",
}

func TestSyncUpstreamMigrationLedger(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	found := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, err := goose.NumericComponent(entry.Name())
		if err != nil {
			t.Fatalf("parse migration %q: %v", entry.Name(), err)
		}
		if version >= 61 {
			found[version] = entry.Name()
		}
	}

	if len(found) != len(syncUpstreamMigrationLedger) {
		t.Fatalf("post-fork migration count = %d, want %d: %v", len(found), len(syncUpstreamMigrationLedger), found)
	}
	for version, wantName := range syncUpstreamMigrationLedger {
		if gotName := found[version]; gotName != wantName {
			t.Errorf("migration %d = %q, want %q", version, gotName, wantName)
		}
	}
}

func TestSyncUpstreamUpgradesShippedVersion60Database(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		gooseMu.Unlock()
		t.Fatalf("set goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 60); err != nil {
		gooseMu.Unlock()
		t.Fatalf("migrate shipped schema to 60: %v", err)
	}
	gooseMu.Unlock()

	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at)
VALUES ('sync-project', '/tmp/sync-project', CURRENT_TIMESTAMP);
INSERT INTO sessions (
    id, project_id, num, harness, activity_last_at, last_error, created_at, updated_at
) VALUES (
    'sync-project-1', 'sync-project', 1, 'codex-fugu', CURRENT_TIMESTAMP,
    'preserve-me', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
);`); err != nil {
		t.Fatalf("seed shipped version 60 schema: %v", err)
	}

	gooseMu.Lock()
	if err := goose.UpTo(db, "migrations", 83); err != nil {
		gooseMu.Unlock()
		t.Fatalf("apply incoming harness migrations: %v", err)
	}
	gooseMu.Unlock()
	for _, harness := range []string{"'codex-fugu'", "'kimchi'", "'prime-agent'"} {
		if schema := tableSchema(t, db, "sessions"); !strings.Contains(schema, harness) {
			t.Fatalf("incoming harness migrations silently omitted %s:\n%s", harness, schema)
		}
	}

	beforeLedger := appliedLedgerAtOrBelow(t, db, 60)
	if err := migrate(db); err != nil {
		t.Fatalf("upgrade version 60 database: %v", err)
	}

	if got := appliedLedgerAtOrBelow(t, db, 60); got != beforeLedger {
		t.Fatalf("shipped migration ledger changed during upgrade:\nbefore: %s\nafter:  %s", beforeLedger, got)
	}

	var currentVersion int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`).Scan(&currentVersion); err != nil {
		t.Fatalf("read current migration version: %v", err)
	}
	if currentVersion != 92 {
		t.Fatalf("current migration version = %d, want 92", currentVersion)
	}
	var newVersions int
	if err := db.QueryRow(`
SELECT COUNT(DISTINCT version_id)
FROM goose_db_version
WHERE is_applied = 1 AND version_id BETWEEN 61 AND 92
`).Scan(&newVersions); err != nil {
		t.Fatalf("count new migration versions: %v", err)
	}
	if newVersions != 32 {
		t.Fatalf("applied migration versions 61..92 = %d, want 32", newVersions)
	}

	schema := tableSchema(t, db, "sessions")
	for _, harness := range []string{"'codex-fugu'", "'muse'", "'kimchi'", "'prime-agent'"} {
		if !strings.Contains(schema, harness) {
			t.Errorf("sessions harness CHECK is missing %s:\n%s", harness, schema)
		}
	}
	if _, err := db.Exec(`
INSERT INTO sessions (id, project_id, num, harness, activity_last_at, created_at, updated_at)
VALUES ('sync-project-2', 'sync-project', 2, 'muse', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
`); err != nil {
		t.Fatalf("insert Muse session after upgrade: %v", err)
	}
	for number, harness := range []string{"kimchi", "prime-agent"} {
		if _, err := db.Exec(`
INSERT INTO sessions (id, project_id, num, harness, activity_last_at, created_at, updated_at)
VALUES (?, 'sync-project', ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
`, fmt.Sprintf("sync-project-%d", number+3), number+3, harness); err != nil {
			t.Fatalf("insert %s session after upgrade: %v", harness, err)
		}
	}

	for _, field := range []struct {
		table  string
		column string
	}{
		{"notifications", "resolved_at"},
		{"sessions", "session_mode"},
		{"sessions", "provider_conversation_id"},
		{"agent_model_catalog", "catalog_json"},
		{"model_usage_events", "source_event_key"},
		{"conversations", "current_session_id"},
		{"session_interface_transitions", "phase"},
		{"session_interface_transition_messages", "client_message_id"},
	} {
		if !tableHasColumn(t, db, field.table, field.column) {
			t.Errorf("upgraded schema is missing %s.%s", field.table, field.column)
		}
	}

	trigger := triggerSchema(t, db, "sessions_cdc_update")
	for _, field := range []string{"last_error", "is_pinned", "session_mode"} {
		if !strings.Contains(trigger, field) {
			t.Errorf("sessions_cdc_update is missing %s invalidation:\n%s", field, trigger)
		}
	}
	if _, err := db.Exec(`DELETE FROM change_log; UPDATE sessions SET last_error = 'changed' WHERE id = 'sync-project-1'`); err != nil {
		t.Fatalf("update last_error after upgrade: %v", err)
	}
	var changes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM change_log WHERE session_id = 'sync-project-1' AND event_type = 'session_updated'`).Scan(&changes); err != nil {
		t.Fatalf("count last_error invalidations: %v", err)
	}
	if changes != 1 {
		t.Fatalf("last_error invalidations = %d, want 1", changes)
	}

	var ledgerRowsBefore int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&ledgerRowsBefore); err != nil {
		t.Fatalf("count migration rows before repeat: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("repeat migration pass: %v", err)
	}
	var ledgerRowsAfter int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&ledgerRowsAfter); err != nil {
		t.Fatalf("count migration rows after repeat: %v", err)
	}
	if ledgerRowsAfter != ledgerRowsBefore {
		t.Fatalf("repeat migration changed goose ledger row count from %d to %d", ledgerRowsBefore, ledgerRowsAfter)
	}
}

func appliedLedgerAtOrBelow(t *testing.T, db *sql.DB, maxVersion int64) string {
	t.Helper()
	rows, err := db.Query(`
SELECT id, version_id, is_applied
FROM goose_db_version
WHERE version_id <= ?
ORDER BY id
`, maxVersion)
	if err != nil {
		t.Fatalf("read shipped migration ledger: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ledger strings.Builder
	for rows.Next() {
		var id, version int64
		var applied bool
		if err := rows.Scan(&id, &version, &applied); err != nil {
			t.Fatalf("scan shipped migration ledger: %v", err)
		}
		_, _ = fmt.Fprintf(&ledger, "%d:%d:%t;", id, version, applied)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate shipped migration ledger: %v", err)
	}
	return ledger.String()
}

func tableSchema(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&schema); err != nil {
		t.Fatalf("read %s schema: %v", table, err)
	}
	return schema
}

func triggerSchema(t *testing.T, db *sql.DB, trigger string) string {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trigger).Scan(&schema); err != nil {
		t.Fatalf("read %s trigger: %v", trigger, err)
	}
	return schema
}

func tableHasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
		t.Fatalf("inspect %s.%s: %v", table, column, err)
	}
	return count == 1
}
