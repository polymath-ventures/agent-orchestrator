#!/usr/bin/env node
import { existsSync } from "node:fs";
import { spawnSync } from "node:child_process";

// Keep these needles constructed so the guard and its tests do not become
// the tracked drift they are meant to forbid.
const SX_MANAGED = ["@sx", "managed"].join("-");
const VAULT_SLUGS = [["agent", "vault"].join("-"), ["polymath", "agent", "assets"].join("-")];
const REPO_LOCAL_POLYSCRIBE = "scripts/polyscribe.sh";

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

function grepTracked(pattern, pathspecs = []) {
	const result = git(["grep", "--cached", "-Il", "-e", pattern, "--", ...pathspecs], {
		allowNoMatches: true,
	});
	if (result.status === 1) return [];
	if (result.status !== 0) throw new Error(result.stderr || result.stdout);
	return result.stdout.split("\n").filter(Boolean).sort();
}

function main() {
	process.chdir(repoRoot());

	const managedOutsideGithub = grepTracked(SX_MANAGED, [":!.github"]);
	const vaultSlugReferences = VAULT_SLUGS.flatMap((slug) =>
		grepTracked(slug).map((path) => `${path} (${slug})`),
	).sort();

	const failures = [];
	if (existsSync(REPO_LOCAL_POLYSCRIBE)) {
		failures.push({
			title: "repo-local polyscribe copy is forbidden",
			items: [REPO_LOCAL_POLYSCRIBE],
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
