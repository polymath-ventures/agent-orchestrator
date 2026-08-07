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
}
