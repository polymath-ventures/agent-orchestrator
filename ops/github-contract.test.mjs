import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { evaluatePullRequest } = require("../.github/scripts/one-closing-issue.cjs");
const { evaluateFinalReview, isCurrentDefaultBranchPullRequest } = require("../.github/scripts/valid-final-review.cjs");
import { buildStatusDescription } from "../ops/final-review-status-core.mjs";

const head = "0123456789abcdef0123456789abcdef01234567";

test("one-closing-issue accepts exactly one GitHub closing reference", () => {
	for (const body of [
		"Closes #123",
		"Fixed #123",
		"Resolves polymath-ventures/agent-orchestrator#123",
		"closed https://github.com/polymath-ventures/agent-orchestrator/issues/123",
	]) {
		assert.equal(evaluatePullRequest({ body, closingIssueCount: 1 }).ok, true, body);
	}
});

test("one-closing-issue rejects unavailable, missing, and multiple closing issues", () => {
	assert.deepEqual(evaluatePullRequest({ body: "Closes #1", closingIssueCount: undefined }), {
		ok: false,
		description: "GitHub closing-issue count is unavailable",
	});
	assert.equal(evaluatePullRequest({ body: "", closingIssueCount: 0 }).ok, false);
	assert.equal(evaluatePullRequest({ body: "Closes #1\nCloses #2", closingIssueCount: 2 }).ok, false);
});

test("one-closing-issue allows only the no-close label as the standalone escape", () => {
	assert.deepEqual(evaluatePullRequest({ labels: ["no-close"], closingIssueCount: 0 }), {
		ok: true,
		description: "standalone PR accepted by no-close label",
	});
	assert.equal(evaluatePullRequest({ body: "Intentionally-no-close: policy update", closingIssueCount: 0 }).ok, false);
});

test("valid-final-review accepts only a current clean status", () => {
	assert.deepEqual(
		evaluateFinalReview(
			[
				{
					context: "final-review",
					state: "success",
					description: buildStatusDescription({ sha: head, verdict: "clean", reviewerFamily: "claude" }),
				},
			],
			head,
		),
		{ ok: true, description: "clean final-review verified for current head" },
	);
	assert.equal(evaluateFinalReview([], head).ok, false);
	assert.equal(
		evaluateFinalReview(
			[{ context: "final-review", state: "failure", description: `verdict=clean reviewer_family=claude head=${head}` }],
			head,
		).ok,
		false,
	);
	assert.equal(
		evaluateFinalReview([{ context: "final-review", state: "success", description: "clean" }], head).ok,
		false,
	);
	assert.equal(
		evaluateFinalReview(
			[
				{
					context: "final-review",
					state: "success",
					description: "verdict=clean reviewer_family=claude head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				},
			],
			head,
		).ok,
		false,
	);
});

test("valid-final-review scopes statuses to open pull requests targeting the default branch", () => {
	assert.equal(
		isCurrentDefaultBranchPullRequest([{ state: "open", head: { sha: head }, base: { ref: "main" } }], head, "main"),
		true,
	);
	assert.equal(
		isCurrentDefaultBranchPullRequest([{ state: "closed", head: { sha: head }, base: { ref: "main" } }], head, "main"),
		false,
	);
	assert.equal(
		isCurrentDefaultBranchPullRequest(
			[{ state: "open", head: { sha: "b".repeat(40) }, base: { ref: "main" } }],
			head,
			"main",
		),
		false,
	);
	assert.equal(
		isCurrentDefaultBranchPullRequest([{ state: "open", head: { sha: head }, base: { ref: "develop" } }], head, "main"),
		false,
	);
});
