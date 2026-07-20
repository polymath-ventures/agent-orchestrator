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
		if row.Harness == snap.Harness && row.AccountID == snap.AccountID && row.Model == snap.Model {
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

func TestStoreQuotaCollectorRecordsNoSignalForSubscriptionHarnesses(t *testing.T) {
	store := &fakeQuotaStore{}
	observedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	rows, err := NewStoreQuotaCollector(store).CollectQuota(context.Background(), observedAt)
	if err != nil {
		t.Fatalf("CollectQuota returned error: %v", err)
	}

	want := map[domain.AgentHarness]bool{
		domain.HarnessClaudeCode: false,
		domain.HarnessCodex:      false,
		harnessCodexFugu:         false,
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d quota rows, want %d: %+v", len(rows), len(want), rows)
	}
	for _, row := range rows {
		if _, ok := want[row.Harness]; !ok {
			t.Fatalf("unexpected quota harness %q", row.Harness)
		}
		want[row.Harness] = true
		if row.SignalQuality != domain.QuotaSignalNone {
			t.Errorf("%s signal = %q, want none", row.Harness, row.SignalQuality)
		}
		if row.AccountID != "unknown" {
			t.Errorf("%s account = %q, want unknown", row.Harness, row.AccountID)
		}
		if !row.ObservedAt.Equal(observedAt) {
			t.Errorf("%s observedAt = %s, want %s", row.Harness, row.ObservedAt, observedAt)
		}
		if row.Limit != nil || row.Remaining != nil || row.Used != nil {
			t.Errorf("%s no-signal snapshot must not fabricate numeric values: %+v", row.Harness, row)
		}
	}
	for harness, seen := range want {
		if !seen {
			t.Errorf("missing quota snapshot for %s", harness)
		}
	}
}
