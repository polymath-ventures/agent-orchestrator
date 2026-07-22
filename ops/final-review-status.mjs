#!/usr/bin/env node
import { execFileSync } from "node:child_process";

import {
	FINAL_REVIEW_CONTEXT,
	assertCleanReviewIndependence,
	assertFullSHA,
	buildHumanMergeRequiredStatusPayload,
	buildReviewPassedStatusPayload,
	buildStatusPayload,
	evaluateAutonomousMergeStatuses,
	evaluateFinalReviewStatuses,
	normalizeRepoSlug,
} from "./final-review-status-core.mjs";

function usage(exitCode = 1) {
	const out = exitCode === 0 ? process.stdout : process.stderr;
	out.write(`Usage:
  node ops/final-review-status.mjs set --repo owner/repo --sha <full-head-sha> --verdict <clean|parked> --reviewer-family <family> --author-family <family> [--author-family <family>] [--target-url <url>] [--human-merge-required]
  node ops/final-review-status.mjs check --repo owner/repo --sha <full-head-sha> [--mode human]
  node ops/final-review-status.mjs check --repo owner/repo --sha <full-head-sha> --mode autonomous --pr <number>

A clean set REQUIRES one or more --author-family (the implementer family/families)
and is refused when --reviewer-family matches any of them: reviewer independence is
enforced here, at write time, so a clean ${FINAL_REVIEW_CONTEXT} status is
independent by construction. Parked verdicts need no --author-family.

The check command exits 0 only for a successful ${FINAL_REVIEW_CONTEXT} status
whose description says verdict=clean and head=<that exact SHA>; it is deliberately
family-agnostic (independence was already enforced at set time), so the required
review-passed merge-queue gate — which cannot see per-session harness provenance —
is never bricked. Autonomous mode also exits non-zero when a current-head
merge-park status requires a human merge or when --pr names a pull request whose
linked issue carries a manual worker hold.
`);
	process.exit(exitCode);
}

const BOOLEAN_FLAGS = new Set(["human_merge_required"]);
// Flags that may be repeated; each occurrence appends to an array.
const REPEATABLE_FLAGS = new Set(["author_family"]);

function parseArgs(argv) {
	const [command, ...rest] = argv;
	if (!command || command === "-h" || command === "--help") usage(command ? 0 : 1);
	const opts = { command };
	for (let i = 0; i < rest.length; i += 1) {
		const arg = rest[i];
		if (!arg.startsWith("--")) throw new Error(`unexpected argument: ${arg}`);
		const key = arg.slice(2).replaceAll("-", "_");
		if (BOOLEAN_FLAGS.has(key)) {
			opts[key] = true;
			continue;
		}
		const value = rest[i + 1];
		if (!value || value.startsWith("--")) throw new Error(`missing value for ${arg}`);
		if (REPEATABLE_FLAGS.has(key)) {
			(opts[key] ??= []).push(value);
		} else {
			opts[key] = value;
		}
		i += 1;
	}
	return opts;
}

function requireOpt(opts, key) {
	const value = String(opts[key] ?? "").trim();
	if (!value) throw new Error(`missing required --${key.replaceAll("_", "-")}`);
	return value;
}

function ghJSON(args, input) {
	const stdout = execFileSync("gh", args, {
		encoding: "utf8",
		input,
		stdio: input === undefined ? ["ignore", "pipe", "pipe"] : ["pipe", "pipe", "pipe"],
	});
	return stdout.trim() ? JSON.parse(stdout) : null;
}

function postStatus(opts) {
	const repo = normalizeRepoSlug(requireOpt(opts, "repo"));
	const sha = assertFullSHA(requireOpt(opts, "sha"));
	const verdict = requireOpt(opts, "verdict");
	const reviewerFamily = requireOpt(opts, "reviewer_family");
	// Enforce reviewer independence at write time (fail-closed) BEFORE any status
	// is posted: a clean status can only exist for a review by a family distinct
	// from every implementer family. This is the sole independence gate; check
	// stays family-agnostic.
	assertCleanReviewIndependence({ verdict, reviewerFamily, authorFamilies: opts.author_family });
	const payload = buildStatusPayload({
		sha,
		verdict,
		reviewerFamily,
		targetUrl: opts.target_url ?? "",
	});
	const reviewPassedPayload = buildReviewPassedStatusPayload({
		sha,
		verdict,
		reviewerFamily,
		targetUrl: opts.target_url ?? "",
	});
	if (opts.human_merge_required && verdict !== "clean") {
		throw new Error("--human-merge-required is only valid with --verdict clean");
	}

	const mergePark = opts.human_merge_required
		? buildHumanMergeRequiredStatusPayload({
				sha,
				reviewerFamily,
				targetUrl: opts.target_url ?? "",
			})
		: null;
	if (mergePark) postGitHubStatus(repo, sha, mergePark);
	postGitHubStatus(repo, sha, payload);
	postGitHubStatus(repo, sha, reviewPassedPayload);

	const result = {
		ok: true,
		context: payload.context,
		state: payload.state,
		description: payload.description,
		head: sha.toLowerCase(),
	};
	if (mergePark) {
		result.mergePark = {
			context: mergePark.context,
			state: mergePark.state,
			description: mergePark.description,
		};
	}
	result.reviewPassed = {
		context: reviewPassedPayload.context,
		state: reviewPassedPayload.state,
		description: reviewPassedPayload.description,
	};
	process.stdout.write(`${JSON.stringify(result)}\n`);
}

function postGitHubStatus(repo, sha, payload) {
	ghJSON(
		[
			"api",
			"--method",
			"POST",
			`repos/${repo}/statuses/${sha}`,
			"-f",
			`state=${payload.state}`,
			"-f",
			`context=${payload.context}`,
			"-f",
			`description=${payload.description}`,
			...(payload.target_url ? ["-f", `target_url=${payload.target_url}`] : []),
		],
		undefined,
	);
}

function checkStatus(opts) {
	const repo = normalizeRepoSlug(requireOpt(opts, "repo"));
	const sha = assertFullSHA(requireOpt(opts, "sha"));
	const mode = String(opts.mode ?? "human").trim();
	if (mode !== "human" && mode !== "autonomous") throw new Error("--mode must be human or autonomous");
	if (mode === "autonomous" && !opts.pr) throw new Error("--pr is required for autonomous mode");
	const statuses = ghJSON(["api", "--method", "GET", `repos/${repo}/commits/${sha}/statuses`, "-f", "per_page=100"]);
	const linkedIssues = mode === "autonomous" ? fetchLinkedIssues(repo, opts.pr) : [];
	const result =
		mode === "autonomous"
			? evaluateAutonomousMergeStatuses(statuses, sha, linkedIssues)
			: evaluateFinalReviewStatuses(statuses, sha);

	process.stdout.write(`${JSON.stringify(result)}\n`);
	if (!result.ok) process.exit(1);
}

function fetchLinkedIssues(repo, pr) {
	const number = Number.parseInt(String(pr ?? ""), 10);
	if (!Number.isInteger(number) || number <= 0) throw new Error("--pr must be a positive pull request number");
	const [owner, name] = repo.split("/");
	const query = `
query($owner: String!, $name: String!, $number: Int!, $issueCursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      closingIssuesReferences(first: 20, after: $issueCursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          number
          body
          labels(first: 100) { pageInfo { hasNextPage } nodes { name } }
          comments(first: 100) { pageInfo { hasNextPage endCursor } nodes { body } }
        }
      }
    }
  }
}`;
	const issues = [];
	let issueCursor = null;
	for (let page = 0; ; page += 1) {
		if (page >= 20) throw new Error("linked issue scan exceeded 400 closing issues");
		const args = [
			"api",
			"graphql",
			"-f",
			`query=${query}`,
			"-F",
			`owner=${owner}`,
			"-F",
			`name=${name}`,
			"-F",
			`number=${number}`,
		];
		if (issueCursor) args.push("-F", `issueCursor=${issueCursor}`);
		const payload = ghJSON(args);
		const pullRequest = payload?.data?.repository?.pullRequest ?? payload?.repository?.pullRequest;
		if (!pullRequest) throw new Error(`linked issue scan failed: pull request #${number} was not found`);
		const refs = pullRequest.closingIssuesReferences;
		if (!refs || !Array.isArray(refs.nodes)) {
			throw new Error(`linked issue scan failed: pull request #${number} missing closing issue references`);
		}
		for (const issue of refs.nodes ?? []) {
			let comments = commentsFromGraphQL(issue);
			if (issue?.comments?.pageInfo?.hasNextPage) {
				comments = comments.concat(
					fetchRemainingIssueComments(issue?.id ?? "", issue?.number ?? "", issue.comments.pageInfo.endCursor ?? ""),
				);
			}
			issues.push({
				number: issue?.number ?? "",
				body: issue?.body ?? "",
				labels: labelsFromGraphQL(issue),
				comments,
			});
		}
		const pageInfo = refs.pageInfo ?? {};
		if (!pageInfo.hasNextPage) return issues;
		if (!pageInfo.endCursor) throw new Error("linked issue scan truncated: missing closing issue cursor");
		issueCursor = pageInfo.endCursor;
	}
}

function fetchRemainingIssueComments(issueID, issueNumber, cursor) {
	if (!issueID) throw new Error(`linked issue scan truncated: issue #${issueNumber} is missing a GraphQL id`);
	if (!cursor) throw new Error(`linked issue scan truncated: issue #${issueNumber} is missing a comment cursor`);
	const query = `
query($id: ID!, $commentCursor: String) {
  node(id: $id) {
    ... on Issue {
      comments(first: 100, after: $commentCursor) {
        pageInfo { hasNextPage endCursor }
        nodes { body }
      }
    }
  }
}`;
	const comments = [];
	let commentCursor = cursor;
	for (let page = 0; ; page += 1) {
		if (page >= 50) throw new Error(`linked issue scan exceeded 5100 comments for issue #${issueNumber}`);
		const payload = ghJSON([
			"api",
			"graphql",
			"-f",
			`query=${query}`,
			"-F",
			`id=${issueID}`,
			"-F",
			`commentCursor=${commentCursor}`,
		]);
		const pageData = payload?.data?.node?.comments ?? payload?.node?.comments ?? {};
		comments.push(...commentsFromGraphQL({ comments: pageData }));
		const pageInfo = pageData.pageInfo ?? {};
		if (!pageInfo.hasNextPage) return comments;
		if (!pageInfo.endCursor)
			throw new Error(`linked issue scan truncated: issue #${issueNumber} missing next comment cursor`);
		commentCursor = pageInfo.endCursor;
	}
}

function labelsFromGraphQL(issue) {
	if (issue?.labels?.pageInfo?.hasNextPage) {
		throw new Error(`linked issue scan truncated: issue #${issue?.number ?? ""} has more than 100 labels`);
	}
	return (issue?.labels?.nodes ?? []).map((label) => label?.name ?? "").filter(Boolean);
}

function commentsFromGraphQL(issue) {
	return (issue?.comments?.nodes ?? []).map((comment) => comment?.body ?? "").filter(Boolean);
}

try {
	const opts = parseArgs(process.argv.slice(2));
	if (opts.command === "set") postStatus(opts);
	else if (opts.command === "check") checkStatus(opts);
	else throw new Error(`unknown command: ${opts.command}`);
} catch (err) {
	process.stderr.write(`final-review-status: ${err.message}\n`);
	process.exit(1);
}
