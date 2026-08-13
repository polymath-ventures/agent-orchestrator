package domain

import "testing"

// TestCodexFuguIsAKnownReviewer pins the fix for the reviewer picker omitting
// Codex Fugu: codex-fugu must be a recognized reviewer harness that maps back to
// the codex-fugu worker harness. Because the agent service derives an agent's
// reviewerCapable flag from ReviewerHarness(id).IsKnown(), this is the single
// place that makes Fugu selectable as a reviewer end to end.
func TestCodexFuguIsAKnownReviewer(t *testing.T) {
	h := ReviewerHarness("codex-fugu")
	if !h.IsKnown() {
		t.Fatalf("codex-fugu must be a known reviewer harness; AllReviewerHarnesses = %v", AllReviewerHarnesses)
	}
	if got := h.AgentHarness(); got != HarnessCodexFugu {
		t.Fatalf("ReviewerHarness(codex-fugu).AgentHarness() = %q, want %q", got, HarnessCodexFugu)
	}
}

func TestGrokIsAKnownReviewer(t *testing.T) {
	harness := ReviewerGrok
	if !harness.IsKnown() {
		t.Fatalf("grok must be a known reviewer harness; AllReviewerHarnesses = %v", AllReviewerHarnesses)
	}
	if got := harness.AgentHarness(); got != HarnessGrok {
		t.Errorf("ReviewerHarness(grok).AgentHarness() = %q, want %q", got, HarnessGrok)
	}
}
