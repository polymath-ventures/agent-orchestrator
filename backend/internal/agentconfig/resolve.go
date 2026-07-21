// Package agentconfig resolves project agent settings for launches.
package agentconfig

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ErrModelHarnessMismatch means an explicit per-spawn model belongs to a
// different provider than the resolved harness.
var ErrModelHarnessMismatch = errors.New("session: model not valid for harness")

// Effective resolves the agent config for a spawn of the given kind and harness.
func Effective(kind domain.SessionKind, cfg domain.ProjectConfig, spawnModel string, harness domain.AgentHarness) (ports.AgentConfig, error) {
	return EffectiveFromConfigs(cfg.AgentConfig, roleOverride(kind, cfg).AgentConfig, spawnModel, harness)
}

// EffectiveFromConfigs resolves a base + override pair for a concrete harness.
func EffectiveFromConfigs(base, override domain.AgentConfig, spawnModel string, harness domain.AgentHarness) (ports.AgentConfig, error) {
	hp := harness.ModelProvider()

	resolved := ports.AgentConfig{Permissions: base.Permissions}
	if override.Permissions != "" {
		resolved.Permissions = override.Permissions
	}

	var model string
	var effort domain.Effort
	if m := strings.TrimSpace(base.Model); m != "" && domain.ClassifyModelProvider(m).CompatibleWith(hp) {
		model = m
	}
	if base.Effort != "" {
		effort = base.Effort
	}
	if m := strings.TrimSpace(override.Model); m != "" && domain.ClassifyModelProvider(m).CompatibleWith(hp) {
		model = m
	}
	if override.Effort != "" {
		effort = override.Effort
	}

	applyHarnessModel := func(hm domain.HarnessModel) {
		if m := strings.TrimSpace(hm.Model); m != "" && domain.ClassifyModelProvider(m).CompatibleWith(hp) {
			model = m
		}
		if hm.Effort != "" {
			effort = hm.Effort
		}
	}
	if hm, ok := base.ModelByHarness[harness]; ok {
		applyHarnessModel(hm)
	}
	if hm, ok := override.ModelByHarness[harness]; ok {
		applyHarnessModel(hm)
	}

	if sm := strings.TrimSpace(spawnModel); sm != "" {
		if !domain.ClassifyModelProvider(sm).CompatibleWith(hp) {
			return ports.AgentConfig{}, fmt.Errorf("%w: %q is not a %s model (harness %q)", ErrModelHarnessMismatch, sm, hp, harness)
		}
		model = sm
	}
	if model == "" {
		model = domain.DefaultModelForHarness(harness)
	}

	resolved.Model = model
	resolved.Effort = effort
	return resolved, nil
}

func roleOverride(kind domain.SessionKind, cfg domain.ProjectConfig) domain.RoleOverride {
	switch kind {
	case domain.KindOrchestrator:
		return cfg.Orchestrator
	case domain.KindPrime:
		return cfg.Prime
	default:
		return cfg.Worker
	}
}
