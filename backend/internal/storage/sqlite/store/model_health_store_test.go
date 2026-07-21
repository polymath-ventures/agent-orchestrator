package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestModelHealthStoreUpsertAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	observed := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)

	if _, err := s.UpsertModelHealth(ctx, domain.ModelAvailability{
		ProjectID:  "mer",
		Harness:    domain.HarnessCodex,
		Model:      "gpt-5-codex",
		Status:     domain.ModelAvailabilityUnreachable,
		Reason:     domain.ModelAvailabilityReasonUnreachable,
		Message:    "model not found",
		ObservedAt: observed,
		UpdatedAt:  observed,
	}); err != nil {
		t.Fatalf("UpsertModelHealth: %v", err)
	}
	if _, err := s.UpsertModelHealth(ctx, domain.ModelAvailability{
		ProjectID:  "mer",
		Harness:    domain.HarnessCodex,
		Model:      "gpt-5-codex",
		Status:     domain.ModelAvailabilityReachable,
		Reason:     domain.ModelAvailabilityReasonRecovered,
		ObservedAt: observed.Add(time.Minute),
		UpdatedAt:  observed.Add(time.Minute),
	}); err != nil {
		t.Fatalf("second UpsertModelHealth: %v", err)
	}

	rows, err := s.ListModelHealthByProject(ctx, "mer")
	if err != nil {
		t.Fatalf("ListModelHealthByProject: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one upserted row", rows)
	}
	if got := rows[0]; got.Status != domain.ModelAvailabilityReachable || got.Reason != domain.ModelAvailabilityReasonRecovered || !got.ObservedAt.Equal(observed.Add(time.Minute)) {
		t.Fatalf("row = %+v, want recovered latest", got)
	}
}
