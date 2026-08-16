import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
const read = (rel) => readFileSync(fileURLToPath(new URL(`../${rel}`, import.meta.url)), "utf8");
const readJson = (rel) => JSON.parse(read(rel));
const exists = (rel) => existsSync(fileURLToPath(new URL(`../${rel}`, import.meta.url)));
// Mirrors the comment-stripping/blank-trimming canonicalization in the current
// user-level polyscribe hook; assets#255 records that this local manifest is an
// interim marker-free pin until hook-side regeneration is fixed upstream.
const stripHtmlComments = (text) => text.replace(/<!--[\s\S]*?-->/g, "");
const trimBlankRuns = (text) => {
	const out = [];
	let started = false;
	let pending = 0;
	for (const line of text.split(/\r?\n/)) {
		if (/^\s*$/.test(line)) {
			if (started) pending += 1;
			continue;
		}
		while (pending > 0) {
			out.push("");
			pending -= 1;
		}
		out.push(line);
		started = true;
	}
	return `${out.join("\n")}\n`;
};
const sha256 = (text) => createHash("sha256").update(text).digest("hex");

test("repo state is canonical polypowers-init, not legacy nickify", () => {
	assert.equal(exists("polypowers.json"), true);
	assert.equal(exists(".polypowers-init.json"), true);
	assert.equal(exists("nickify.json"), false);
	assert.equal(exists(".nickified.json"), false);

	const desired = readJson("polypowers.json");
	assert.equal(desired.schema, 1);
	assert.equal(desired.repo, "polymath-ventures/agent-orchestrator");
	assert.equal(desired.repository, "polymath-ventures/agent-orchestrator");
	assert.deepEqual(desired.clients, ["claude-code", "codex"]);
	assert.equal(desired.subsystems.agent_instructions.standard_version, 3);
	assert.deepEqual(desired.subsystems.agent_instructions.opt_outs, {
		"agent-instructions/agent-overrides/agy.md": {
			reason: "repo clients are claude-code and codex only",
		},
	});
	assert.equal(desired.subsystems.github_contract.enabled, true);
});

test("canonical receipt preserves historical runs and records polypowers-init", () => {
	const receipt = readJson(".polypowers-init.json");
	assert.equal(receipt.schema, 1);
	assert.equal(receipt.runs.length >= 3, true);
	assert.equal(
		receipt.runs.some((run) => run.nickify_version === "11"),
		true,
	);
	assert.equal(
		receipt.runs.some((run) => run.nickify_version === "40"),
		true,
	);
	assert.equal(receipt.runs.at(-1).scope, "repo");
	assert.equal(receipt.runs.at(-1).polypowers_init_version, "1");
	assert.match(receipt.runs.at(-1).state_sha256, /^[0-9a-f]{64}$/);
});

test("polyscribe callers use the user-level hook, not a repo-local managed copy", () => {
	assert.equal(exists("scripts/polyscribe.sh"), false);

	const packageJson = readJson("package.json");
	for (const name of ["agents", "agents:system"]) {
		assert.match(packageJson.scripts[name], /\$HOME\/.claude\/hooks\/polyscribe\/polyscribe\.sh/);
		assert.doesNotMatch(packageJson.scripts[name], /scripts\/polyscribe\.sh/);
	}
	assert.equal(packageJson.scripts["agents:check"], "node ops/canonical-sx-drift-check.mjs");
});

test("agent-instruction inputs stay tracked but marker-free until hook regeneration is fixed", () => {
	const trackedInputs = [
		"agent-instructions/README.md",
		"agent-instructions/standard-set.json",
		"agent-instructions/source/30-polypowers.md",
		"agent-instructions/source/35-worktree-recipe.ref.md",
		"agent-instructions/source/40-operating-principles.md",
		"agent-instructions/source/65-agent-identity.md",
		"agent-instructions/agent-overrides/claude.md",
		"agent-instructions/agent-overrides/codex.md",
	];
	for (const path of trackedInputs) {
		assert.equal(exists(path), true, `${path} should remain available in a clean checkout`);
		assert.doesNotMatch(read(path), new RegExp(["@sx", "managed"].join("-")));
	}

	const manifest = readJson("agent-instructions/standard-set.json");
	assert.equal(
		manifest.modules.some((module) => "marker" in module),
		false,
	);
	assert.equal(
		manifest.modules.some((module) => module.path === "scripts/polyscribe.sh"),
		false,
	);
	for (const module of manifest.modules) {
		assert.equal(exists(module.path), true, `${module.path} should be present when pinned`);
		assert.equal(sha256(trimBlankRuns(stripHtmlComments(read(module.path)))), module.sha256, module.path);
	}
});
