package domain

import (
	"fmt"
	"strings"
)

// PermissionMode controls how much review an agent requires before acting. It
// lives in domain (not ports) so the typed AgentConfig can carry it; ports
// re-exports it as a type alias so agent adapters keep referring to
// ports.PermissionMode unchanged.
type PermissionMode string

// The permission modes adapters map onto their agent's native approval flags.
const (
	// PermissionModeDefault is special: adapters choose their own baseline
	// behavior for it. Most defer to the agent's own config; some managed
	// adapters may map it to a safer non-interactive default.
	PermissionModeDefault           PermissionMode = "default"
	PermissionModeAcceptEdits       PermissionMode = "accept-edits"
	PermissionModeAuto              PermissionMode = "auto"
	PermissionModeBypassPermissions PermissionMode = "bypass-permissions"
)

// Effort sets an agent's reasoning-effort level. Providers expose overlapping
// but distinct vocabularies, so the union is accepted here and each adapter maps
// or forwards the value its CLI understands.
type Effort string

const (
	// EffortMinimal requests the smallest reasoning budget a provider exposes.
	EffortMinimal Effort = "minimal"
	// EffortLow requests a low reasoning budget.
	EffortLow Effort = "low"
	// EffortMedium requests a medium reasoning budget.
	EffortMedium Effort = "medium"
	// EffortHigh requests a high reasoning budget.
	EffortHigh Effort = "high"
	// EffortXHigh requests an extra-high reasoning budget for providers that support it.
	EffortXHigh Effort = "xhigh"
	// EffortMax requests the largest reasoning budget a provider exposes.
	EffortMax Effort = "max"
)

// Valid reports whether e is empty (unset) or one of the known effort levels.
func (e Effort) Valid() bool {
	switch e {
	case "", EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	default:
		return false
	}
}

// NormalizeEffortForHarness converts compatibility aliases into the native
// vocabulary of the selected harness. Fugu documents max as an alias for
// xhigh, while ordinary Codex and other harnesses keep their configured value.
func NormalizeEffortForHarness(h AgentHarness, effort Effort) Effort {
	if h == HarnessCodexFugu && effort == EffortMax {
		return EffortXHigh
	}
	return effort
}

// DefaultClaudeCodeModel is AO's cheap default for unpinned claude-code
// launches. Explicit model choices are honored; this only fills an otherwise
// empty resolution result.
const DefaultClaudeCodeModel = "opus"

// DefaultModelForHarness returns the model AO substitutes when a spawn of the
// given harness resolves to no explicit model.
func DefaultModelForHarness(h AgentHarness) string {
	if h == HarnessClaudeCode {
		return DefaultClaudeCodeModel
	}
	return ""
}

// HarnessModel is the model + reasoning effort AO applies when a spawn resolves
// to a specific harness.
type HarnessModel struct {
	Model  string `json:"model,omitempty"`
	Effort Effort `json:"effort,omitempty"`
}

// AgentConfig is the typed per-project agent configuration. It replaces the
// former free-form map so the fields are validated and the API/UI render a
// real form rather than arbitrary JSON. An empty value (IsZero) means unset.
type AgentConfig struct {
	// Model overrides the agent's default model (e.g. claude-opus-4-5).
	Model string `json:"model,omitempty"`
	// Effort is the default reasoning-effort level.
	Effort Effort `json:"effort,omitempty"`
	// Mode selects an agent-owned operating mode when the adapter exposes modes
	// instead of raw model ids (currently Amp: low|medium|high|ultra).
	Mode string `json:"mode,omitempty"`
	// Permissions sets the agent's starting permission mode. Empty is treated
	// like the adapter's default mode.
	Permissions PermissionMode `json:"permissions,omitempty"`
	// ModelByHarness pins the model and effort per resolved harness.
	ModelByHarness map[AgentHarness]HarnessModel `json:"modelByHarness,omitempty"`
}

// IsZero reports whether the config carries no settings, so storage can persist
// SQL NULL and resolution can skip an empty config.
func (c AgentConfig) IsZero() bool {
	return c.Model == "" && c.Effort == "" && c.Mode == "" && c.Permissions == "" && len(c.ModelByHarness) == 0
}

// Valid reports whether the mode is one AO knows. Empty counts as valid: it means
// "the adapter's own baseline", which is a legitimate choice rather than a missing
// one.
func (m PermissionMode) Valid() bool {
	switch m {
	case "", PermissionModeDefault, PermissionModeAcceptEdits,
		PermissionModeAuto, PermissionModeBypassPermissions:
		return true
	default:
		return false
	}
}

// Validate rejects values outside the typed vocabulary so a bad config is
// refused when it is set (CLI/API) rather than silently dropped at spawn.
func (c AgentConfig) Validate() error {
	if !c.Permissions.Valid() {
		return fmt.Errorf("invalid permissions %q: want one of default, accept-edits, auto, bypass-permissions", c.Permissions)
	}
	switch c.Mode {
	case "", "low", "medium", "high", "ultra":
	default:
		return fmt.Errorf("invalid mode %q: want one of low, medium, high, ultra", c.Mode)
	}
	if !c.Effort.Valid() {
		return fmt.Errorf("invalid effort %q: want one of minimal, low, medium, high, xhigh, max", c.Effort)
	}
	if c.Model != "" && c.Model != strings.TrimSpace(c.Model) {
		return fmt.Errorf("model: must not have leading or trailing whitespace")
	}
	for harness, hm := range c.ModelByHarness {
		if !harness.IsKnown() {
			return fmt.Errorf("modelByHarness: unknown harness %q", harness)
		}
		if hm.Model != "" && hm.Model != strings.TrimSpace(hm.Model) {
			return fmt.Errorf("modelByHarness[%s].model: must not have leading or trailing whitespace", harness)
		}
		if !hm.Effort.Valid() {
			return fmt.Errorf("modelByHarness[%s].effort: invalid effort %q", harness, hm.Effort)
		}
		if hp := harness.ModelProvider(); !ClassifyModelProvider(hm.Model).CompatibleWith(hp) {
			return fmt.Errorf("modelByHarness[%s].model: %q is not a %s model", harness, hm.Model, hp)
		}
	}
	return nil
}
