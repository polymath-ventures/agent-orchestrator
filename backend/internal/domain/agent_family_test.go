package domain

import "testing"

func TestAgentHarnessFamily(t *testing.T) {
	tests := map[AgentHarness]AgentFamily{
		HarnessClaudeCode: AgentFamilyClaude,
		HarnessCodex:      AgentFamilyCodex,
		HarnessCodexFugu:  AgentFamilyFugu,
		HarnessOpenCode:   AgentFamilyOpenCode,
		HarnessAider:      "",
		HarnessCrush:      "",
		"":                "",
	}
	for harness, want := range tests {
		if got := harness.Family(); got != want {
			t.Fatalf("%q.Family() = %q, want %q", harness, got, want)
		}
	}
}

func TestKnownReviewFamiliesAreDistinct(t *testing.T) {
	seen := map[AgentFamily]bool{}
	for _, family := range []AgentFamily{AgentFamilyClaude, AgentFamilyCodex, AgentFamilyFugu, AgentFamilyOpenCode} {
		if family == "" {
			t.Fatal("known review family must not be empty")
		}
		if seen[family] {
			t.Fatalf("duplicate review family %q", family)
		}
		seen[family] = true
	}
}
