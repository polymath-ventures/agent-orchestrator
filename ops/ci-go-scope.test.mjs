// Behavioral coverage for the Go-stage scope predicate
// (scripts/ci/go-stages-skippable.sh), which lets the local pre-push gate skip
// the Go stages — including the ~200s `go test -race` suite — on a branch that
// cannot have changed Go behavior.
//
// The exit convention is INVERTED on purpose and these tests pin it:
//
//     exit 0        = the Go stages may be skipped
//     exit non-zero = run them
//
// The only path that reaches `exit 0` is the one that walked the entire changed
// set and positively found nothing Go-relevant. Every failure mode — an
// underivable changed set, a mktemp failure, a `set -e` abort, the script not
// existing — lands on non-zero and therefore RUNS the suite. Fail-safe is a
// structural property of the convention, not something each caller has to
// remember. `exit 0 = run` would have inverted it: any crash would silently skip
// the gate's most expensive and most load-bearing stage.
//
// No `go` binary is invoked anywhere in this file, so the whole suite is ~1s.
import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, mkdirSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(new URL("../scripts/ci/go-stages-skippable.sh", import.meta.url));

function git(cwd, ...args) {
	const r = spawnSync("git", args, { cwd, encoding: "utf8" });
	if (r.status !== 0) throw new Error(`git ${args.join(" ")} failed: ${r.stderr}`);
	return r.stdout;
}

function write(dir, rel, body) {
	const abs = join(dir, rel);
	mkdirSync(dirname(abs), { recursive: true });
	writeFileSync(abs, body);
	return abs;
}

// A throwaway repo whose committed baseline contains one file of every shape the
// predicate has to reason about.
//
// `withBase` wires refs/remotes/origin/{main,HEAD} at the baseline commit, which
// is the normal state of any real checkout. It defaults to true because a repo
// WITHOUT a base ref is itself a "cannot decide → run" condition now, so testing
// the skippable cases without one would assert against a state that can no longer
// produce a skip. The no-base case gets its own explicit test below.
function setupRepo({ withBase = true } = {}) {
	const dir = mkdtempSync(join(tmpdir(), "go-scope-"));
	git(dir, "init", "-q");
	git(dir, "config", "user.email", "gate@example.com");
	git(dir, "config", "user.name", "gate");
	git(dir, "config", "commit.gpgsign", "false");
	write(dir, "README.md", "# readme\n");
	write(dir, "backend/foo.go", "package backend\n");
	write(dir, "backend/internal/storage/sqlite/migrations/003_x.sql", "SELECT 1;\n");
	write(dir, "scripts/ci/golangci.sh", "#!/usr/bin/env bash\n");
	// Decoys: "backend" appears in the path but never as the leading segment.
	write(dir, "docs/backend-notes.md", "# notes\n");
	write(dir, "frontend/backend-client.ts", "export const x = 1;\n");
	git(dir, "add", "-A");
	git(dir, "commit", "-qm", "base");
	if (withBase) {
		const base = git(dir, "rev-parse", "HEAD").trim();
		git(dir, "update-ref", "refs/remotes/origin/main", base);
		git(dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main");
	}
	return dir;
}

function runPredicate(cwd) {
	return spawnSync("bash", [scriptPath], { cwd, encoding: "utf8" });
}

test("predicate says skippable when only a prose file changed", () => {
	const dir = setupRepo();
	try {
		write(dir, "README.md", "# readme\n\nmore\n");
		const r = runPredicate(dir);
		assert.equal(r.status, 0, `expected skippable\nstdout:${r.stdout}\nstderr:${r.stderr}`);
		assert.match(r.stdout, /no changed paths/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate says skippable when nothing changed at all", () => {
	const dir = setupRepo();
	try {
		const r = runPredicate(dir);
		assert.equal(r.status, 0, `expected skippable\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate runs the Go stages for a modified backend .go file", () => {
	// The core negative case: a predicate that skipped here would let a real
	// build break reach the remote.
	const dir = setupRepo();
	try {
		write(dir, "backend/foo.go", "package backend\n\nvar X = 1\n");
		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
		assert.match(r.stdout, /backend\/foo\.go/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate runs the Go stages for a DELETE-only backend change", () => {
	// `git diff --diff-filter=d` excludes deletions. Reusing that filter for the
	// predicate would report an empty changed set for a branch that removed a
	// package — skipping the build that would have caught it. This is why
	// changed-files.sh drops the filter and leaves existence checks to the one
	// consumer that needs them (Prettier).
	const dir = setupRepo();
	try {
		git(dir, "rm", "-q", "backend/foo.go");
		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
		assert.match(r.stdout, /backend\/foo\.go/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate runs the Go stages for a non-.go build input (embedded migration)", () => {
	// backend/**/migrations/*.sql is a //go:embed root, so it is compiled in even
	// though it is not Go source. A `*.go`-only trigger set would miss it.
	const dir = setupRepo();
	try {
		write(dir, "backend/internal/storage/sqlite/migrations/003_x.sql", "SELECT 2;\n");
		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate runs the Go stages when the gate's own scripts changed", () => {
	// A change to the gate must exercise the gate.
	const dir = setupRepo();
	try {
		write(dir, "scripts/ci/golangci.sh", "#!/usr/bin/env bash\n# tweak\n");
		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate sees backend changes that are already COMMITTED on the branch", () => {
	// The common real case: the branch's Go changes are committed and the working
	// tree is clean. A working-tree-only check (`git diff --quiet HEAD -- backend`)
	// would report "nothing changed" here and skip the suite.
	const dir = setupRepo();
	try {
		write(dir, "backend/foo.go", "package backend\n\nvar Y = 2\n");
		git(dir, "add", "backend/foo.go");
		git(dir, "commit", "-qm", "backend change on the branch");

		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
		assert.match(r.stdout, /backend\/foo\.go/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate matches on the leading path segment, not a substring", () => {
	// `docs/backend-notes.md` and `frontend/backend-client.ts` both contain
	// "backend" but neither is a Go input. A `*backend*` glob would run the full
	// race suite on a docs edit — the exact waste this change removes.
	const dir = setupRepo();
	try {
		write(dir, "docs/backend-notes.md", "# notes\n\nmore\n");
		write(dir, "frontend/backend-client.ts", "export const x = 2;\n");
		const r = runPredicate(dir);
		assert.equal(r.status, 0, `expected skippable\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate FAILS SAFE when the base ref is missing and the change is committed", () => {
	// The nastiest shape, and the one a shared changed-set helper invites: the
	// branch's backend changes are COMMITTED, so the working tree is clean, and
	// `origin/main` is absent (single-branch clone, unfetched remote, shallow
	// checkout). Deriving the changed set from the working tree alone then reports
	// nothing, and the Go stages get skipped on a branch that may not compile.
	//
	// Prettier degrades gracefully in exactly this situation on purpose — missing
	// a few committed files is a small, visible miss. A skip-the-build decision
	// cannot borrow that tolerance, which is why the predicate passes
	// --require-base and this case must RUN.
	const dir = setupRepo({ withBase: false });
	try {
		write(dir, "backend/foo.go", "package backend\n\nvar Z = 3\n");
		git(dir, "add", "backend/foo.go");
		git(dir, "commit", "-qm", "committed backend change, no base ref");
		assert.equal(git(dir, "status", "--porcelain").trim(), "", "working tree must be clean");

		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate runs the Go stages for a root go.work, the one input outside backend/", () => {
	// The toolchain searches parent directories for a workspace file, so a
	// go.work added at the repo root changes module selection for a build run
	// from backend/ — the only Go input that would not match `backend/*`.
	const dir = setupRepo();
	try {
		write(dir, "go.work", "go 1.21\n\nuse ./backend\n");
		git(dir, "add", "go.work");
		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
		assert.match(r.stdout, /go\.work/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate FAILS SAFE (runs) when the changed set cannot be determined", () => {
	// A repo with no commits makes `git diff HEAD` fail. The predicate must treat
	// an underivable changed set as "run the stages", never as "nothing changed,
	// skip". This is the case an `exit 0 = run` convention would get wrong.
	const dir = mkdtempSync(join(tmpdir(), "go-scope-"));
	try {
		git(dir, "init", "-q");
		git(dir, "config", "user.email", "gate@example.com");
		git(dir, "config", "user.name", "gate");
		write(dir, "backend/foo.go", "package backend\n");
		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate resolves its helper relative to itself, not to the inspected repo", () => {
	// It runs against throwaway repos that contain no scripts/ci/ of their own, so
	// a repo-relative lookup of changed-files.sh would break every case above —
	// and, worse, would fail-safe its way to a green-looking "run" verdict for the
	// wrong reason. Pin the sibling resolution explicitly.
	const src = readFileSync(scriptPath, "utf8");
	assert.match(src, /dirname "\$0"/);
	assert.doesNotMatch(src, /bash scripts\/ci\/changed-files\.sh/);
});
