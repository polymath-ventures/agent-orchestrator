package domain

// ReviewerHarness identifies a code-review agent. It is a separate vocabulary
// from AgentHarness on purpose: a reviewer-only tool (e.g. the Greptile CLI)
// must not become a valid worker, and a worker harness does not automatically
// become a valid reviewer. The two sets are maintained independently and only
// happen to share ids where the same tool serves both roles.
type ReviewerHarness string

// Supported reviewer harnesses. Add a reviewer-only tool here (and register its
// adapter) without widening the worker AgentHarness set.
const (
	ReviewerClaudeCode ReviewerHarness = "claude-code"
	ReviewerCodex      ReviewerHarness = "codex"
	ReviewerCodexFugu  ReviewerHarness = "codex-fugu"
	ReviewerOpenCode   ReviewerHarness = "opencode"
)

// AllReviewerHarnesses is the canonical set used to validate a configured
// reviewer harness.
var AllReviewerHarnesses = []ReviewerHarness{
	ReviewerClaudeCode,
	ReviewerCodex,
	ReviewerCodexFugu,
	ReviewerOpenCode,
}

// IsKnown reports whether h is one of the supported reviewer harnesses.
func (h ReviewerHarness) IsKnown() bool {
	for _, k := range AllReviewerHarnesses {
		if h == k {
			return true
		}
	}
	return false
}

// AgentHarness returns the worker-agent harness that backs this reviewer when
// the same CLI can launch both workers and reviewers.
func (h ReviewerHarness) AgentHarness() AgentHarness {
	switch h {
	case ReviewerClaudeCode:
		return HarnessClaudeCode
	case ReviewerCodex:
		return HarnessCodex
	case ReviewerCodexFugu:
		return HarnessCodexFugu
	case ReviewerOpenCode:
		return HarnessOpenCode
	default:
		return ""
	}
}
