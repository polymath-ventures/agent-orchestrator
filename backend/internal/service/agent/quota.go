package agent

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// QuotaProbers returns the installed harnesses whose adapter implements
// ports.AgentQuotaProber, so the daemon quota prober iterates the dynamic agent
// registry rather than a hardcoded harness list. It capability-casts each
// adapter and, for the ones that can probe, runs the same bounded binary-resolve
// check probeAgent uses to decide "installed" — it does NOT run the auth probe,
// because quota probing only needs the CLI to be present. An adapter that is a
// prober but whose binary does not resolve is excluded; a non-prober adapter is
// excluded regardless of install state.
func (s *Service) QuotaProbers(ctx context.Context) []ports.HarnessQuotaProber {
	out := make([]ports.HarnessQuotaProber, 0, len(s.agents))
	for _, item := range s.agents {
		prober, ok := item.Agent.(ports.AgentQuotaProber)
		if !ok {
			continue
		}
		if !s.binaryInstalled(ctx, item.Agent) {
			continue
		}
		out = append(out, ports.HarnessQuotaProber{
			Harness: item.Harness,
			Prober:  prober,
		})
	}
	return out
}

// binaryInstalled runs the same bounded resolve probe probeAgent uses to gate
// "installed", without the auth probe. An adapter that does not expose a binary
// resolver capability cannot be confirmed installed and is treated as absent.
func (s *Service) binaryInstalled(ctx context.Context, a ports.Agent) bool {
	resolver, ok := a.(ports.AgentBinaryResolver)
	if !ok {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, agentInstallProbeTimeout)
	defer cancel()
	_, err := resolver.ResolveBinary(probeCtx)
	return err == nil
}
