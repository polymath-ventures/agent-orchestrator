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

test("polyscribe and instruction docs name polypowers-init as the recovery path", () => {
	const polyscribe = read("scripts/polyscribe.sh");
	assert.match(polyscribe, /polypowers\.json/);
	assert.doesNotMatch(polyscribe, /nickify\.json/);
	assert.match(polyscribe, /polypowers-init/);
	assert.doesNotMatch(polyscribe, /re-run nickify/);

	const readme = read("agent-instructions/README.md");
	assert.match(readme, /polypowers-init/);
	assert.match(readme, /polypowers\.json/);
	assert.match(readme, /\.polypowers-init\.json/);
	assert.doesNotMatch(readme, /One skill, one entrypoint \(`\/nickify`\)/);
	assert.doesNotMatch(readme, /`nickify\.json` is the source of truth/);
	assert.doesNotMatch(readme, /`\.nickified\.json` remains the receipt/);
});
