// Behavioral coverage for the local Prettier format-parity gate
// (scripts/ci/format-check.sh), which mirrors the remote `format` CI job in
// .github/workflows/prettier.yml. The gate must fail on an unformatted file the
// branch changes and pass when the changed files are clean — that is the whole
// point of catching format violations locally before a wasted CI round-trip.
import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync, readFileSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(new URL("../scripts/ci/format-check.sh", import.meta.url));

function git(cwd, ...args) {
	const r = spawnSync("git", args, { cwd, encoding: "utf8" });
	if (r.status !== 0) throw new Error(`git ${args.join(" ")} failed: ${r.stderr}`);
	return r.stdout;
}

// A throwaway repo with a prettier-clean committed baseline. No remote is
// configured, so the gate's default-branch lookup falls back and the changed
// set comes from the working tree — which is exactly what we want to exercise.
function setupRepo() {
	const dir = mkdtempSync(join(tmpdir(), "fmt-gate-"));
	git(dir, "init", "-q");
	git(dir, "config", "user.email", "gate@example.com");
	git(dir, "config", "user.name", "gate");
	git(dir, "config", "commit.gpgsign", "false");
	writeFileSync(join(dir, "doc.md"), "# Title\n\nBody\n"); // prettier-clean
	git(dir, "add", "doc.md");
	git(dir, "commit", "-qm", "base");
	return dir;
}

function runGate(cwd) {
	return spawnSync("bash", [scriptPath], { cwd, encoding: "utf8" });
}

test("format-check gate fails on an unformatted tracked file", () => {
	const dir = setupRepo();
	try {
		// Multiple consecutive blank lines are collapsed by Prettier's markdown
		// formatter, so this is reliably flagged as unformatted.
		writeFileSync(join(dir, "doc.md"), "# Title\n\n\n\nBody\n");
		const r = runGate(dir);
		assert.notEqual(r.status, 0, `expected non-zero exit\nstdout:${r.stdout}\nstderr:${r.stderr}`);
		assert.match(r.stdout + r.stderr, /doc\.md/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("format-check gate passes when the changed files are formatted", () => {
	const dir = setupRepo();
	try {
		writeFileSync(join(dir, "doc.md"), "# Title\n\nMore body\n"); // still prettier-clean
		const r = runGate(dir);
		assert.equal(r.status, 0, `expected zero exit\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("format-check gate passes when nothing changed", () => {
	const dir = setupRepo();
	try {
		// No working-tree edits and no reachable base ref → empty changed set →
		// the empty-array guard makes this a clean pass, not an error.
		const r = runGate(dir);
		assert.equal(r.status, 0, `expected zero exit\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("format-check skips a changed file that a later rename removed", () => {
	const dir = setupRepo();
	try {
		// Reproduce the real shape: the file must be in the COMMITTED changed set
		// (base...HEAD) and then be renamed away uncommitted. That needs a base ref,
		// so point origin/main at the baseline commit. Without it the gate skips the
		// committed diff entirely and the missing-path case never arises.
		const base = git(dir, "rev-parse", "HEAD").trim();
		git(dir, "update-ref", "refs/remotes/origin/main", base);
		git(dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main");

		writeFileSync(join(dir, "added.md"), "# Added\n\nBody\n"); // prettier-clean
		git(dir, "add", "added.md");
		git(dir, "commit", "-qm", "add a file on the branch");

		// Now rename it away without committing: base...HEAD still lists added.md.
		git(dir, "mv", "added.md", "renamed.md");

		const r = runGate(dir);
		assert.ok(
			!/No files matching the pattern/.test(r.stdout + r.stderr),
			`gate passed a nonexistent path to Prettier\nstdout:${r.stdout}\nstderr:${r.stderr}`,
		);
		assert.equal(r.status, 0, `expected zero exit\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("format-check still checks a dangling symlink the branch changed", () => {
	const dir = setupRepo();
	try {
		// -e alone calls a dangling symlink nonexistent, but remote Prettier still
		// rejects it. Skipping it locally would break the CI parity this gate is for.
		symlinkSync("missing-target.md", join(dir, "link.md"));
		git(dir, "add", "link.md");
		const r = runGate(dir);
		assert.notEqual(r.status, 0, `expected a nonzero exit\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("format-check treats a flag-looking filename as a path, not a Prettier option", () => {
	const dir = setupRepo();
	try {
		// This is a real regression test for the `--` terminator, not just a
		// smoke test: without `--`, Prettier parses the bare `--weird.md` as an
		// unknown option, ignores it, finds no files, and exits 0 — silently
		// MISSING an unformatted file. With `--` it is checked as a path and the
		// violation is caught (non-zero). So a non-zero exit here proves the
		// terminator is present and working.
		writeFileSync(join(dir, "--weird.md"), "# Title\n\n\n\nBody\n");
		git(dir, "add", "--", "--weird.md");
		const r = runGate(dir);
		assert.notEqual(r.status, 0, `expected non-zero exit\nstdout:${r.stdout}\nstderr:${r.stderr}`);
		assert.match(r.stdout + r.stderr, /weird\.md/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("format-check fails loudly instead of false-passing when the changed-file query errors", () => {
	// A fresh repo with no commits makes `git diff HEAD` fail. The gate must
	// surface that as a non-zero exit, not swallow it into a clean pass — a
	// producer failure hidden behind a process substitution would silently skip
	// the check. (Regression guard for the temp-file + set -e capture.)
	const dir = mkdtempSync(join(tmpdir(), "fmt-gate-"));
	try {
		git(dir, "init", "-q");
		git(dir, "config", "user.email", "gate@example.com");
		git(dir, "config", "user.name", "gate");
		writeFileSync(join(dir, "bad.md"), "# Title\n\n\n\nBody\n");
		const r = runGate(dir);
		assert.notEqual(r.status, 0, `expected non-zero exit\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("format-check mirrors the prettier CI job command shape", () => {
	const src = readFileSync(scriptPath, "utf8");
	assert.match(src, /prettier@3/);
	assert.match(src, /--check/);
	assert.match(src, /--ignore-unknown/);
	assert.match(src, /--ignore-unknown -- "\$\{files\[@\]\}"/); // option terminator before the paths
});
