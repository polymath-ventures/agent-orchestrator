package domain

// AgentFamily is the review-independence family of a worker harness. Two
// harnesses in the same family are not independent reviewers of each other.
// The empty family means the harness is unclassified and cannot anchor a hard
// independence decision.
type AgentFamily string

const (
	// AgentFamilyClaude is the Claude/Anthropic review family.
	AgentFamilyClaude AgentFamily = "claude"
	// AgentFamilyCodex is the Codex/OpenAI review family.
	AgentFamilyCodex AgentFamily = "codex"
	// AgentFamilyFugu is the Codex-Fugu review family, distinct for review independence.
	AgentFamilyFugu AgentFamily = "fugu"
	// AgentFamilyOpenCode is the OpenCode review family.
	AgentFamilyOpenCode AgentFamily = "opencode"
)

// Family maps a worker harness to the family that authors its changes.
func (h AgentHarness) Family() AgentFamily {
	switch h {
	case HarnessClaudeCode:
		return AgentFamilyClaude
	case HarnessCodex:
		return AgentFamilyCodex
	case HarnessCodexFugu:
		return AgentFamilyFugu
	case HarnessOpenCode:
		return AgentFamilyOpenCode
	default:
		return ""
	}
}
