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

func TestStoreQuotaCollectorSuppressesStaticNoSignalWhenExactSignalExists(t *testing.T) {
	used, remaining, limit := 12.0, 88.0, 100.0
	store := &fakeQuotaStore{rows: []domain.QuotaSnapshot{
		{
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
	var codexRows, claudeRows int
	for _, row := range rows {
		switch row.Harness {
		case domain.HarnessCodex:
			codexRows++
			if row.SignalQuality == domain.QuotaSignalNone {
				t.Fatalf("Codex no-signal row should be suppressed when exact signal exists: %+v", rows)
			}
		case domain.HarnessClaudeCode:
			claudeRows++
			if row.SignalQuality != domain.QuotaSignalNone {
				t.Fatalf("Claude fallback should remain no-signal: %+v", row)
			}
		}
	}
	if codexRows != 1 || claudeRows != 1 {
		t.Fatalf("rows = %+v, want one exact Codex row and one Claude no-signal row", rows)
	}
}

func TestStoreQuotaCollectorDoesNotRewriteStaticNoSignalRows(t *testing.T) {
	store := &fakeQuotaStore{}
	firstAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(time.Hour)

	if _, err := NewStoreQuotaCollector(store).CollectQuota(context.Background(), firstAt); err != nil {
		t.Fatalf("first CollectQuota: %v", err)
	}
	if _, err := NewStoreQuotaCollector(store).CollectQuota(context.Background(), secondAt); err != nil {
		t.Fatalf("second CollectQuota: %v", err)
	}
	for _, row := range store.rows {
		if !row.ObservedAt.Equal(firstAt) {
			t.Fatalf("static no-signal row was rewritten on second collect: %+v", row)
		}
	}
}
