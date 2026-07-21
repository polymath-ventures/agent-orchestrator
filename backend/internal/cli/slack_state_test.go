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
	state.path = filepath.Join("/dev/null", "state.json")
	if err := state.record("n1"); err == nil {
		t.Fatal("record succeeded with unwritable path")
	}
	if state.contains("n1") || len(state.Delivered) != 0 {
		t.Fatalf("failed persist mutated state: %+v", state)
	}
}

func TestSlackDeliveryStateTailBoundsIDs(t *testing.T) {
	setConfigEnv(t)
	t.Setenv(slackStateEnv, filepath.Join(t.TempDir(), "state.json"))
	state, err := loadSlackDeliveryState()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, slackStateLimit+1)
	for i := range ids {
		ids[i] = string(rune(i + 1))
	}
	if err := state.record(ids...); err != nil {
		t.Fatal(err)
	}
	if len(state.Delivered) != slackStateLimit || state.contains(ids[0]) || !state.contains(ids[len(ids)-1]) {
		t.Fatalf("tail-bound state size=%d", len(state.Delivered))
	}
}
