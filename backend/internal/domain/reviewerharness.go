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
	ReviewerCopilot    ReviewerHarness = "copilot"
	ReviewerCursor     ReviewerHarness = "cursor"
	ReviewerKiloCode   ReviewerHarness = "kilocode"
	ReviewerKimchi     ReviewerHarness = "kimchi"
	ReviewerOpenCode   ReviewerHarness = "opencode"
	ReviewerKiro       ReviewerHarness = "kiro"
	ReviewerPi         ReviewerHarness = "pi"
	ReviewerQwen       ReviewerHarness = "qwen"
	ReviewerAgy        ReviewerHarness = "agy"
	ReviewerContinue   ReviewerHarness = "continue"
	ReviewerGoose      ReviewerHarness = "goose"
	ReviewerVibe       ReviewerHarness = "vibe"
	ReviewerDevin      ReviewerHarness = "devin"
	ReviewerDroid      ReviewerHarness = "droid"
	ReviewerKimi       ReviewerHarness = "kimi"
	ReviewerMuse       ReviewerHarness = "muse"
	ReviewerAmp        ReviewerHarness = "amp"
	ReviewerAider      ReviewerHarness = "aider"
	ReviewerGrok       ReviewerHarness = "grok"
	ReviewerCrush      ReviewerHarness = "crush"
	ReviewerAuggie     ReviewerHarness = "auggie"
	ReviewerCline      ReviewerHarness = "cline"
	ReviewerAutohand   ReviewerHarness = "autohand"
)

// AllReviewerHarnesses is the canonical set used to validate a configured
// reviewer harness.
var AllReviewerHarnesses = []ReviewerHarness{
	ReviewerClaudeCode,
	ReviewerCodex,
	ReviewerCodexFugu,
	ReviewerCopilot,
	ReviewerCursor,
	ReviewerKiloCode,
	ReviewerKimchi,
	ReviewerOpenCode,
	ReviewerKiro,
	ReviewerPi,
	ReviewerQwen,
	ReviewerAgy,
	ReviewerContinue,
	ReviewerGoose,
	ReviewerVibe,
	ReviewerDevin,
	ReviewerDroid,
	ReviewerKimi,
	ReviewerMuse,
	ReviewerAmp,
	ReviewerAider,
	ReviewerGrok,
	ReviewerCrush,
	ReviewerAuggie,
	ReviewerCline,
	ReviewerAutohand,
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
	case ReviewerCopilot:
		return HarnessCopilot
	case ReviewerCursor:
		return HarnessCursor
	case ReviewerKiloCode:
		return HarnessKilocode
	case ReviewerKimchi:
		return HarnessKimchi
	case ReviewerKiro:
		return HarnessKiro
	case ReviewerPi:
		return HarnessPi
	case ReviewerQwen:
		return HarnessQwen
	case ReviewerAgy:
		return HarnessAgy
	case ReviewerContinue:
		return HarnessContinue
	case ReviewerGoose:
		return HarnessGoose
	case ReviewerVibe:
		return HarnessVibe
	case ReviewerDevin:
		return HarnessDevin
	case ReviewerDroid:
		return HarnessDroid
	case ReviewerKimi:
		return HarnessKimi
	case ReviewerMuse:
		return HarnessMuse
	case ReviewerAmp:
		return HarnessAmp
	case ReviewerAider:
		return HarnessAider
	case ReviewerGrok:
		return HarnessGrok
	case ReviewerCrush:
		return HarnessCrush
	case ReviewerAuggie:
		return HarnessAuggie
	case ReviewerCline:
		return HarnessCline
	case ReviewerAutohand:
		return HarnessAutohand
	default:
		return ""
	}
}
