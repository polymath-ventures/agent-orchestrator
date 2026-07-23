package metrics

import (
	"context"
	"strings"
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

// TestLowQuotaAlertFiresOnProbePersistedSnapshot is the GH #97 regression that
// low-quota alerts key off daemon-probe data, not just hook-ingested data. A
// probe persists an exact snapshot to quota_snapshots; the collector reads it
// and the evaluator must fire quota_low when remaining is at/below threshold and
// stay quiet when it is healthy — proving the probe → persist → collect → alert
// path end to end for both a used-percent (claude) and a remaining (codex) row.
func TestLowQuotaAlertFiresOnProbePersistedSnapshot(t *testing.T) {
	// claude -p /usage reporting 95% used on the weekly all-models window.
	lowUsed, lowRemaining, limit := 95.0, 5.0, 100.0
	// a healthy codex primary window (17% used → 83% remaining) must not fire.
	okUsed, okRemaining := 17.0, 83.0
	store := &fakeQuotaStore{rows: []domain.QuotaSnapshot{
		{
			Harness: domain.HarnessClaudeCode, AccountID: "unknown", WindowName: "week (all models)",
			Used: &lowUsed, Remaining: &lowRemaining, Limit: &limit,
			SignalQuality: domain.QuotaSignalExact, Source: "claude -p /usage", ObservedAt: time.Unix(10, 0).UTC(),
		},
		{
			Harness: domain.HarnessCodex, AccountID: "unknown", WindowName: "primary",
			Used: &okUsed, Remaining: &okRemaining, Limit: &limit,
			SignalQuality: domain.QuotaSignalExact, Source: "codex rollout token_count.rate_limits", ObservedAt: time.Unix(10, 0).UTC(),
		},
	}}

	rows, err := NewStoreQuotaCollector(store).CollectQuota(context.Background(), time.Unix(20, 0).UTC())
	if err != nil {
		t.Fatalf("CollectQuota: %v", err)
	}

	alerts, transitions := newEvaluator(Thresholds{LowQuotaPercent: 10}).evaluate(Snapshot{Quotas: rows})
	if len(alerts) != 1 {
		t.Fatalf("got %d firing alerts, want exactly 1 (the low claude window): %+v", len(alerts), alerts)
	}
	if alerts[0].Kind != AlertQuotaLow {
		t.Fatalf("alert kind = %q, want %q", alerts[0].Kind, AlertQuotaLow)
	}
	if alerts[0].Value != 5.0 {
		t.Errorf("alert value = %.1f, want 5.0%% remaining", alerts[0].Value)
	}
	if !strings.Contains(alerts[0].Subject, string(domain.HarnessClaudeCode)) {
		t.Errorf("alert subject %q should name the claude-code harness", alerts[0].Subject)
	}
	if len(transitions) != 1 || !transitions[0].Firing {
		t.Fatalf("want one firing transition, got %+v", transitions)
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
