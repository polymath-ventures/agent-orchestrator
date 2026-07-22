package review

import (
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

// reviewTexts returns the user-facing prompt and the system prompt to deliver to
// a reviewer, authored in one place — the reviewer analogue of
// session_manager.buildSpawnTexts. The standing reviewer role lives in the
// system prompt; the per-pass task (which PR/commit, and the exact submit
// command carrying the ids) lives in the prompt, so it is also what AO injects
// into an already-running reviewer to review a new commit.
//
// The texts are self-contained — they carry the ids the reviewer needs to
// submit — so no environment variables are required.
// reviewerSystemPromptBase is the standing reviewer role prompt, before any
// operator-controlled reviewer rules are appended.
const reviewerSystemPromptBase = `## Code reviewer role

You are an AO code reviewer.

This prompt includes the standing reviewer policy when this project configures one. Treat that injected policy as the authority for review scope, severity, independence, and verdict boundaries. If no policy is configured, no role policy is appended.`

// ReviewerRules loads the operator-controlled reviewer standing instructions for
// a project (inline + repo-relative file) through the shared role-rules loader.
// It is the single entry point both the reviewer spawn path and the
// effective-prompt visibility path use, so what an operator inspects always
// matches what the reviewer is actually launched with. Fail-closed:
// misconfiguration returns a sessionmanager.RulesLoadError.
func ReviewerRules(projectID, projectPath string, cfg domain.ProjectConfig) (string, error) {
	return sessionmanager.LoadRoleRules(sessionmanager.RoleRulesConfig{
		Role:        "reviewer",
		ProjectID:   projectID,
		ProjectPath: projectPath,
		InlineRules: cfg.ReviewerRules,
		RulesFile:   cfg.ReviewerRulesFile,
	})
}

// AssembleReviewerSystemPrompt returns the reviewer role's full system prompt
// with the operator's reviewer rules appended (content-preserving). It is the reviewer
// analogue of the worker/orchestrator system prompt, exposed so the effective
// reviewer prompt can be inspected by an operator and is never a black box.
func AssembleReviewerSystemPrompt(reviewerRules string) string {
	systemPrompt := reviewerSystemPromptBase
	if rules := strings.TrimSpace(reviewerRules); rules != "" {
		systemPrompt += "\n\n## Project-Specific Reviewer Rules\n" + rules
	}
	return systemPrompt
}

func reviewTexts(spec LaunchSpec) (prompt, systemPrompt string) {
	systemPrompt = AssembleReviewerSystemPrompt(spec.ReviewerRules)

	queueText := reviewQueueText(spec)
	prompt = fmt.Sprintf(`Review the requested pull request(s) for worker session %s.
%s

Complete every review task in the queue autonomously. Do not ask the user whether to continue to the next PR, and do not stop after the first PR unless the provider or checkout is genuinely unusable for every queued task.

Do these steps in order:
1. For each PR below, post a separate review on that pull request and capture its id in one call. Post with `+"`gh api`"+` rather than `+"`gh pr review`"+`: it is the only way to attach inline comments, and its response carries the created review's id, so AO can tell the worker exactly which review to address. Send the review as a JSON body so the inline comments form a proper array of objects:

    printf '%%s' '{ "event": "COMMENT", "body": "<summary>", "comments": [ { "path": "<file>", "line": <n>, "body": "<finding>" } ] }' | gh api --method POST repos/{owner}/{repo}/pulls/{number}/reviews --input - --jq '.id'

   - Substitute the PR's owner/repo/number. Add one object to "comments" per inline finding; omit the field for a review with no inline comments.
	   - Keep the JSON on one line and shell-escape any single quotes in review text before passing it to printf; do not use a heredoc because reviewer panes run through an interactive PTY.
   - Always use "event": "COMMENT": reviews are posted from the PR author's own account, and GitHub rejects both APPROVE and REQUEST_CHANGES on your own PR. State in the body whether you are requesting changes or approving; the machine-readable verdict goes to AO in step 2.
   - The printed number is the review id. If the call fails on the provider, leave the id empty.
2. After every PR has its own GitHub review from step 1, record AO's bookkeeping for those already-posted reviews using one command. Pass JSON on stdin so nothing is ever written into the worktree (a file there could be committed onto the worker's branch). Include one object per PR/run from the queue:

    printf '%%s' '{ "reviews": [ { "runId": "<run-id>", "verdict": "<approved|changes_requested>", "githubReviewId": "<id-from-step-1-or-empty>", "body": "<your full review markdown>" } ] }' | ao review submit --session %s --reviews -

Only if step 1 genuinely fails on the provider for a PR, still include that run in step 2 with an empty githubReviewId so the result is recorded.`,
		spec.WorkerID, queueText, spec.WorkerID)
	return prompt, systemPrompt
}

func reviewQueueText(spec LaunchSpec) string {
	if len(spec.ReviewQueue) <= 1 {
		return fmt.Sprintf("\nReview task queue:\n* 1. %s (head commit %s, run %s)\n", spec.PRURL, spec.TargetSHA, spec.RunID)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nAO created %d review tasks for this worker session. Review every queued PR, then submit all results together.\n\nReview task queue:\n", len(spec.ReviewQueue))
	for i, task := range spec.ReviewQueue {
		fmt.Fprintf(&b, "* %d. %s (head commit %s, run %s)\n", i+1, task.PRURL, task.TargetSHA, task.RunID)
	}
	return b.String()
}
