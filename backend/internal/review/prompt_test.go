package review

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestReviewTextsInjectsReviewerRules(t *testing.T) {
	spec := launchSpec()
	spec.ReviewerRules = "Pay special attention to tenant isolation."
	_, systemPrompt := reviewTexts(spec)
	for _, want := range []string{
		"## Code reviewer role",
		"## Project-Specific Reviewer Rules",
		"Pay special attention to tenant isolation.",
	} {
		if !strings.Contains(systemPrompt, want) {
			t.Fatalf("reviewer system prompt missing %q:\n%s", want, systemPrompt)
		}
	}
}

func TestReviewTextsOmitsReviewerRulesHeadingWhenUnset(t *testing.T) {
	_, systemPrompt := reviewTexts(launchSpec())
	if strings.Contains(systemPrompt, "## Project-Specific Reviewer Rules") {
		t.Fatalf("unexpected reviewer rules heading with no rules:\n%s", systemPrompt)
	}
}

func TestReviewTextsIncludesMultiPRQueue(t *testing.T) {
	spec := launchSpec()
	spec.RunID = "run-2"
	spec.PRURL = "https://github.com/o/r/pull/2"
	spec.TargetSHA = "sha2"
	spec.ReviewIndex = 1
	spec.ReviewQueue = []ports.ReviewTask{
		{RunID: "run-1", PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1"},
		{RunID: "run-2", PRURL: "https://github.com/o/r/pull/2", TargetSHA: "sha2"},
	}

	prompt, _ := reviewTexts(spec)
	for _, want := range []string{
		"AO created 2 review tasks",
		"Review every queued PR, then submit all results together",
		"Complete every review task in the queue autonomously",
		"Do not ask the user whether to continue to the next PR",
		"* 1. https://github.com/o/r/pull/1 (head commit sha1, run run-1)",
		"* 2. https://github.com/o/r/pull/2 (head commit sha2, run run-2)",
		"After every PR has its own GitHub review from step 1",
		"printf '%s'",
		"do not use a heredoc",
		"ao review submit --session mer-1 --reviews -",
		`"reviews": [`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
