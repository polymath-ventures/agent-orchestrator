#!/usr/bin/env node
import { existsSync } from "node:fs";
import { spawnSync } from "node:child_process";

// Keep these needles constructed so the guard and its tests do not become
// the tracked drift they are meant to forbid.
const SX_MANAGED = ["@sx", "managed"].join("-");
const VAULT_SLUGS = [["agent", "vault"].join("-"), ["polymath", "agent", "assets"].join("-")];
const FORBIDDEN_TRACKED_PATHS = ["AGENTS.shared.md"];
const FORBIDDEN_BASENAMES = ["polyscribe.sh"];
const GENERATED_BANNER =
	"<!-- GENERATED — DO NOT EDIT. Edit agent-instructions/{source,agent-overrides,system}/, then rebuild with polyscribe (system scope adds --system) -->";
const FAIL_OPEN_STUBS = {
	"AGENTS.md": renderFailOpenStub("Codex", "codex"),
	"CLAUDE.md": renderFailOpenStub("Claude", "claude"),
};

function git(args, options = {}) {
	const result = spawnSync("git", args, { encoding: "utf8", ...options });
	if (result.status !== 0 && !options.allowNoMatches) {
		throw new Error(`git ${args.join(" ")} failed: ${result.stderr || result.stdout}`);
	}
	return result;
}

function repoRoot() {
	const result = git(["rev-parse", "--show-toplevel"]);
	return result.stdout.trim();
}

function trackedFiles() {
	const result = git(["ls-files", "-z"]);
	return result.stdout.split("\0").filter(Boolean).sort();
}

function gitBlob(path) {
	const result = git(["show", `:${path}`], { allowNoMatches: true });
	if (result.status !== 0) return null;
	return result.stdout;
}

function workingTreeBasenameMatches(names) {
	const result = spawnSync(
		"find",
		[
			".",
			"(",
			"-path",
			"./.git",
			"-o",
			"-path",
			"./frontend/node_modules",
			"-o",
			"-path",
			"./node_modules",
			")",
			"-prune",
			"-o",
			"-type",
			"f",
			"(",
			...names.flatMap((name, index) => (index === 0 ? ["-name", name] : ["-o", "-name", name])),
			")",
			"-print",
		],
		{ encoding: "utf8" },
	);
	if (result.status !== 0) throw new Error(`find forbidden basenames failed: ${result.stderr || result.stdout}`);
	return result.stdout
		.split("\n")
		.filter(Boolean)
		.map((path) => path.replace(/^\.\//, ""))
		.sort();
}

function grepTracked(pattern, pathspecs = []) {
	const result = git(["grep", "--cached", "-Il", "-e", pattern, "--", ...pathspecs], {
		allowNoMatches: true,
	});
	if (result.status === 1) return [];
	if (result.status !== 0) throw new Error(result.stderr || result.stdout);
	return result.stdout.split("\n").filter(Boolean).sort();
}

function renderFailOpenStub(client, override) {
	return `${GENERATED_BANNER}

# Agent instructions — fail-open baseline (${client})

SessionStart normally injects the current vault rules plus this repository’s local context. If that context is absent, use this safety baseline only.

Before acting, read the ordered Markdown fragments under \`agent-instructions/source/\` and \`agent-instructions/agent-overrides/${override}.md\`.

1. GitHub Issues are the sole durable tracker.
2. Make every mutation in an agent-owned worktree, never the shared checkout.
3. For behavior changes, write a failing test first, then implement and verify the fix.
4. Verify the result with the repository’s real checks before claiming success.
5. Use an independent reviewer; do not self-review merge readiness.
6. Never merge without explicit authorization from the user. In autonomous mode, merge only after final-review is clean, CI is green, and all current-head review threads are resolved.
`;
}

function staleFailOpenOutputs(files) {
	const repoUsesPolyscribe =
		files.includes("agent-instructions/standard-set.json") ||
		Object.keys(FAIL_OPEN_STUBS).some((path) => files.includes(path) || existsSync(path));
	if (!repoUsesPolyscribe) return [];

	return Object.entries(FAIL_OPEN_STUBS)
		.flatMap(([path, expected]) => {
			if (!files.includes(path)) return [`${path} (not tracked)`];
			const actual = gitBlob(path);
			if (actual !== expected) return [path];
			return [];
		})
		.sort();
}

function main() {
	process.chdir(repoRoot());

	const files = trackedFiles();
	const managedOutsideGithub = grepTracked(SX_MANAGED, [":!.github"]);
	const vaultSlugReferences = VAULT_SLUGS.flatMap((slug) =>
		grepTracked(slug).map((path) => `${path} (${slug})`),
	).sort();
	const forbiddenTrackedPaths = files.filter((path) => FORBIDDEN_TRACKED_PATHS.includes(path));
	const trackedPolyscribeCopies = files.filter((path) =>
		FORBIDDEN_BASENAMES.some((name) => path === name || path.endsWith(`/${name}`)),
	);
	const workingTreePolyscribeCopies = workingTreeBasenameMatches(FORBIDDEN_BASENAMES);
	const explicitScriptsCopy = existsSync("scripts/polyscribe.sh") ? ["scripts/polyscribe.sh"] : [];
	const staleOutputs = staleFailOpenOutputs(files);

	const failures = [];
	if (staleOutputs.length > 0) {
		failures.push({
			title: "tracked fail-open instruction output is stale",
			items: staleOutputs,
		});
	}
	if (forbiddenTrackedPaths.length > 0) {
		failures.push({
			title: "generated agent instruction output is tracked",
			items: forbiddenTrackedPaths,
		});
	}
	if (trackedPolyscribeCopies.length > 0 || workingTreePolyscribeCopies.length > 0 || explicitScriptsCopy.length > 0) {
		failures.push({
			title: "repo-local polyscribe copy is forbidden",
			items: [...new Set([...trackedPolyscribeCopies, ...workingTreePolyscribeCopies, ...explicitScriptsCopy])],
		});
	}
	if (managedOutsideGithub.length > 0) {
		failures.push({
			title: "tracked sx-managed marker outside .github",
			items: managedOutsideGithub,
		});
	}
	if (vaultSlugReferences.length > 0) {
		failures.push({
			title: "vault slug reference in tracked file",
			items: vaultSlugReferences,
		});
	}

	if (failures.length > 0) {
		for (const failure of failures) {
			console.error(`canonical sx drift: ${failure.title}`);
			for (const item of failure.items) console.error(`  ${item}`);
		}
		process.exit(1);
	}

	console.log("canonical sx drift: clean");
}

try {
	main();
} catch (error) {
	console.error(`canonical sx drift: ${error.message}`);
	process.exit(1);
}
