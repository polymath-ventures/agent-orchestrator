package metrics

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const harnessCodexFugu domain.AgentHarness = "codex-fugu"

type quotaStore interface {
	UpsertQuotaSnapshot(ctx context.Context, snap domain.QuotaSnapshot) (domain.QuotaSnapshot, error)
	ListLatestQuotaSnapshots(ctx context.Context) ([]domain.QuotaSnapshot, error)
}

// StoreQuotaCollector records no-signal snapshots for known subscription
// harnesses until an adapter exposes a stable exact or estimated quota source.
type StoreQuotaCollector struct {
	store     quotaStore
	harnesses []domain.AgentHarness
}

// NewStoreQuotaCollector constructs a quota collector over durable storage.
func NewStoreQuotaCollector(store quotaStore) *StoreQuotaCollector {
	return &StoreQuotaCollector{
		store: store,
		harnesses: []domain.AgentHarness{
			domain.HarnessClaudeCode,
			domain.HarnessCodex,
			harnessCodexFugu,
		},
	}
}

// CollectQuota refreshes best-effort quota snapshots and returns the latest
// known state. Today Claude Code and Codex expose user-facing quota warnings,
// but no stable public local/API quota contract for AO to consume, so the
// honest snapshot is no signal.
func (c *StoreQuotaCollector) CollectQuota(ctx context.Context, observedAt time.Time) ([]domain.QuotaSnapshot, error) {
	if c == nil || c.store == nil {
		return nil, nil
	}
	for _, harness := range c.harnesses {
		_, err := c.store.UpsertQuotaSnapshot(ctx, domain.QuotaSnapshot{
			Harness:       harness,
			AccountID:     "unknown",
			SignalQuality: domain.QuotaSignalNone,
			Source:        "official docs and local inspection",
			Basis:         "No stable public quota endpoint, response header, CLI output, or local state file is available to AO; user-facing limit warnings exist but are not machine-readable.",
			ObservedAt:    observedAt,
		})
		if err != nil {
			return nil, err
		}
	}
	return c.store.ListLatestQuotaSnapshots(ctx)
}
