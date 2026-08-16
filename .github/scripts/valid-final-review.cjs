#!/usr/bin/env node
// @sx-managed: polypowers-init-valid-final-review

"use strict";

const { readFileSync } = require("node:fs");
const { spawnSync } = require("node:child_process");

const STATUS_CONTEXT = "valid-final-review";
const SOURCE_CONTEXT = "final-review";
const FULL_SHA = /^[0-9a-f]{40}$/;
const CLEAN_DESCRIPTION = /^verdict=clean reviewer_family=([^\s=]+) head=([0-9a-f]{40})$/;

function evaluateFinalReview(statuses, sha) {
	const latest = (Array.isArray(statuses) ? statuses : []).find((status) => status?.context === SOURCE_CONTEXT);
	if (!latest) return { ok: false, description: "final-review status is missing" };
	if (latest.state !== "success") {
		return { ok: false, description: `final-review state is ${latest.state || "unknown"}` };
	}
	const match = typeof latest.description === "string" ? latest.description.match(CLEAN_DESCRIPTION) : null;
	if (!match) return { ok: false, description: "final-review description is malformed or not clean" };
	if (match[2] !== sha) return { ok: false, description: "final-review description names a stale head" };
	return { ok: true, description: "clean final-review verified for current head" };
}

function isCurrentDefaultBranchPullRequest(pullRequests, sha, defaultBranch) {
	return (Array.isArray(pullRequests) ? pullRequests : []).some(
		(pullRequest) =>
			pullRequest?.state === "open" && pullRequest?.head?.sha === sha && pullRequest?.base?.ref === defaultBranch,
	);
}

function runGh(args) {
	const result = spawnSync("gh", args, { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] });
	if (result.status !== 0) throw new Error(`gh ${args.slice(0, 2).join(" ")} failed`);
	return result.stdout.trim();
}

function fetchJson(repository, endpoint) {
	const value = runGh(["api", `repos/${repository}/${endpoint}`]);
	try {
		return JSON.parse(value);
	} catch {
		throw new Error(`GitHub returned invalid JSON for ${endpoint}`);
	}
}

function postStatus(repository, sha, state, description) {
	runGh([
		"api",
		"-X",
		"POST",
		`repos/${repository}/statuses/${sha}`,
		"-f",
		`state=${state}`,
		"-f",
		`context=${STATUS_CONTEXT}`,
		"-f",
		`description=${description.slice(0, 140)}`,
	]);
}

function main() {
	const eventPath = process.env.GITHUB_EVENT_PATH;
	const repository = process.env.GITHUB_REPOSITORY;
	if (!eventPath || !repository) throw new Error("GITHUB_EVENT_PATH and GITHUB_REPOSITORY are required");
	const event = JSON.parse(readFileSync(eventPath, "utf8"));
	if (event.context !== SOURCE_CONTEXT) return;
	const sha = event.sha;
	const defaultBranch = event.repository?.default_branch;
	if (!FULL_SHA.test(sha || "") || !defaultBranch) {
		throw new Error("status SHA and repository default branch are required");
	}
	try {
		const pulls = fetchJson(repository, `commits/${sha}/pulls?per_page=100`);
		if (!isCurrentDefaultBranchPullRequest(pulls, sha, defaultBranch)) return;
		postStatus(repository, sha, "pending", "checking current final-review status");
		const verdict = evaluateFinalReview(fetchJson(repository, `commits/${sha}/statuses?per_page=100`), sha);
		postStatus(repository, sha, verdict.ok ? "success" : "failure", verdict.description);
		if (!verdict.ok) process.exitCode = 1;
	} catch (error) {
		postStatus(repository, sha, "error", error.message);
		throw error;
	}
}

module.exports = { SOURCE_CONTEXT, STATUS_CONTEXT, evaluateFinalReview, isCurrentDefaultBranchPullRequest };

if (require.main === module) {
	try {
		main();
	} catch (error) {
		console.error(`${STATUS_CONTEXT}: ${error.message}`);
		process.exitCode = 2;
	}
}
