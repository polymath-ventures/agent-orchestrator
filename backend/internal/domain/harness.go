package domain

// AgentHarness identifies which agent CLI/runtime a session drives.
type AgentHarness string

// Supported agent harnesses.
const (
	HarnessClaudeCode AgentHarness = "claude-code"
	HarnessCodex      AgentHarness = "codex"
	// Fork-only: a Polymath-internal Codex wrapper; never ships upstream.
	HarnessCodexFugu AgentHarness = "codex-fugu"
	HarnessAider     AgentHarness = "aider"
	HarnessOpenCode  AgentHarness = "opencode"
	HarnessGrok      AgentHarness = "grok"
	HarnessDroid     AgentHarness = "droid"
	HarnessAmp       AgentHarness = "amp"
	HarnessAgy       AgentHarness = "agy"
	HarnessCrush     AgentHarness = "crush"
	HarnessCursor    AgentHarness = "cursor"
	HarnessQwen      AgentHarness = "qwen"
	HarnessCopilot   AgentHarness = "copilot"
	HarnessGoose     AgentHarness = "goose"
	HarnessAuggie    AgentHarness = "auggie"
	HarnessContinue  AgentHarness = "continue"
	HarnessDevin     AgentHarness = "devin"
	HarnessCline     AgentHarness = "cline"
	HarnessKimi      AgentHarness = "kimi"
	HarnessMuse      AgentHarness = "muse"
	HarnessKiro      AgentHarness = "kiro"
	HarnessKilocode  AgentHarness = "kilocode"
	HarnessVibe      AgentHarness = "vibe"
	HarnessPi        AgentHarness = "pi"
	HarnessAutohand  AgentHarness = "autohand"
	// HarnessFake is a deterministic, LLM-free harness used by e2e tests and is
	// retained for existing fixtures and historical session rows.
	HarnessFake AgentHarness = "fake"
)

// AllHarnesses lists every supported harness. It is the canonical set used to
// validate user-supplied harness names (e.g. per-project role overrides).
var AllHarnesses = []AgentHarness{
	HarnessClaudeCode, HarnessCodex, HarnessCodexFugu, HarnessAider, HarnessOpenCode, HarnessGrok,
	HarnessDroid, HarnessAmp, HarnessAgy, HarnessCrush, HarnessCursor, HarnessQwen,
	HarnessCopilot, HarnessGoose, HarnessAuggie, HarnessContinue, HarnessDevin,
	HarnessCline, HarnessKimi, HarnessMuse, HarnessKiro, HarnessKilocode, HarnessVibe, HarnessPi,
	HarnessAutohand, HarnessFake,
}

// IsKnown reports whether h is one of the supported harnesses.
func (h AgentHarness) IsKnown() bool {
	for _, k := range AllHarnesses {
		if h == k {
			return true
		}
	}
	return false
}

// ModelProvider maps a harness to the vendor family whose models it accepts.
// Only harnesses whose model namespace AO knows are classified; every other
// harness returns ProviderUnknown and is left unguarded, so its configured
// model is passed through untouched. This is what lets model resolution reject
// a cross-provider model (e.g. a Claude model on a Codex harness) without
// constraining the many harnesses AO has not mapped.
func (h AgentHarness) ModelProvider() ModelProvider {
	switch h {
	case HarnessClaudeCode:
		return ProviderAnthropic
	case HarnessCodex:
		return ProviderOpenAI
	case HarnessCodexFugu:
		return ProviderFugu
	default:
		return ProviderUnknown
	}
}

// RequiresLaunchProcessLivenessSweep reports whether a live runtime pane is
// insufficient proof that the launched agent is still alive. These harnesses
// run under a keep-alive shell, so the pane can survive after the CLI exits.
func (h AgentHarness) RequiresLaunchProcessLivenessSweep() bool {
	switch h {
	case HarnessClaudeCode, HarnessCodex, HarnessCodexFugu:
		return true
	default:
		return false
	}
}
