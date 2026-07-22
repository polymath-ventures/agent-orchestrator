// Package agentconfig resolves project agent settings for launches.
package agentconfig

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ErrModelHarnessMismatch means an explicit per-spawn model belongs to a
// different provider than the resolved harness.
var ErrModelHarnessMismatch = errors.New("session: model not valid for harness")

// ModelPin is one exact harness/model candidate implied by project config.
type ModelPin struct {
	Scope   string
	Harness domain.AgentHarness
	Model   string
}

// Effective resolves the agent config for a spawn of the given kind and harness.
func Effective(kind domain.SessionKind, cfg domain.ProjectConfig, spawnModel string, harness domain.AgentHarness) (ports.AgentConfig, error) {
	return EffectiveFromConfigs(cfg.AgentConfig, RoleOverride(kind, cfg).AgentConfig, spawnModel, harness)
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
	resolved.Effort = domain.NormalizeEffortForHarness(harness, effort)
	return resolved, nil
}

// EffectiveHarness applies role-level harness defaults after explicit selection.
func EffectiveHarness(explicit domain.AgentHarness, kind domain.SessionKind, cfg domain.ProjectConfig) domain.AgentHarness {
	if explicit != "" {
		return explicit
	}
	return RoleOverride(kind, cfg).Harness
}

// RoleOverride returns the project override for a session role.
func RoleOverride(kind domain.SessionKind, cfg domain.ProjectConfig) domain.RoleOverride {
	switch kind {
	case domain.KindOrchestrator:
		return cfg.Orchestrator
	case domain.KindPrime:
		return cfg.Prime
	default:
		return cfg.Worker
	}
}

// ConfiguredModelPins projects exact model candidates through the same
// precedence rules used at launch. The scope labels remain descriptive while
// duplicate harness/model pairs collapse to one probe target.
func ConfiguredModelPins(cfg domain.ProjectConfig) []ModelPin {
	var pins []ModelPin
	addResolved := func(scope string, kind domain.SessionKind, harness domain.AgentHarness, spawnModel string) {
		harness = EffectiveHarness(harness, kind, cfg)
		if harness == "" {
			return
		}
		resolved, err := Effective(kind, cfg, spawnModel, harness)
		if err != nil || strings.TrimSpace(resolved.Model) == "" {
			return
		}
		pins = append(pins, ModelPin{Scope: scope, Harness: harness, Model: strings.TrimSpace(resolved.Model)})
	}
	addResolvedConfig := func(scope string, harness domain.AgentHarness, override domain.AgentConfig) {
		if harness == "" {
			return
		}
		resolved, err := EffectiveFromConfigs(cfg.AgentConfig, override, "", harness)
		if err != nil || strings.TrimSpace(resolved.Model) == "" {
			return
		}
		pins = append(pins, ModelPin{Scope: scope, Harness: harness, Model: strings.TrimSpace(resolved.Model)})
	}

	addResolved("worker", domain.KindWorker, "", "")
	addResolved("orchestrator", domain.KindOrchestrator, "", "")
	addResolved("prime", domain.KindPrime, "", "")
	for i, reviewer := range cfg.Reviewers {
		addResolvedConfig("reviewers["+strconv.Itoa(i)+"]", reviewer.Harness.AgentHarness(), reviewer.AgentConfig)
	}
	addHarnessMaps := func(scope string, kind domain.SessionKind, models map[domain.AgentHarness]domain.HarnessModel) {
		harnesses := make([]domain.AgentHarness, 0, len(models))
		for harness := range models {
			harnesses = append(harnesses, harness)
		}
		sort.Slice(harnesses, func(i, j int) bool { return harnesses[i] < harnesses[j] })
		for _, harness := range harnesses {
			addResolved(scope+"["+string(harness)+"]", kind, harness, "")
		}
	}
	addHarnessMaps("agentConfig.modelByHarness", domain.KindWorker, cfg.AgentConfig.ModelByHarness)
	addHarnessMaps("worker.agentConfig.modelByHarness", domain.KindWorker, cfg.Worker.AgentConfig.ModelByHarness)
	addHarnessMaps("orchestrator.agentConfig.modelByHarness", domain.KindOrchestrator, cfg.Orchestrator.AgentConfig.ModelByHarness)
	addHarnessMaps("prime.agentConfig.modelByHarness", domain.KindPrime, cfg.Prime.AgentConfig.ModelByHarness)
	for i, bucket := range cfg.WorkerMix {
		addResolved("workerMix["+strconv.Itoa(i)+"]", domain.KindWorker, bucket.Harness, bucket.Model)
	}
	return dedupeModelPins(pins)
}

func dedupeModelPins(in []ModelPin) []ModelPin {
	seen := make(map[string]struct{}, len(in))
	out := make([]ModelPin, 0, len(in))
	for _, pin := range in {
		key := string(pin.Harness) + "\x00" + strings.TrimSpace(pin.Model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pin)
	}
	return out
}
