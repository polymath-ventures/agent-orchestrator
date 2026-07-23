package daemon

import (
	"context"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/quota"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// startQuotaProber wires the daemon harness quota prober behind config. A zero
// (or negative) interval disables it entirely: the returned prober is nil (so
// the API mounts the probe endpoint as not-implemented and reports no probe
// statuses) and the done channel is already closed.
//
// The prober enumerates installed harnesses via the agent Service (dynamic
// registry, no hardcoded harness list), probes each adapter that implements the
// quota-probe capability, persists real usage snapshots to the same store the
// metrics collector reads, and holds honest per-harness status in memory.
func startQuotaProber(ctx context.Context, cfg config.Config, store *sqlite.Store, agentSvc *agentsvc.Service, logger *slog.Logger) (*quota.Prober, <-chan struct{}) {
	if cfg.Metrics.QuotaProbeInterval <= 0 {
		logger.Info("quota prober disabled (AO_QUOTA_PROBE_INTERVAL=0)")
		closed := make(chan struct{})
		close(closed)
		return nil, closed
	}

	prober := quota.New(quota.Deps{
		Enumerator: agentSvc,
		Store:      store,
		Interval:   cfg.Metrics.QuotaProbeInterval,
		Logger:     logger,
	})
	done := prober.Start(ctx)
	return prober, done
}

// quotaProberProvider maps a disabled (nil) prober to a nil controller interface
// so the endpoint reports not-implemented instead of wrapping a typed-nil
// pointer that would panic on call.
func quotaProberProvider(prober *quota.Prober) controllers.QuotaProber {
	if prober == nil {
		return nil
	}
	return prober
}
