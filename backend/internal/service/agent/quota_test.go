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
	result ports.QuotaProbeResult
}

func (q quotaProbeAgent) ProbeQuota(_ context.Context, _ time.Time) (ports.QuotaProbeResult, error) {
	return q.result, nil
}

func TestQuotaProbersIncludesInstalledProbers(t *testing.T) {
	installed := quotaProbeAgent{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}}
	// A prober whose binary does not resolve must be excluded.
	uninstalled := quotaProbeAgent{fakeAgent: fakeAgent{err: errors.New("not on PATH")}}
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
