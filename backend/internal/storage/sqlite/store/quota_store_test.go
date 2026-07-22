package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestQuotaStoreListLatestBreaksObservedAtTiesByWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	observedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	olderWindowEnd := observedAt.Add(5 * time.Hour)
	newerWindowEnd := observedAt.Add(10 * time.Hour)

	for _, snap := range []domain.QuotaSnapshot{
		{
			Harness:       domain.HarnessCodex,
			AccountID:     "chatgpt",
			Model:         "gpt-5",
			WindowStart:   observedAt,
			WindowEnd:     olderWindowEnd,
			SignalQuality: domain.QuotaSignalEstimated,
			ObservedAt:    observedAt,
		},
		{
			Harness:       domain.HarnessCodex,
			AccountID:     "chatgpt",
			Model:         "gpt-5",
			WindowStart:   observedAt,
			WindowEnd:     newerWindowEnd,
			SignalQuality: domain.QuotaSignalEstimated,
			ObservedAt:    observedAt,
		},
	} {
		if _, err := s.UpsertQuotaSnapshot(ctx, snap); err != nil {
			t.Fatalf("UpsertQuotaSnapshot: %v", err)
		}
	}

	rows, err := s.ListLatestQuotaSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListLatestQuotaSnapshots: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one latest row", rows)
	}
	if !rows[0].WindowEnd.Equal(newerWindowEnd) {
		t.Fatalf("WindowEnd = %s, want %s", rows[0].WindowEnd, newerWindowEnd)
	}
}

func TestQuotaStoreKeepsDistinctWindowNames(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	observedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	windowEnd := observedAt.Add(7 * 24 * time.Hour)
	used, remaining, limit := 92.0, 8.0, 100.0

	for _, name := range []string{"primary", "secondary"} {
		if _, err := s.UpsertQuotaSnapshot(ctx, domain.QuotaSnapshot{
			Harness:       domain.HarnessCodex,
			AccountID:     "unknown",
			WindowName:    name,
			WindowStart:   observedAt,
			WindowEnd:     windowEnd,
			Used:          &used,
			Remaining:     &remaining,
			Limit:         &limit,
			SignalQuality: domain.QuotaSignalExact,
			Source:        "codex rollout token_count.rate_limits",
			ObservedAt:    observedAt,
		}); err != nil {
			t.Fatalf("UpsertQuotaSnapshot(%s): %v", name, err)
		}
	}

	rows, err := s.ListLatestQuotaSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListLatestQuotaSnapshots: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want primary and secondary", rows)
	}
	if rows[0].WindowName != "primary" || rows[1].WindowName != "secondary" {
		t.Fatalf("window names = %q, %q; want primary, secondary", rows[0].WindowName, rows[1].WindowName)
	}
}
