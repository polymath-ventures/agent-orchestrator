#!/usr/bin/env node
import { existsSync, readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";

const SX_MANAGED = ["@sx", "managed"].join("-");
const VAULT_SLUGS = [["agent", "vault"].join("-"), ["polymath", "agent", "assets"].join("-")];
const REPO_LOCAL_POLYSCRIBE = "scripts/polyscribe.sh";

function git(args) {
	const result = spawnSync("git", args, { encoding: "utf8" });
	if (result.status !== 0) {
		throw new Error(`git ${args.join(" ")} failed: ${result.stderr || result.stdout}`);
	}
	return result.stdout;
}

function trackedFiles() {
	return git(["ls-files", "-z"]).split("\0").filter(Boolean).sort();
}

function trackedText(rel) {
	let contents;
	try {
		contents = readFileSync(rel);
	} catch (error) {
		if (["EISDIR", "ENOENT"].includes(error.code)) return null;
		throw error;
	}
	if (contents.includes(0)) return null;
	return contents.toString("utf8");
}

function isGithubManagedException(rel) {
	return rel.startsWith(".github/");
}

function main() {
	const files = trackedFiles();
	const managedOutsideGithub = [];
	const vaultSlugReferences = [];

	for (const rel of files) {
		const text = trackedText(rel);
		if (text === null) continue;

		if (text.includes(SX_MANAGED) && !isGithubManagedException(rel)) {
			managedOutsideGithub.push(rel);
		}

		const matchedSlugs = VAULT_SLUGS.filter((slug) => text.includes(slug));
		if (matchedSlugs.length > 0) {
			vaultSlugReferences.push(`${rel} (${matchedSlugs.join(", ")})`);
		}
	}

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
			title: "vault slug reference outside bootstrap allowlist",
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
