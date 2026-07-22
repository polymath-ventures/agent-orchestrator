import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, it } from "node:test";

import {
	assertCleanReviewIndependence,
	buildHumanMergeRequiredStatusPayload,
	buildReviewPassedStatusPayload,
	buildStatusPayload,
	evaluateAutonomousMergeStatuses,
	evaluateFinalReviewStatuses,
	normalizeAuthorFamilies,
	normalizeRepoSlug,
	parseStatusDescription,
} from "./final-review-status-core.mjs";

const HEAD = "0123456789abcdef0123456789abcdef01234567";
const OTHER = "fedcba9876543210fedcba9876543210fedcba98";
const CLI = new URL("./final-review-status.mjs", import.meta.url);

describe("reviewer independence at set time", () => {
	it("normalizes and de-dupes author families case-insensitively", () => {
		assert.deepEqual(normalizeAuthorFamilies(["Claude", " codex ", "claude", ""]), ["claude", "codex"]);
		assert.deepEqual(normalizeAuthorFamilies("Codex"), ["codex"]);
		assert.deepEqual(normalizeAuthorFamilies(undefined), []);
	});

	it("accepts a clean review whose reviewer family differs from every author family", () => {
		assert.deepEqual(
			assertCleanReviewIndependence({ verdict: "clean", reviewerFamily: "codex", authorFamilies: ["claude"] }),
			["claude"],
		);
		// Multi-author (e.g. a cross-family handoff): reviewer must differ from ALL.
		assert.deepEqual(
			assertCleanReviewIndependence({
				verdict: "clean",
				reviewerFamily: "fugu",
				authorFamilies: ["claude", "codex"],
			}),
			["claude", "codex"],
		);
	});

	it("refuses a clean review whose reviewer family also authored the code (case-insensitive)", () => {
		assert.throws(
			() => assertCleanReviewIndependence({ verdict: "clean", reviewerFamily: "Codex", authorFamilies: ["codex"] }),
			/non-independent review/,
		);
		assert.throws(
			() =>
				assertCleanReviewIndependence({
					verdict: "clean",
					reviewerFamily: "claude",
					authorFamilies: ["codex", "claude"],
				}),
			/non-independent review/,
		);
	});

	it("refuses a clean review with no author-family provenance (fail-closed)", () => {
		assert.throws(
			() => assertCleanReviewIndependence({ verdict: "clean", reviewerFamily: "codex", authorFamilies: [] }),
			/requires at least one --author-family/,
		);
		assert.throws(
			() => assertCleanReviewIndependence({ verdict: "clean", reviewerFamily: "codex" }),
			/requires at least one --author-family/,
		);
	});

	it("does not gate a parked verdict on author families", () => {
		assert.deepEqual(assertCleanReviewIndependence({ verdict: "parked", reviewerFamily: "codex" }), []);
	});
});

describe("final-review status payload", () => {
	it("emits a success commit status for a clean verdict on the reviewed head", () => {
		assert.deepEqual(
			buildStatusPayload({
				sha: HEAD,
				verdict: "clean",
				reviewerFamily: "codex",
				targetUrl: "https://github.example/pr/1",
			}),
			{
				context: "final-review",
				description: `verdict=clean reviewer_family=codex head=${HEAD}`,
				state: "success",
				target_url: "https://github.example/pr/1",
			},
		);
	});

	it("emits a failure commit status for an unclean parked verdict", () => {
		assert.equal(buildStatusPayload({ sha: HEAD, verdict: "parked", reviewerFamily: "claude" }).state, "failure");
	});

	it("emits a review-passed status for branch protection", () => {
		assert.deepEqual(
			buildReviewPassedStatusPayload({
				sha: HEAD,
				verdict: "clean",
				reviewerFamily: "codex",
			}),
			{
				context: "review-passed",
				description: `verdict=clean reviewer_family=codex head=${HEAD}`,
				state: "success",
			},
		);
	});

	it("emits a failing review-passed status for parked verdicts", () => {
		assert.equal(
			buildReviewPassedStatusPayload({ sha: HEAD, verdict: "parked", reviewerFamily: "claude" }).state,
			"failure",
		);
	});

	it("emits a green review status and a separate merge park marker for clean human-gated reviews", () => {
		assert.deepEqual(
			buildStatusPayload({
				sha: HEAD,
				verdict: "clean",
				reviewerFamily: "codex",
			}),
			{
				context: "final-review",
				description: `verdict=clean reviewer_family=codex head=${HEAD}`,
				state: "success",
			},
		);
		assert.deepEqual(
			buildHumanMergeRequiredStatusPayload({
				sha: HEAD,
				reviewerFamily: "codex",
				targetUrl: "https://github.example/pr/180",
			}),
			{
				context: "merge-park",
				description: `reason=human-required reviewer_family=codex head=${HEAD}`,
				state: "success",
				target_url: "https://github.example/pr/180",
			},
		);
	});

	it("rejects a misleading humanMergeRequired core payload option", () => {
		assert.throws(
			() =>
				buildStatusPayload({
					sha: HEAD,
					verdict: "clean",
					reviewerFamily: "codex",
					humanMergeRequired: true,
				}),
			/buildHumanMergeRequiredStatusPayload/,
		);
	});

	it("rejects non-full SHA values so the gate cannot bless an ambiguous ref", () => {
		assert.throws(
			() => buildStatusPayload({ sha: "main", verdict: "clean", reviewerFamily: "codex" }),
			/full 40-character head SHA/,
		);
	});
});

describe("final-review status evaluation", () => {
	it("accepts only the latest clean success status for the exact head", () => {
		const result = evaluateFinalReviewStatuses(
			[
				{
					context: "final-review",
					state: "failure",
					description: `verdict=parked reviewer_family=codex head=${HEAD}`,
					created_at: "2026-07-08T00:00:00Z",
				},
				{
					context: "final-review",
					state: "success",
					description: `verdict=clean reviewer_family=codex head=${HEAD}`,
					created_at: "2026-07-08T00:01:00Z",
				},
			],
			HEAD,
		);

		assert.deepEqual(result, {
			ok: true,
			reason: "clean",
			verdict: "clean",
			reviewerFamily: "codex",
			head: HEAD,
			state: "success",
		});
	});

	it("rejects stale status descriptions that refer to a prior pushed head", () => {
		const result = evaluateFinalReviewStatuses(
			[
				{
					context: "final-review",
					state: "success",
					description: `verdict=clean reviewer_family=codex head=${OTHER}`,
				},
			],
			HEAD,
		);

		assert.equal(result.ok, false);
		assert.equal(result.reason, "stale-head");
	});

	it("rejects a newer parked status even when an older clean status exists on the same head", () => {
		const result = evaluateFinalReviewStatuses(
			[
				{
					context: "final-review",
					state: "failure",
					description: `verdict=parked reviewer_family=codex head=${HEAD}`,
					created_at: "2026-07-08T00:02:00Z",
				},
				{
					context: "final-review",
					state: "success",
					description: `verdict=clean reviewer_family=codex head=${HEAD}`,
					created_at: "2026-07-08T00:01:00Z",
				},
			],
			HEAD,
		);

		assert.equal(result.ok, false);
		assert.equal(result.reason, "unclean-final-review");
	});

	it("fails closed when same-timestamp final-review statuses disagree", () => {
		const result = evaluateFinalReviewStatuses(
			[
				{
					context: "final-review",
					state: "success",
					description: `verdict=clean reviewer_family=codex head=${HEAD}`,
					created_at: "2026-07-08T00:01:00Z",
				},
				{
					context: "final-review",
					state: "failure",
					description: `verdict=parked reviewer_family=codex head=${HEAD}`,
					created_at: "2026-07-08T00:01:00Z",
				},
			],
			HEAD,
		);

		assert.equal(result.ok, false);
		assert.equal(result.reason, "unclean-final-review");
	});

	it("rejects malformed, clean-worded, and reviewer-less statuses", () => {
		assert.equal(
			evaluateFinalReviewStatuses(
				[{ context: "final-review", state: "success", description: "status=needs_review" }],
				HEAD,
			).reason,
			"invalid-final-review-status",
		);
		assert.equal(
			evaluateFinalReviewStatuses(
				[
					{
						context: "final-review",
						state: "failure",
						description: `verdict=clean reviewer_family=codex head=${HEAD}`,
					},
				],
				HEAD,
			).reason,
			"unclean-final-review",
		);
		assert.equal(
			evaluateFinalReviewStatuses(
				[{ context: "final-review", state: "success", description: `verdict=clean head=${HEAD}` }],
				HEAD,
			).reason,
			"missing-reviewer-family",
		);
	});

	it("rejects ao native review state values because they are not final-review verdicts", () => {
		const parsed = parseStatusDescription("status=needs_review reviewer_family=codex");

		assert.deepEqual(parsed, {});
		assert.equal(evaluateFinalReviewStatuses([], HEAD).reason, "missing-final-review-status");
	});
});

describe("autonomous merge evaluation", () => {
	it("passes the human review gate but rejects autonomous merge when a current-head human park marker exists", () => {
		const statuses = [
			{
				context: "final-review",
				state: "success",
				description: `verdict=clean reviewer_family=codex head=${HEAD}`,
				created_at: "2026-07-08T00:01:00Z",
			},
			{
				context: "merge-park",
				state: "success",
				description: `reason=human-required reviewer_family=codex head=${HEAD}`,
				created_at: "2026-07-08T00:02:00Z",
			},
		];

		assert.equal(evaluateFinalReviewStatuses(statuses, HEAD).ok, true);
		assert.deepEqual(evaluateAutonomousMergeStatuses(statuses, HEAD, []), {
			ok: false,
			reason: "human-merge-required",
			reviewerFamily: "codex",
			head: HEAD,
			state: "success",
		});
	});

	it("rejects merge park markers that declare a different head", () => {
		assert.deepEqual(
			evaluateAutonomousMergeStatuses(
				[
					{
						context: "final-review",
						state: "success",
						description: `verdict=clean reviewer_family=codex head=${HEAD}`,
					},
					{
						context: "merge-park",
						state: "success",
						description: `reason=human-required reviewer_family=codex head=${OTHER}`,
					},
				],
				HEAD,
				[],
			),
			{
				ok: false,
				reason: "invalid-merge-park-status",
				head: HEAD,
				state: "success",
			},
		);
	});

	it("rejects malformed merge park markers on the current head", () => {
		assert.deepEqual(
			evaluateAutonomousMergeStatuses(
				[
					{
						context: "final-review",
						state: "success",
						description: `verdict=clean reviewer_family=codex head=${HEAD}`,
					},
					{
						context: "merge-park",
						state: "success",
						description: `reason=manual reviewer_family=codex head=${HEAD}`,
					},
				],
				HEAD,
				[],
			),
			{
				ok: false,
				reason: "invalid-merge-park-status",
				head: HEAD,
				state: "success",
			},
		);
	});

	it("rejects human park markers without a reviewer family", () => {
		assert.deepEqual(
			evaluateAutonomousMergeStatuses(
				[
					{
						context: "final-review",
						state: "success",
						description: `verdict=clean reviewer_family=codex head=${HEAD}`,
					},
					{
						context: "merge-park",
						state: "success",
						description: `reason=human-required head=${HEAD}`,
					},
				],
				HEAD,
				[],
			),
			{
				ok: false,
				reason: "missing-merge-park-reviewer-family",
				head: HEAD,
				state: "success",
			},
		);
	});

	it("rejects autonomous merge when a linked issue carries a manual worker hold", () => {
		const statuses = [
			{
				context: "final-review",
				state: "success",
				description: `verdict=clean reviewer_family=codex head=${HEAD}`,
			},
		];

		assert.deepEqual(evaluateAutonomousMergeStatuses(statuses, HEAD, [{ number: 12, labels: ["no-ao"] }]), {
			ok: false,
			reason: "manual-worker-hold",
			issue: 12,
			holdSource: "label",
			holdToken: "no-ao",
			head: HEAD,
		});
		assert.equal(
			evaluateAutonomousMergeStatuses(statuses, HEAD, [
				{ number: 12, body: "No AO decision yet.", comments: ["no-ao\nExternal worker stamped this."] },
			]).reason,
			"manual-worker-hold",
		);
		assert.equal(
			evaluateAutonomousMergeStatuses(statuses, HEAD, [
				{ number: 12, comments: ["no-ao removed - handing this back to AO"] },
			]).ok,
			true,
		);
	});
});

describe("final-review status CLI validation", () => {
	it("posts final-review and review-passed statuses for a clean review", () => {
		const dir = mkdtempSync(join(tmpdir(), "final-review-gh-"));
		const log = join(dir, "gh.log");
		const gh = join(dir, "gh");
		writeFileSync(
			gh,
			`#!/usr/bin/env node
const fs = require("node:fs");
fs.appendFileSync(process.env.GH_LOG, JSON.stringify(process.argv.slice(2)) + "\\n");
process.stdout.write("{}\\n");
`,
		);
		chmodSync(gh, 0o755);

		const result = spawnSync(
			process.execPath,
			[
				CLI.pathname,
				"set",
				"--repo",
				"owner/repo",
				"--sha",
				HEAD,
				"--verdict",
				"clean",
				"--reviewer-family",
				"codex",
				"--author-family",
				"claude",
			],
			{
				encoding: "utf8",
				env: {
					...process.env,
					GH_LOG: log,
					PATH: `${dir}:${process.env.PATH}`,
				},
			},
		);

		assert.equal(result.status, 0, result.stderr);
		const contexts = readFileSync(log, "utf8")
			.trim()
			.split("\n")
			.map((line) => JSON.parse(line).find((arg) => arg.startsWith("context=")));
		assert.deepEqual(contexts, ["context=final-review", "context=review-passed"]);
	});

	it("posts the human merge park guard before the green final-review status", () => {
		const dir = mkdtempSync(join(tmpdir(), "final-review-gh-"));
		const log = join(dir, "gh.log");
		const gh = join(dir, "gh");
		writeFileSync(
			gh,
			`#!/usr/bin/env node
const fs = require("node:fs");
fs.appendFileSync(process.env.GH_LOG, JSON.stringify(process.argv.slice(2)) + "\\n");
process.stdout.write("{}\\n");
`,
		);
		chmodSync(gh, 0o755);

		const result = spawnSync(
			process.execPath,
			[
				CLI.pathname,
				"set",
				"--repo",
				"owner/repo",
				"--sha",
				HEAD,
				"--verdict",
				"clean",
				"--reviewer-family",
				"codex",
				"--author-family",
				"claude",
				"--human-merge-required",
			],
			{
				encoding: "utf8",
				env: {
					...process.env,
					GH_LOG: log,
					PATH: `${dir}:${process.env.PATH}`,
				},
			},
		);

		assert.equal(result.status, 0, result.stderr);
		const calls = readFileSync(log, "utf8")
			.trim()
			.split("\n")
			.map((line) => JSON.parse(line));
		const contexts = calls.map((args) => args.find((arg) => arg.startsWith("context=")));
		assert.deepEqual(contexts, ["context=merge-park", "context=final-review", "context=review-passed"]);
	});

	it("refuses a clean set with a same-family reviewer before shelling out to gh", () => {
		// PATH:"" guarantees no gh is reachable; the independence check must fail
		// first, proving nothing is posted for a non-independent review.
		const result = spawnSync(
			process.execPath,
			[
				CLI.pathname,
				"set",
				"--repo",
				"owner/repo",
				"--sha",
				HEAD,
				"--verdict",
				"clean",
				"--reviewer-family",
				"codex",
				"--author-family",
				"codex",
			],
			{ encoding: "utf8", env: { ...process.env, PATH: "" } },
		);

		assert.equal(result.status, 1);
		assert.match(result.stderr, /non-independent review/);
	});

	it("refuses a clean set with no author-family before shelling out to gh", () => {
		const result = spawnSync(
			process.execPath,
			[CLI.pathname, "set", "--repo", "owner/repo", "--sha", HEAD, "--verdict", "clean", "--reviewer-family", "codex"],
			{ encoding: "utf8", env: { ...process.env, PATH: "" } },
		);

		assert.equal(result.status, 1);
		assert.match(result.stderr, /requires at least one --author-family/);
	});

	it("accepts a clean set with multiple independent author families", () => {
		const dir = mkdtempSync(join(tmpdir(), "final-review-gh-"));
		const log = join(dir, "gh.log");
		const gh = join(dir, "gh");
		writeFileSync(
			gh,
			`#!/usr/bin/env node
const fs = require("node:fs");
fs.appendFileSync(process.env.GH_LOG, JSON.stringify(process.argv.slice(2)) + "\\n");
process.stdout.write("{}\\n");
`,
		);
		chmodSync(gh, 0o755);

		const result = spawnSync(
			process.execPath,
			[
				CLI.pathname,
				"set",
				"--repo",
				"owner/repo",
				"--sha",
				HEAD,
				"--verdict",
				"clean",
				"--reviewer-family",
				"claude",
				"--author-family",
				"codex",
				"--author-family",
				"fugu",
			],
			{ encoding: "utf8", env: { ...process.env, GH_LOG: log, PATH: `${dir}:${process.env.PATH}` } },
		);

		assert.equal(result.status, 0, result.stderr);
		const contexts = readFileSync(log, "utf8")
			.trim()
			.split("\n")
			.map((line) => JSON.parse(line).find((arg) => arg.startsWith("context=")));
		assert.deepEqual(contexts, ["context=final-review", "context=review-passed"]);
	});

	it("rejects human merge required with an unclean review verdict before posting statuses", () => {
		const result = spawnSync(
			process.execPath,
			[
				CLI.pathname,
				"set",
				"--repo",
				"owner/repo",
				"--sha",
				HEAD,
				"--verdict",
				"parked",
				"--reviewer-family",
				"codex",
				"--human-merge-required",
			],
			{ encoding: "utf8" },
		);

		assert.equal(result.status, 1);
		assert.match(result.stderr, /only valid with --verdict clean/);
	});

	it("validates check mode before shelling out to gh", () => {
		const result = spawnSync(
			process.execPath,
			[CLI.pathname, "check", "--repo", "owner/repo", "--sha", HEAD, "--mode", "robot"],
			{ encoding: "utf8", env: { ...process.env, PATH: "" } },
		);

		assert.equal(result.status, 1);
		assert.match(result.stderr, /--mode must be human or autonomous/);
	});

	it("requires a PR number for autonomous checks so linked issue holds cannot be skipped", () => {
		const result = spawnSync(
			process.execPath,
			[CLI.pathname, "check", "--repo", "owner/repo", "--sha", HEAD, "--mode", "autonomous"],
			{ encoding: "utf8", env: { ...process.env, PATH: "" } },
		);

		assert.equal(result.status, 1);
		assert.match(result.stderr, /--pr is required for autonomous mode/);
	});

	// #313 review-authority collapse: autonomous mode is exactly a clean, SHA-current
	// final-review status plus no current-head merge-park and no linked issue hold.
	// It must NOT consult GitHub review states.
	it("passes autonomous check on a clean final-review and linked issues without reading GitHub reviews", () => {
		const dir = mkdtempSync(join(tmpdir(), "final-review-gh-"));
		const log = join(dir, "gh.log");
		const gh = join(dir, "gh");
		writeFileSync(
			gh,
			`#!/usr/bin/env node
const fs = require("node:fs");
const argv = process.argv.slice(2);
fs.appendFileSync(process.env.GH_LOG, JSON.stringify(argv) + "\\n");
const path = argv.find((a) => a.includes("/commits/")) || "";
if (path.endsWith("/statuses")) {
	process.stdout.write(
		JSON.stringify([
			{
				context: "final-review",
				state: "success",
				description: "verdict=clean reviewer_family=codex head=${HEAD}",
				updated_at: "2026-07-13T00:00:00Z",
			},
		]) + "\\n",
	);
} else if (argv.includes("graphql")) {
	process.stdout.write(JSON.stringify({
		data: {
			repository: {
				pullRequest: {
					closingIssuesReferences: {
						pageInfo: { hasNextPage: false },
						nodes: [
							{
								number: 12,
								body: "ready for AO",
								labels: { pageInfo: { hasNextPage: false }, nodes: [] },
								comments: { pageInfo: { hasPreviousPage: false }, nodes: [] }
							}
						]
					}
				}
			}
		}
	}) + "\\n");
} else {
	process.stdout.write("null\\n");
}
`,
		);
		chmodSync(gh, 0o755);

		const result = spawnSync(
			process.execPath,
			[CLI.pathname, "check", "--repo", "owner/repo", "--sha", HEAD, "--mode", "autonomous", "--pr", "9"],
			{ encoding: "utf8", env: { ...process.env, GH_LOG: log, PATH: `${dir}:${process.env.PATH}` } },
		);

		assert.equal(result.status, 0, result.stderr);
		const calls = readFileSync(log, "utf8").trim().split("\n").map(JSON.parse);
		// Statuses + linked-issue GraphQL, but nothing touching reviews or pulls.
		assert.equal(calls.length, 2, `expected statuses + graphql calls, got ${JSON.stringify(calls)}`);
		const flat = calls.flat().join(" ");
		assert.match(flat, /commits\/.*\/statuses/);
		assert.match(flat, /graphql/);
		assert.doesNotMatch(flat, /\/reviews|\/pulls/);
	});

	it("fails autonomous check for a PR whose closing issue has a manual worker hold", () => {
		const dir = mkdtempSync(join(tmpdir(), "final-review-gh-"));
		const log = join(dir, "gh.log");
		const gh = join(dir, "gh");
		writeFileSync(
			gh,
			`#!/usr/bin/env node
const fs = require("node:fs");
const argv = process.argv.slice(2);
fs.appendFileSync(process.env.GH_LOG, JSON.stringify(argv) + "\\n");
const joined = argv.join(" ");
if (joined.includes("/commits/") && joined.includes("/statuses")) {
	process.stdout.write(JSON.stringify([
		{
			context: "final-review",
			state: "success",
			description: "verdict=clean reviewer_family=codex head=${HEAD}",
			updated_at: "2026-07-13T00:00:00Z"
		}
	]) + "\\n");
} else if (argv.includes("graphql")) {
	process.stdout.write(JSON.stringify({
		data: {
			repository: {
				pullRequest: {
					closingIssuesReferences: {
						pageInfo: { hasNextPage: false },
						nodes: [
							{
								number: 345,
								body: "tracked work",
								labels: { pageInfo: { hasNextPage: false }, nodes: [{ name: "no-ao" }] },
								comments: { pageInfo: { hasPreviousPage: false }, nodes: [] }
							}
						]
					}
				}
			}
		}
	}) + "\\n");
} else {
	process.stdout.write("null\\n");
}
`,
		);
		chmodSync(gh, 0o755);

		const result = spawnSync(
			process.execPath,
			[CLI.pathname, "check", "--repo", "owner/repo", "--sha", HEAD, "--mode", "autonomous", "--pr", "9"],
			{ encoding: "utf8", env: { ...process.env, GH_LOG: log, PATH: `${dir}:${process.env.PATH}` } },
		);

		assert.equal(result.status, 1);
		const parsed = JSON.parse(result.stdout);
		assert.equal(parsed.reason, "manual-worker-hold");
		assert.equal(parsed.issue, 345);
		const calls = readFileSync(log, "utf8").trim().split("\n").map(JSON.parse);
		assert.equal(calls.length, 2, `expected statuses + graphql calls, got ${JSON.stringify(calls)}`);
		assert.match(calls.flat().join(" "), /graphql/);
	});

	it("fails autonomous check when the requested PR cannot be scanned", () => {
		const dir = mkdtempSync(join(tmpdir(), "final-review-gh-"));
		const gh = join(dir, "gh");
		writeFileSync(
			gh,
			`#!/usr/bin/env node
const argv = process.argv.slice(2);
const joined = argv.join(" ");
if (joined.includes("/commits/") && joined.includes("/statuses")) {
	process.stdout.write(JSON.stringify([
		{
			context: "final-review",
			state: "success",
			description: "verdict=clean reviewer_family=codex head=${HEAD}",
			updated_at: "2026-07-13T00:00:00Z"
		}
	]) + "\\n");
} else if (argv.includes("graphql")) {
	process.stdout.write(JSON.stringify({ data: { repository: { pullRequest: null } } }) + "\\n");
} else {
	process.stdout.write("null\\n");
}
`,
		);
		chmodSync(gh, 0o755);

		const result = spawnSync(
			process.execPath,
			[CLI.pathname, "check", "--repo", "owner/repo", "--sha", HEAD, "--mode", "autonomous", "--pr", "999"],
			{ encoding: "utf8", env: { ...process.env, PATH: `${dir}:${process.env.PATH}` } },
		);

		assert.equal(result.status, 1);
		assert.match(result.stderr, /pull request #999 was not found/);
	});

	it("pages linked issue comments before deciding autonomous hold status", () => {
		const dir = mkdtempSync(join(tmpdir(), "final-review-gh-"));
		const log = join(dir, "gh.log");
		const gh = join(dir, "gh");
		writeFileSync(
			gh,
			`#!/usr/bin/env node
const fs = require("node:fs");
const counterPath = process.env.GH_COUNTER;
const argv = process.argv.slice(2);
fs.appendFileSync(process.env.GH_LOG, JSON.stringify(argv) + "\\n");
const joined = argv.join(" ");
if (joined.includes("/commits/") && joined.includes("/statuses")) {
	process.stdout.write(JSON.stringify([
		{
			context: "final-review",
			state: "success",
			description: "verdict=clean reviewer_family=codex head=${HEAD}",
			updated_at: "2026-07-13T00:00:00Z"
		}
	]) + "\\n");
} else if (argv.includes("graphql")) {
	const count = Number(fs.existsSync(counterPath) ? fs.readFileSync(counterPath, "utf8") : "0") + 1;
	fs.writeFileSync(counterPath, String(count));
	if (count === 1) {
		process.stdout.write(JSON.stringify({
			data: {
				repository: {
					pullRequest: {
						closingIssuesReferences: {
							pageInfo: { hasNextPage: false, endCursor: null },
							nodes: [
								{
									id: "I_345",
									number: 345,
									body: "tracked work",
									labels: { pageInfo: { hasNextPage: false }, nodes: [] },
									comments: {
										pageInfo: { hasNextPage: true, endCursor: "comment-page-1" },
										nodes: [{ body: "ordinary discussion" }]
									}
								}
							]
						}
					}
				}
			}
		}) + "\\n");
	} else {
		process.stdout.write(JSON.stringify({
			data: {
				node: {
					comments: {
						pageInfo: { hasNextPage: false, endCursor: null },
						nodes: [{ body: "no-ao: external worker owns this" }]
					}
				}
			}
		}) + "\\n");
	}
} else {
	process.stdout.write("null\\n");
}
`,
		);
		chmodSync(gh, 0o755);

		const result = spawnSync(
			process.execPath,
			[CLI.pathname, "check", "--repo", "owner/repo", "--sha", HEAD, "--mode", "autonomous", "--pr", "9"],
			{
				encoding: "utf8",
				env: { ...process.env, GH_COUNTER: join(dir, "gh.counter"), GH_LOG: log, PATH: `${dir}:${process.env.PATH}` },
			},
		);

		assert.equal(result.status, 1);
		const parsed = JSON.parse(result.stdout);
		assert.equal(parsed.reason, "manual-worker-hold");
		assert.equal(parsed.issue, 345);
		assert.equal(parsed.holdSource, "comment");
		const calls = readFileSync(log, "utf8").trim().split("\n").map(JSON.parse);
		assert.equal(calls.length, 3, `expected statuses + two graphql calls, got ${JSON.stringify(calls)}`);
	});
});

describe("repo slug validation", () => {
	it("accepts owner/name and rejects path traversal", () => {
		assert.equal(normalizeRepoSlug("polymath-ventures/agent-orchestrator"), "polymath-ventures/agent-orchestrator");
		assert.throws(() => normalizeRepoSlug("polymath-ventures/../agent-orchestrator"), /owner\/name/);
	});
});
