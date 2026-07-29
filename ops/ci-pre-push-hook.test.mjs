// Behavioral coverage for .githooks/pre-push.
//
// The gate's Go stages are scoped to the checked-out HEAD. Git, however, will
// happily push a ref you are not standing on — `git push origin backend-branch`
// from a docs-only branch. Before the stages were scoped that did not matter,
// because they ran unconditionally; now a hook that ignores the ref tuples git
// feeds it on stdin would evaluate the wrong commit, skip the Go stages, and let
// an unbuilt backend commit reach the remote.
//
// `npm` is stubbed on PATH so these assert the hook's DECISION without running
// the real multi-minute gate.
import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, mkdirSync, rmSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const hookPath = fileURLToPath(new URL("../.githooks/pre-push", import.meta.url));
const ZERO = "0".repeat(40);

// A stub `npm` that reports what the hook decided, instead of running the gate.
function stubDir() {
	const dir = mkdtempSync(join(tmpdir(), "prepush-"));
	const npm = join(dir, "npm");
	writeFileSync(npm, `#!/usr/bin/env bash\necho "FORCE=[\${CI_LOCAL_FORCE_GO:-}]"\n`);
	chmodSync(npm, 0o755);
	return dir;
}

// The hook runs `git rev-parse HEAD`, so it needs a real repo to stand in.
function repoWithHead() {
	const dir = mkdtempSync(join(tmpdir(), "prepush-repo-"));
	const git = (...args) => {
		const r = spawnSync("git", args, { cwd: dir, encoding: "utf8" });
		if (r.status !== 0) throw new Error(`git ${args.join(" ")} failed: ${r.stderr}`);
		return r.stdout;
	};
	git("init", "-q");
	git("config", "user.email", "gate@example.com");
	git("config", "user.name", "gate");
	git("config", "commit.gpgsign", "false");
	mkdirSync(join(dir, "sub"), { recursive: true });
	writeFileSync(join(dir, "sub", "f.txt"), "x\n");
	git("add", "-A");
	git("commit", "-qm", "base");
	return { dir, head: git("rev-parse", "HEAD").trim() };
}

function runHook(repo, stdin, stubs) {
	return spawnSync("bash", [hookPath], {
		cwd: repo,
		input: stdin,
		encoding: "utf8",
		env: { ...process.env, PATH: `${stubs}:${process.env.PATH}`, CI_LOCAL_FORCE_GO: "" },
	});
}

test("pre-push does NOT force the Go stages when pushing the checked-out HEAD", () => {
	const { dir, head } = repoWithHead();
	const stubs = stubDir();
	try {
		// The ordinary case: scoping is allowed to do its job.
		const r = runHook(dir, `refs/heads/main ${head} refs/heads/main ${ZERO}\n`, stubs);
		assert.equal(r.status, 0, r.stderr);
		assert.match(r.stdout, /FORCE=\[\]/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
		rmSync(stubs, { recursive: true, force: true });
	}
});

test("pre-push FORCES the Go stages when pushing a ref that is not HEAD", () => {
	const { dir, head } = repoWithHead();
	const stubs = stubDir();
	try {
		// The regression: standing on a docs-only branch while pushing a backend
		// branch. Scoping by HEAD would skip the build for the commit being pushed.
		const other = "b".repeat(40);
		assert.notEqual(other, head);
		const r = runHook(dir, `refs/heads/backend-work ${other} refs/heads/backend-work ${ZERO}\n`, stubs);
		assert.equal(r.status, 0, r.stderr);
		assert.match(r.stdout, /FORCE=\[1\]/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
		rmSync(stubs, { recursive: true, force: true });
	}
});

test("pre-push forces on a MULTI-ref push where only one ref is HEAD", () => {
	const { dir, head } = repoWithHead();
	const stubs = stubDir();
	try {
		// `git push --all` and `git push origin a b` send several lines. One
		// non-HEAD ref anywhere in the set is enough to make HEAD-scoping wrong.
		const other = "c".repeat(40);
		const stdin =
			`refs/heads/main ${head} refs/heads/main ${ZERO}\n` + `refs/heads/other ${other} refs/heads/other ${ZERO}\n`;
		const r = runHook(dir, stdin, stubs);
		assert.equal(r.status, 0, r.stderr);
		assert.match(r.stdout, /FORCE=\[1\]/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
		rmSync(stubs, { recursive: true, force: true });
	}
});

test("pre-push ignores a branch DELETION, which builds nothing", () => {
	const { dir, head } = repoWithHead();
	const stubs = stubDir();
	try {
		// A deletion pushes the all-zero SHA. Treating it as a non-HEAD ref would
		// force the full gate on every `git push --delete`, for no benefit.
		const stdin = `refs/heads/main ${head} refs/heads/main ${ZERO}\n` + `(delete) ${ZERO} refs/heads/gone ${ZERO}\n`;
		const r = runHook(dir, stdin, stubs);
		assert.equal(r.status, 0, r.stderr);
		assert.match(r.stdout, /FORCE=\[\]/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
		rmSync(stubs, { recursive: true, force: true });
	}
});

test("pre-push FORCES on an unparseable local SHA rather than ignoring the line", () => {
	const { dir } = repoWithHead();
	const stubs = stubDir();
	try {
		// Git should never emit this. The point is narrow and worth stating exactly:
		// only the all-zero deletion marker is skipped, so any other value that is
		// not HEAD — including a blank or unparseable one — lands on "force". The
		// hook does not otherwise validate the tuple, and does not need to.
		const r = runHook(dir, "refs/heads/weird not-a-sha refs/heads/weird\n", stubs);
		assert.equal(r.status, 0, r.stderr);
		assert.match(r.stdout, /FORCE=\[1\]/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
		rmSync(stubs, { recursive: true, force: true });
	}
});

test("pre-push still runs the gate when git feeds it no refs at all", () => {
	const { dir } = repoWithHead();
	const stubs = stubDir();
	try {
		// Empty stdin (nothing to push, or a git that supplies none): the hook must
		// still hand off to the gate rather than exiting early on the read loop.
		const r = runHook(dir, "", stubs);
		assert.equal(r.status, 0, r.stderr);
		assert.match(r.stdout, /FORCE=\[\]/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
		rmSync(stubs, { recursive: true, force: true });
	}
});
