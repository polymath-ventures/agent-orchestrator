package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestEvaluatorZeroThresholdDisables(t *testing.T) {
	e := newEvaluator(Thresholds{})
	remaining, limit := 1.0, 100.0
	alerts, tr := e.evaluate(Snapshot{Quotas: []domain.QuotaSnapshot{{
		Harness:       domain.HarnessCodex,
		AccountID:     "chatgpt",
		Remaining:     &remaining,
		Limit:         &limit,
		SignalQuality: domain.QuotaSignalEstimated,
	}}})
	if len(alerts) != 0 || len(tr) != 0 {
		t.Fatalf("zero thresholds must disable alerts, got alerts=%+v tr=%+v", alerts, tr)
	}
}

func TestEvaluatorLowQuotaIgnoresNoSignal(t *testing.T) {
	e := newEvaluator(Thresholds{LowQuotaPercent: 10})
	alerts, tr := e.evaluate(Snapshot{Quotas: []domain.QuotaSnapshot{{
		Harness:       domain.HarnessClaudeCode,
		AccountID:     "unknown",
		SignalQuality: domain.QuotaSignalNone,
		ObservedAt:    time.Unix(1, 0).UTC(),
	}}})
	if len(alerts) != 0 || len(tr) != 0 {
		t.Fatalf("no-signal quota must not alert, got alerts=%+v tr=%+v", alerts, tr)
	}
}

func TestEvaluatorLowQuotaFiresAndDedupePerSubject(t *testing.T) {
	e := newEvaluator(Thresholds{LowQuotaPercent: 10})
	codexRemaining, claudeRemaining, limit := 9.0, 2.0, 100.0
	windowStart := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(5 * time.Hour)
	snap := Snapshot{Quotas: []domain.QuotaSnapshot{
		{
			Harness:       domain.HarnessCodex,
			AccountID:     "chatgpt",
			Remaining:     &codexRemaining,
			Limit:         &limit,
			SignalQuality: domain.QuotaSignalEstimated,
			WindowStart:   windowStart,
			WindowEnd:     windowEnd,
			ObservedAt:    time.Unix(1, 0).UTC(),
		},
		{
			Harness:       domain.HarnessClaudeCode,
			AccountID:     "max",
			Remaining:     &claudeRemaining,
			Limit:         &limit,
			SignalQuality: domain.QuotaSignalExact,
			WindowStart:   windowStart,
			WindowEnd:     windowEnd,
			ObservedAt:    time.Unix(1, 0).UTC(),
		},
	}}
	alerts, tr := e.evaluate(snap)
	if len(alerts) != 2 || len(tr) != 2 {
		t.Fatalf("want two low-quota alerts/transitions, got alerts=%+v tr=%+v", alerts, tr)
	}
	for _, alert := range alerts {
		if alert.Kind != AlertQuotaLow {
			t.Fatalf("unexpected alert kind: %+v", alert)
		}
		if !strings.Contains(alert.Subject, "window:2026-07-20T10:00:00Z-2026-07-20T15:00:00Z") {
			t.Fatalf("subject must include quota window for per-window dedupe: %q", alert.Subject)
		}
	}

	_, tr = e.evaluate(snap)
	if len(tr) != 0 {
		t.Fatalf("sustained low quota must dedupe per subject, got %+v", tr)
	}
}

func TestEvaluatorLowQuotaClearsPerSubject(t *testing.T) {
	e := newEvaluator(Thresholds{LowQuotaPercent: 10})
	remaining, limit := 9.0, 100.0
	snap := Snapshot{Quotas: []domain.QuotaSnapshot{{
		Harness:       domain.HarnessCodex,
		AccountID:     "chatgpt",
		Remaining:     &remaining,
		Limit:         &limit,
		SignalQuality: domain.QuotaSignalEstimated,
	}}}
	e.evaluate(snap)

	remaining = 20
	alerts, tr := e.evaluate(snap)
	if len(alerts) != 0 {
		t.Fatalf("want no firing alerts after quota recovers, got %+v", alerts)
	}
	if len(tr) != 1 || tr[0].Firing {
		t.Fatalf("want one clear transition, got %+v", tr)
	}
}

func TestEvaluatorLowQuotaCanUseReportedUsedPercent(t *testing.T) {
	e := newEvaluator(Thresholds{LowQuotaPercent: 10})
	used, limit := 92.0, 100.0
	alerts, tr := e.evaluate(Snapshot{Quotas: []domain.QuotaSnapshot{{
		Harness:       domain.HarnessCodex,
		AccountID:     "unknown",
		WindowName:    "primary",
		Used:          &used,
		Limit:         &limit,
		SignalQuality: domain.QuotaSignalExact,
		ObservedAt:    time.Unix(1, 0).UTC(),
	}}})
	if len(alerts) != 1 || len(tr) != 1 || !tr[0].Firing {
		t.Fatalf("used_percent 92 with threshold 10 should alert, got alerts=%+v tr=%+v", alerts, tr)
	}
	if alerts[0].Value != 8 {
		t.Fatalf("alert value = %.1f, want 8.0 remaining percent derived from used_percent", alerts[0].Value)
	}
	if !strings.Contains(alerts[0].Subject, "/primary/") {
		t.Fatalf("alert subject should include quota window name, got %q", alerts[0].Subject)
	}
}
