package metrics

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

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
	latest, err := c.store.ListLatestQuotaSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	exactOrEstimated := make(map[domain.AgentHarness]bool, len(latest))
	for _, snap := range latest {
		if snap.SignalQuality == domain.QuotaSignalExact || snap.SignalQuality == domain.QuotaSignalEstimated {
			exactOrEstimated[snap.Harness] = true
		}
	}
	seen := make(map[domain.AgentHarness]bool, len(latest))
	for _, snap := range latest {
		if snap.SignalQuality == domain.QuotaSignalNone && snap.AccountID == "unknown" && snap.Model == "" {
			seen[snap.Harness] = true
		}
	}
	for _, harness := range c.harnesses {
		if exactOrEstimated[harness] {
			continue
		}
		if seen[harness] {
			continue
		}
		_, err := c.store.UpsertQuotaSnapshot(ctx, domain.QuotaSnapshot{
			Harness:       harness,
			AccountID:     "unknown",
			SignalQuality: domain.QuotaSignalNone,
			Source:        noSignalSource(harness),
			Basis:         noSignalBasis(harness),
			ObservedAt:    observedAt,
		})
		if err != nil {
			return nil, err
		}
	}
	latest, err = c.store.ListLatestQuotaSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	out := latest[:0]
	for _, snap := range latest {
		if snap.SignalQuality == domain.QuotaSignalNone && exactOrEstimated[snap.Harness] {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

func noSignalSource(harness domain.AgentHarness) string {
	switch harness {
	case domain.HarnessClaudeCode:
		return "Claude Code local probe evidence"
	case domain.HarnessCodex:
		return "Codex rollout probe evidence"
	default:
		return "local probe evidence"
	}
}

func noSignalBasis(harness domain.AgentHarness) string {
	switch harness {
	case domain.HarnessClaudeCode:
		return "Claude Code transcripts expose per-message token usage only; local CLI help exposes no quota/status command; checked local Claude config/data paths on this host and found no stable machine-readable quota snapshot. Authenticated /usage remains a future integration path."
	case domain.HarnessCodex:
		return "No matching Codex rollout token_count.rate_limits snapshot has been observed yet; AO will replace this no-signal row when rollout rate limits appear."
	default:
		return "No stable machine-readable quota snapshot source has been observed for this harness."
	}
}
