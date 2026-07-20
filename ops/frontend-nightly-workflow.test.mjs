import assert from "node:assert/strict";
import { execFile as execFileCallback } from "node:child_process";
import { copyFile, mkdir, mkdtemp, rm, writeFile, readFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { afterEach, describe, it } from "node:test";

const execFile = promisify(execFileCallback);
const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const cleanup = [];

afterEach(async () => {
	await Promise.all(
		cleanup
			.splice(0)
			.reverse()
			.map((item) => item()),
	);
});

describe("Desktop nightly workflow", () => {
	it("falls back to desktop-v0.0.0 when no stable desktop tags exist", async () => {
		const repo = await makeGitRepo();
		const outputPath = path.join(repo, "github-output.txt");
		const script = await nightlyVersionStepScript();

		await execFile("bash", ["--noprofile", "--norc", "-e", "-o", "pipefail", "-c", script], {
			cwd: repo,
			env: { ...process.env, GITHUB_OUTPUT: outputPath },
		});

		const output = await readFile(outputPath, "utf8");
		assert.match(output, /^version=0\.0\.1-nightly\.\d{12}\+[0-9a-f]+\n$/);
	});
});

async function nightlyVersionStepScript() {
	const workflow = await readFile(path.join(REPO_ROOT, ".github/workflows/frontend-nightly.yml"), "utf8");
	const stepStart = workflow.indexOf("      - id: version\n");
	assert.notEqual(stepStart, -1, "frontend-nightly.yml should contain an id: version step");

	const runStart = workflow.indexOf("        run: |\n", stepStart);
	assert.notEqual(runStart, -1, "id: version step should contain a run block");

	const nextSection = workflow.indexOf("\n\n  release:", runStart);
	assert.notEqual(nextSection, -1, "id: version run block should end before the release job");

	return workflow
		.slice(runStart + "        run: |\n".length, nextSection)
		.split("\n")
		.map((line) => line.replace(/^          /, ""))
		.join("\n");
}

async function makeGitRepo() {
	const repo = await mkdtemp(path.join(os.tmpdir(), "ao-nightly-workflow-"));
	cleanup.push(() => rm(repo, { recursive: true, force: true }));

	await execFile("git", ["init"], { cwd: repo });
	await execFile("git", ["config", "user.email", "test@example.com"], { cwd: repo });
	await execFile("git", ["config", "user.name", "Test"], { cwd: repo });
	await writeFile(path.join(repo, "README.md"), "nightly workflow fixture\n");
	await execFile("git", ["add", "README.md"], { cwd: repo });
	await execFile("git", ["commit", "-m", "fixture"], { cwd: repo });

	await mkdir(path.join(repo, "frontend/scripts"), { recursive: true });
	await copyFile(
		path.join(REPO_ROOT, "frontend/scripts/nightly-version.mjs"),
		path.join(repo, "frontend/scripts/nightly-version.mjs"),
	);

	return repo;
}
