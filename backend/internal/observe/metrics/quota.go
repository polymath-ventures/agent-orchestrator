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

// StoreQuotaCollector surfaces the latest persisted quota snapshots for the
// metrics tick (alert evaluation + history). Quota data is written by the
// daemon QuotaProber (source of record) and the Stop-hook freshness path; the
// collector never fabricates rows. Legacy "no signal" placeholder rows from the
// pre-probe implementation are filtered out so they can never render.
type StoreQuotaCollector struct {
	store quotaStore
}

// NewStoreQuotaCollector constructs a quota collector over durable storage.
func NewStoreQuotaCollector(store quotaStore) *StoreQuotaCollector {
	return &StoreQuotaCollector{store: store}
}

// CollectQuota returns the latest persisted quota snapshots, dropping any legacy
// no-signal placeholder rows. It is a pure read: honest states for un-probed or
// failed harnesses are carried by the QuotaProber's in-memory status, not by
// fabricated store rows.
func (c *StoreQuotaCollector) CollectQuota(ctx context.Context, _ time.Time) ([]domain.QuotaSnapshot, error) {
	if c == nil || c.store == nil {
		return nil, nil
	}
	latest, err := c.store.ListLatestQuotaSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	out := latest[:0]
	for _, snap := range latest {
		if snap.SignalQuality == domain.QuotaSignalNone {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}
