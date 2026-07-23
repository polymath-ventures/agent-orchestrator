package agent

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
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
//
// Probers are keyed by their canonical QuotaHarness and deduplicated: fork
// variants that share a login and usage pool (codex-fugu → codex) collapse to a
// single entry, so the daemon probes the shared pool once and never renders a
// duplicate chip. The first installed prober for a canonical harness wins; the
// returned Harness is the canonical one, which is also what the widget labels.
func (s *Service) QuotaProbers(ctx context.Context) []ports.HarnessQuotaProber {
	out := make([]ports.HarnessQuotaProber, 0, len(s.agents))
	seen := make(map[domain.AgentHarness]struct{}, len(s.agents))
	for _, item := range s.agents {
		prober, ok := item.Agent.(ports.AgentQuotaProber)
		if !ok {
			continue
		}
		canonical := prober.QuotaHarness()
		if _, dup := seen[canonical]; dup {
			continue
		}
		if !s.binaryInstalled(ctx, item.Agent) {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, ports.HarnessQuotaProber{
			Harness: canonical,
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
