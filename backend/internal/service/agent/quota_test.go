package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	agentregistry "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// quotaProbeAgent implements ports.Agent (via fakeAgent) plus AgentQuotaProber.
// fakeAgent already implements ResolveBinary, so an installed prober resolves.
type quotaProbeAgent struct {
	fakeAgent
	result       ports.QuotaProbeResult
	quotaHarness domain.AgentHarness
}

func (q quotaProbeAgent) ProbeQuota(_ context.Context, _ time.Time) (ports.QuotaProbeResult, error) {
	return q.result, nil
}

func (q quotaProbeAgent) QuotaHarness() domain.AgentHarness { return q.quotaHarness }

func TestQuotaProbersIncludesInstalledProbers(t *testing.T) {
	installed := quotaProbeAgent{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}, quotaHarness: domain.HarnessClaudeCode}
	// A prober whose binary does not resolve must be excluded.
	uninstalled := quotaProbeAgent{fakeAgent: fakeAgent{err: errors.New("not on PATH")}, quotaHarness: domain.HarnessCodex}
	// A non-prober agent must be excluded even though it is installed.
	nonProber := fakeAgent{}

	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessClaudeCode, "Claude Code", installed),
		harnessCatalogAgent(domain.HarnessCodex, "Codex", uninstalled),
		harnessCatalogAgent(domain.HarnessOpenCode, "OpenCode", nonProber),
	})

	probers := svc.QuotaProbers(context.Background())
	if len(probers) != 1 {
		t.Fatalf("QuotaProbers() returned %d probers, want 1: %#v", len(probers), probers)
	}
	if probers[0].Harness != domain.HarnessClaudeCode {
		t.Fatalf("QuotaProbers()[0].Harness = %q, want %q", probers[0].Harness, domain.HarnessClaudeCode)
	}
	if probers[0].Prober == nil {
		t.Fatal("QuotaProbers()[0].Prober is nil")
	}
}

// TestQuotaProbersDedupesSharedPool covers the GH #97 codex/codex-fugu case:
// two installed probers that report the same canonical QuotaHarness (codex)
// must collapse to a single entry keyed by that canonical harness, so the daemon
// probes the shared pool once and the widget never renders a duplicate chip.
func TestQuotaProbersDedupesSharedPool(t *testing.T) {
	codex := quotaProbeAgent{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}, quotaHarness: domain.HarnessCodex}
	fugu := quotaProbeAgent{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}, quotaHarness: domain.HarnessCodex}

	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessCodex, "Codex", codex),
		harnessCatalogAgent(domain.HarnessCodexFugu, "Codex Fugu", fugu),
	})

	probers := svc.QuotaProbers(context.Background())
	if len(probers) != 1 {
		t.Fatalf("QuotaProbers() returned %d probers, want 1 (codex+fugu collapse): %#v", len(probers), probers)
	}
	if probers[0].Harness != domain.HarnessCodex {
		t.Fatalf("collapsed prober Harness = %q, want %q", probers[0].Harness, domain.HarnessCodex)
	}
}
