package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type fakeQuotaStore struct {
	rows []domain.QuotaSnapshot
}

func (f *fakeQuotaStore) UpsertQuotaSnapshot(_ context.Context, snap domain.QuotaSnapshot) (domain.QuotaSnapshot, error) {
	for i, row := range f.rows {
		if row.Harness == snap.Harness && row.AccountID == snap.AccountID && row.Model == snap.Model && row.WindowName == snap.WindowName {
			f.rows[i] = snap
			return snap, nil
		}
	}
	f.rows = append(f.rows, snap)
	return snap, nil
}

func (f *fakeQuotaStore) ListLatestQuotaSnapshots(context.Context) ([]domain.QuotaSnapshot, error) {
	return append([]domain.QuotaSnapshot(nil), f.rows...), nil
}

// TestStoreQuotaCollectorDoesNotFabricatePlaceholders reproduces GH #97: on a
// fresh system with no probed data, the collector must not fabricate
// "unknown / no signal / none" placeholder rows. Quota is a property of the
// harness login discovered by daemon probes, not something the collector
// invents on every tick. An empty store must yield an empty result and no
// writes.
func TestStoreQuotaCollectorDoesNotFabricatePlaceholders(t *testing.T) {
	store := &fakeQuotaStore{}
	observedAt := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	rows, err := NewStoreQuotaCollector(store).CollectQuota(context.Background(), observedAt)
	if err != nil {
		t.Fatalf("CollectQuota returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty store must yield no quota rows, got %d: %+v", len(rows), rows)
	}
	if len(store.rows) != 0 {
		t.Fatalf("collector must not write placeholder rows, store has %d: %+v", len(store.rows), store.rows)
	}
}

func TestStoreQuotaCollectorPassesThroughRealRowsAndDropsPlaceholders(t *testing.T) {
	used, remaining, limit := 12.0, 88.0, 100.0
	store := &fakeQuotaStore{rows: []domain.QuotaSnapshot{
		{
			// Legacy placeholder row left behind by the pre-probe implementation:
			// must never surface, even before the purge migration runs.
			Harness:       domain.HarnessCodex,
			AccountID:     "unknown",
			SignalQuality: domain.QuotaSignalNone,
			Source:        "old static row",
			ObservedAt:    time.Unix(1, 0).UTC(),
		},
		{
			Harness:       domain.HarnessCodex,
			AccountID:     "unknown",
			WindowName:    "primary",
			Used:          &used,
			Remaining:     &remaining,
			Limit:         &limit,
			SignalQuality: domain.QuotaSignalExact,
			Source:        "codex rollout token_count.rate_limits",
			ObservedAt:    time.Unix(2, 0).UTC(),
		},
	}}

	rows, err := NewStoreQuotaCollector(store).CollectQuota(context.Background(), time.Unix(3, 0).UTC())
	if err != nil {
		t.Fatalf("CollectQuota returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (real codex row, placeholder dropped): %+v", len(rows), rows)
	}
	if rows[0].Harness != domain.HarnessCodex || rows[0].SignalQuality != domain.QuotaSignalExact {
		t.Fatalf("expected the exact codex row, got %+v", rows[0])
	}
	// The collector must never fabricate a claude-code row (no probe data yet).
	for _, row := range rows {
		if row.Harness == domain.HarnessClaudeCode {
			t.Fatalf("collector fabricated a claude-code row: %+v", row)
		}
	}
	// It must not write anything either.
	if len(store.rows) != 2 {
		t.Fatalf("collector must not write rows, store has %d: %+v", len(store.rows), store.rows)
	}
}
