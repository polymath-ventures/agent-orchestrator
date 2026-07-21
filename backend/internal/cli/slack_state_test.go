package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlackDeliveryStatePersistsAndReloads(t *testing.T) {
	cfg := setConfigEnv(t)
	path := filepath.Join(cfg.dataDir, "nested", "state.json")
	t.Setenv(slackStateEnv, path)
	_ = os.Remove(path)

	state, err := loadSlackDeliveryState()
	if err != nil {
		t.Fatalf("load fresh state: %v", err)
	}
	if state.Initialized || state.contains("n1") {
		t.Fatalf("fresh state = %+v", state)
	}
	if err := state.initialize([]string{"n1", "n2"}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	reloaded, err := loadSlackDeliveryState()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.Initialized || !reloaded.contains("n1") || !reloaded.contains("n2") {
		t.Fatalf("reloaded = %+v", reloaded)
	}
}

func TestSlackDeliveryStateDoesNotMutateOnPersistFailure(t *testing.T) {
	setConfigEnv(t)
	t.Setenv(slackStateEnv, filepath.Join(t.TempDir(), "state.json"))
	state, err := loadSlackDeliveryState()
	if err != nil {
		t.Fatal(err)
	}
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.path = filepath.Join(parentFile, "state.json")
	if err := state.record("n1"); err == nil {
		t.Fatal("record succeeded with unwritable path")
	}
	if state.contains("n1") || len(state.Delivered) != 0 {
		t.Fatalf("failed persist mutated state: %+v", state)
	}
}

func TestSlackDeliveryStateRetainsAllIDs(t *testing.T) {
	setConfigEnv(t)
	t.Setenv(slackStateEnv, filepath.Join(t.TempDir(), "state.json"))
	state, err := loadSlackDeliveryState()
	if err != nil {
		t.Fatal(err)
	}
	const count = 2001
	ids := make([]string, count)
	for i := range ids {
		ids[i] = string(rune(i + 1))
	}
	if err := state.record(ids...); err != nil {
		t.Fatal(err)
	}
	if len(state.Delivered) != count || !state.contains(ids[0]) || !state.contains(ids[len(ids)-1]) {
		t.Fatalf("unbounded state size=%d", len(state.Delivered))
	}
}

func TestSlackSeedIDsRetainsWholeBacklog(t *testing.T) {
	setConfigEnv(t)
	t.Setenv(slackStateEnv, filepath.Join(t.TempDir(), "state.json"))
	const count = 2001
	unread := make([]slackNotification, count)
	for i := range unread {
		unread[i].ID = string(rune(i + 1)) // daemon order: newest first
	}
	state, err := loadSlackDeliveryState()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.initialize(slackSeedIDs(unread)); err != nil {
		t.Fatal(err)
	}
	if len(state.Delivered) != count || !state.contains(unread[0].ID) || !state.contains(unread[len(unread)-1].ID) {
		t.Fatalf("seed did not retain whole backlog")
	}
}

func TestSlackDeliveryStateRecoversCorruptFile(t *testing.T) {
	setConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	t.Setenv(slackStateEnv, path)
	if err := os.WriteFile(path, []byte(`{"broken"`), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := loadSlackDeliveryState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Initialized || state.warning == "" {
		t.Fatalf("recovered state = %+v", state)
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("corrupt backup matches=%v err=%v", matches, err)
	}
}
