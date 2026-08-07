package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A normal database at version 70 already has the fork's migrations 52-65 and
// the first three Chat migrations. Those old version numbers must not be
// mistaken for pre-renumbered Chat history: doing so marks 71-81 applied without
// running them and leaves generated queries pointed at schema that does not
// exist.
func TestMigrateFromVersion70AppliesRemainingChatMigrations(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	upTo(t, db, 70)

	if err := migrate(db); err != nil {
		t.Fatalf("migrate version 70 database: %v", err)
	}

	for _, field := range []struct {
		version int
		table   string
		column  string
	}{
		{71, "conversations", "compacted_at"},
		{72, "conversation_activities", "command_output"},
		{73, "conversations", "context_used"},
		{74, "conversations", "provider_title"},
		{75, "conversations", "model_reroute_json"},
		{78, "conversation_messages", "delivery_content_json"},
		{80, "session_interface_transitions", "phase"},
		{81, "session_interface_transition_messages", "client_message_id"},
	} {
		if !tableHasColumn(t, db, field.table, field.column) {
			t.Errorf("migration %04d schema is missing %s.%s", field.version, field.table, field.column)
		}
	}

	activitySchema := tableSchema(t, db, "conversation_activities")
	for _, effect := range []struct {
		version int
		value   string
	}{
		{76, "'mcp_tool'"},
		{77, "'user_input'"},
		{79, "'cancelled'"},
	} {
		if !strings.Contains(activitySchema, effect.value) {
			t.Errorf("migration %04d schema is missing %s in conversation_activities:\n%s", effect.version, effect.value, activitySchema)
		}
	}

	// Exercise the three table rebuilds with real data. Start from the same
	// version-70 interruption, seed the relational chain while it has the base
	// Chat shape, then stop immediately before the rebuilds after the payload
	// columns have been introduced.
	preserveDB, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open preservation sqlite: %v", err)
	}
	preserveDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = preserveDB.Close() })
	upTo(t, preserveDB, 70)
	mustExec(t, preserveDB, `INSERT INTO projects (id, path, display_name, registered_at)
		VALUES ('p1', '/tmp/p1', 'project', '2026-08-07T00:00:00Z')`)
	mustExec(t, preserveDB, `INSERT INTO sessions (
		id, project_id, num, kind, activity_state, activity_last_at, is_terminated,
		session_mode, created_at, updated_at
	) VALUES (
		'ao-1', 'p1', 1, 'worker', 'idle', '2026-08-07T00:00:00Z', 0,
		'chat', '2026-08-07T00:00:00Z', '2026-08-07T00:00:00Z'
	)`)
	mustExec(t, preserveDB, `INSERT INTO conversations (
		id, scope, project_id, session_id, current_session_id, latest_sequence,
		created_at, updated_at
	) VALUES (
		'conv-1', 'session', 'p1', 'ao-1', 'ao-1', 1,
		'2026-08-07T00:00:00Z', '2026-08-07T00:00:00Z'
	)`)
	mustExec(t, preserveDB, `INSERT INTO conversation_turns (
		id, conversation_id, handled_by_session_id, state, requested_at
	) VALUES (
		'turn-1', 'conv-1', 'ao-1', 'running', '2026-08-07T00:00:00Z'
	)`)
	mustExec(t, preserveDB, `INSERT INTO conversation_activities (
		id, conversation_id, turn_id, sequence, kind, status, summary,
		created_at, updated_at
	) VALUES (
		'activity-1', 'conv-1', 'turn-1', 1, 'command', 'running', 'build',
		'2026-08-07T00:00:00Z', '2026-08-07T00:00:00Z'
	)`)
	upTo(t, preserveDB, 75)
	mustExec(t, preserveDB, `UPDATE conversation_activities
		SET command_output = 'stdout payload', streamed_text = 'reasoning payload'
		WHERE id = 'activity-1'`)

	if err := migrate(preserveDB); err != nil {
		t.Fatalf("migrate rebuild preservation database: %v", err)
	}

	var projectID, sessionID, conversationID, turnID, activityID string
	var commandOutput, streamedText string
	if err := preserveDB.QueryRow(`
SELECT p.id, s.id, c.id, t.id, a.id, a.command_output, a.streamed_text
FROM projects p
JOIN sessions s ON s.project_id = p.id
JOIN conversations c ON c.current_session_id = s.id
JOIN conversation_turns t ON t.conversation_id = c.id
JOIN conversation_activities a ON a.turn_id = t.id
WHERE a.id = 'activity-1'
`).Scan(&projectID, &sessionID, &conversationID, &turnID, &activityID, &commandOutput, &streamedText); err != nil {
		t.Fatalf("read activity after rebuild migrations: %v", err)
	}
	if projectID != "p1" || sessionID != "ao-1" || conversationID != "conv-1" || turnID != "turn-1" || activityID != "activity-1" {
		t.Fatalf("rebuilt row identities = (%q, %q, %q, %q, %q)", projectID, sessionID, conversationID, turnID, activityID)
	}
	if commandOutput != "stdout payload" || streamedText != "reasoning payload" {
		t.Fatalf("rebuilt activity payloads = (%q, %q)", commandOutput, streamedText)
	}
}
