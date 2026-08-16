import test from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
const read = (rel) => readFileSync(fileURLToPath(new URL(`../${rel}`, import.meta.url)), "utf8");
const readJson = (rel) => JSON.parse(read(rel));
const exists = (rel) => existsSync(fileURLToPath(new URL(`../${rel}`, import.meta.url)));

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
	for (const name of ["agents", "agents:check", "agents:system"]) {
		assert.match(packageJson.scripts[name], /\$HOME\/.claude\/hooks\/polyscribe\/polyscribe\.sh/);
		assert.doesNotMatch(packageJson.scripts[name], /scripts\/polyscribe\.sh/);
	}
});
