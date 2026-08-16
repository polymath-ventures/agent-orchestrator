#!/usr/bin/env node
// @sx-managed: polypowers-init-one-closing-issue

"use strict";

const { readFileSync } = require("node:fs");
const { spawnSync } = require("node:child_process");

const STATUS_CONTEXT = "one-closing-issue";
const CONTRACT_REFERENCE =
	/(?:^|[^A-Za-z0-9_-])(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[ \t]*:?[ \t]+(?:(?:[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)?#[1-9][0-9]*|https:\/\/github\.com\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/issues\/[1-9][0-9]*)(?![A-Za-z0-9_])/i;

function normalizeLabels(labels) {
	return (Array.isArray(labels) ? labels : [])
		.map((label) => (typeof label === "string" ? label : label?.name))
		.filter((label) => typeof label === "string")
		.map((label) => label.trim().toLowerCase());
}

function evaluatePullRequest(pullRequest) {
	const body = typeof pullRequest?.body === "string" ? pullRequest.body : "";
	const count = pullRequest?.closingIssueCount;
	if (!Number.isInteger(count) || count < 0) {
		return { ok: false, description: "GitHub closing-issue count is unavailable" };
	}
	if (count > 1) {
		return { ok: false, description: `GitHub reports ${count} closing issues; expected exactly one` };
	}
	if (count === 1) {
		return CONTRACT_REFERENCE.test(body)
			? { ok: true, description: "PR body links exactly one closing issue" }
			: { ok: false, description: "use Closes/Fixes/Resolves #N in the PR body" };
	}
	if (normalizeLabels(pullRequest?.labels).includes("no-close")) {
		return { ok: true, description: "standalone PR accepted by no-close label" };
	}
	return {
		ok: false,
		description: "add exactly one Closes/Fixes/Resolves #N or a standalone escape",
	};
}

function runGh(args) {
	const result = spawnSync("gh", args, { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] });
	if (result.status !== 0) throw new Error(`gh ${args.slice(0, 2).join(" ")} failed`);
	return result.stdout.trim();
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

function fetchClosingIssueCount(repository, number) {
	const [owner, name, extra] = repository.split("/");
	if (!owner || !name || extra) throw new Error("GITHUB_REPOSITORY must be owner/name");
	const query = `query($owner:String!, $name:String!, $number:Int!) {
    repository(owner:$owner, name:$name) {
      pullRequest(number:$number) { closingIssuesReferences(first:100) { totalCount } }
    }
  }`;
	const value = runGh([
		"api",
		"graphql",
		"-f",
		`query=${query}`,
		"-f",
		`owner=${owner}`,
		"-f",
		`name=${name}`,
		"-F",
		`number=${number}`,
		"--jq",
		".data.repository.pullRequest.closingIssuesReferences.totalCount",
	]);
	if (!/^[0-9]+$/.test(value)) throw new Error("GitHub returned an invalid closing-issue count");
	return Number(value);
}

function main() {
	const eventPath = process.env.GITHUB_EVENT_PATH;
	const repository = process.env.GITHUB_REPOSITORY;
	if (!eventPath || !repository) throw new Error("GITHUB_EVENT_PATH and GITHUB_REPOSITORY are required");
	const event = JSON.parse(readFileSync(eventPath, "utf8"));
	const pullRequest = event.pull_request;
	const sha = pullRequest?.head?.sha;
	const number = pullRequest?.number ?? event.number;
	if (!sha || !Number.isInteger(number)) throw new Error("pull request number and head SHA are required");
	try {
		const verdict = evaluatePullRequest({
			...pullRequest,
			closingIssueCount: fetchClosingIssueCount(repository, number),
		});
		postStatus(repository, sha, verdict.ok ? "success" : "failure", verdict.description);
		if (!verdict.ok) process.exitCode = 1;
	} catch (error) {
		postStatus(repository, sha, "error", error.message);
		throw error;
	}
}

module.exports = { STATUS_CONTEXT, evaluatePullRequest };

if (require.main === module) {
	try {
		main();
	} catch (error) {
		console.error(`${STATUS_CONTEXT}: ${error.message}`);
		process.exitCode = 2;
	}
}
