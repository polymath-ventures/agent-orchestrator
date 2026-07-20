package metrics

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func fixedClock() func() time.Time {
	now := time.Unix(100, 0).UTC()
	return func() time.Time { return now }
}

type fakeCost struct {
	c   Cost
	err error
}

func (f fakeCost) Aggregate(context.Context, time.Time) (Cost, error) { return f.c, f.err }

type captureSink struct{ transitions []AlertTransition }

func (c *captureSink) EmitAlert(_ context.Context, t AlertTransition) {
	c.transitions = append(c.transitions, t)
}

type fakeQuotaCollector struct {
	rows []domain.QuotaSnapshot
	err  error
}

func (f fakeQuotaCollector) CollectQuota(context.Context, time.Time) ([]domain.QuotaSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func quotaSnap(harness domain.AgentHarness, account string, remaining, limit float64) domain.QuotaSnapshot {
	return domain.QuotaSnapshot{
		Harness:       harness,
		AccountID:     account,
		Remaining:     &remaining,
		Limit:         &limit,
		SignalQuality: domain.QuotaSignalEstimated,
		ObservedAt:    time.Unix(1, 0).UTC(),
	}
}

func TestObserverTickProducesUsageQuotaSnapshot(t *testing.T) {
	remaining, limit := 9.0, 100.0

	o := New(Deps{
		Cost:  fakeCost{c: Cost{CostTotals: CostTotals{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CostUSD: 0.5, Events: 2}}},
		Quota: fakeQuotaCollector{rows: []domain.QuotaSnapshot{quotaSnap(domain.HarnessCodex, "chatgpt", remaining, limit)}},
	}, Config{Clock: fixedClock(), Logger: quietLogger(), CostWindow: time.Hour})

	snap := o.Tick(context.Background())

	if snap.Cost.TotalTokens != 15 || snap.Cost.WindowSeconds != 3600 {
		t.Errorf("cost wrong: %+v", snap.Cost)
	}
	if len(snap.Quotas) == 0 {
		t.Fatalf("quota snapshots should be present")
	}
	if latest, ok := o.Latest(); !ok || !latest.CollectedAt.Equal(snap.CollectedAt) {
		t.Errorf("Latest did not return the tick snapshot")
	}
	if len(o.History()) != 1 {
		t.Errorf("history should have 1 entry, got %d", len(o.History()))
	}
}

func TestObserverEmitsQuotaAlertTransitions(t *testing.T) {
	sink := &captureSink{}
	remaining, limit := 9.0, 100.0
	o := New(Deps{
		Quota:  fakeQuotaCollector{rows: []domain.QuotaSnapshot{quotaSnap(domain.HarnessCodex, "chatgpt", remaining, limit)}},
		Alerts: sink,
	}, Config{
		Clock: fixedClock(), Logger: quietLogger(),
		Thresholds: Thresholds{LowQuotaPercent: 10},
	})
	o.Tick(context.Background())
	if len(sink.transitions) != 1 || !sink.transitions[0].Firing {
		t.Fatalf("want one firing transition emitted, got %+v", sink.transitions)
	}
	o.Tick(context.Background())
	if len(sink.transitions) != 1 {
		t.Fatalf("sustained condition must not re-emit, got %+v", sink.transitions)
	}
}

func TestObserverHistoryBounded(t *testing.T) {
	o := New(Deps{}, Config{Clock: fixedClock(), Logger: quietLogger(), History: 3})
	for i := 0; i < 5; i++ {
		o.Tick(context.Background())
	}
	if got := len(o.History()); got != 3 {
		t.Fatalf("history should be capped at 3, got %d", got)
	}
}

func TestObserverDegradesOnNilAndFailingCollectors(t *testing.T) {
	o := New(Deps{Cost: fakeCost{err: context.DeadlineExceeded}}, Config{
		Clock: fixedClock(), Logger: quietLogger(),
	})
	snap := o.Tick(context.Background())
	if snap.Cost.Events != 0 || len(snap.Quotas) != 0 {
		t.Fatalf("failing collectors must degrade to empty facts, got %+v", snap)
	}
}
