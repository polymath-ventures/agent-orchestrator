import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

const repoIgnorePath = new URL("../.gitignore", import.meta.url);

test("retired Beads residue cannot dirty an orchestrator checkout", async (t) => {
	const root = await mkdtemp(path.join(tmpdir(), "ao-beads-ignore-"));
	t.after(() => rm(root, { force: true, recursive: true }));

	execFileSync("git", ["init", "--quiet"], { cwd: root });
	execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: root });
	execFileSync("git", ["config", "user.name", "Test"], { cwd: root });
	await writeFile(path.join(root, ".gitignore"), await readFile(repoIgnorePath));
	execFileSync("git", ["add", ".gitignore"], { cwd: root });
	execFileSync("git", ["commit", "--quiet", "-m", "fixture"], { cwd: root });

	await mkdir(path.join(root, ".beads"));
	await writeFile(path.join(root, ".beads", "state.jsonl"), "local state\n");
	await writeFile(path.join(root, ".beads-credential-key"), "local key\n");
	await writeFile(path.join(root, ".beads.gate.lock"), "locked\n");

	const status = execFileSync("git", ["status", "--porcelain", "--untracked-files=all"], {
		cwd: root,
		encoding: "utf8",
	});
	assert.equal(status, "");
});
