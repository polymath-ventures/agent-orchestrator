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
	// A branch that only REMOVES a package still has to compile. Prettier's gate
	// filters deletions out with `--diff-filter=d`, correctly — it cannot check a
	// path that no longer exists — and an early draft of this predicate borrowed
	// that filter, which made a delete-only branch look unchanged and skipped the
	// build that would have caught it. Plain pathspecs report deletions.
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

test("predicate refuses to GUESS the default branch when origin/HEAD is unset", () => {
	// origin/HEAD is a local convenience ref that fetch does not maintain, so it
	// is often absent. An earlier draft fell back to a hardcoded
	// refs/remotes/origin/main. On a repo whose default is `master`, a stale
	// origin/main then silently became the comparison point — and if it sat at the
	// branch tip, a committed backend change diffed clean and the build was
	// skipped. Same class as treating a missing base ref as an empty diff: an
	// undecidable base being treated as decidable.
	const dir = setupRepo({ withBase: false });
	try {
		write(dir, "backend/foo.go", "package backend\n\nvar Guessed = 1\n");
		git(dir, "add", "backend/foo.go");
		git(dir, "commit", "-qm", "committed backend change");
		// A stale origin/main at the tip — the decoy the old fallback would pick.
		git(dir, "update-ref", "refs/remotes/origin/main", git(dir, "rev-parse", "HEAD").trim());
		// ...but origin/HEAD is deliberately NOT set, so the default is unknown.
		const sym = spawnSync("git", ["symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"], {
			cwd: dir,
			encoding: "utf8",
		});
		assert.notEqual(sym.status, 0, "origin/HEAD must be unset for this test to mean anything");

		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
		// The message has to tell the user how to restore the optimization, or the
		// safe behaviour just looks like the gate ignoring its own scoping.
		assert.match(r.stdout, /git remote set-head/);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate is immune to a local branch that shadows the remote base ref", () => {
	// `origin/main` is ambiguous: git searches refs/heads/ before refs/remotes/,
	// so a local branch literally named `origin/main` wins. Git resolves it with
	// only a warning on stderr and exit 0 — so using the short form silently
	// diffed against the wrong commit. With that branch pointing at HEAD both
	// diffs come back empty and a committed backend change is skipped.
	// Using the fully qualified refs/remotes/origin/... makes this unrepresentable.
	const dir = setupRepo();
	try {
		write(dir, "backend/foo.go", "package backend\n\nvar Shadowed = 1\n");
		git(dir, "add", "backend/foo.go");
		git(dir, "commit", "-qm", "committed backend change");
		// The decoy: a local branch named origin/main, at HEAD rather than at base.
		git(dir, "update-ref", "refs/heads/origin/main", git(dir, "rev-parse", "HEAD").trim());

		// Precondition: the short name really does resolve to the local branch, or
		// this test would pass without exercising the ambiguity at all.
		const short = git(dir, "rev-parse", "origin/main").trim();
		const local = git(dir, "rev-parse", "refs/heads/origin/main").trim();
		const remote = git(dir, "rev-parse", "refs/remotes/origin/main").trim();
		assert.notEqual(local, remote, "the decoy must differ from the real base");
		assert.equal(short, local, "short form must resolve to the shadowing local branch");

		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

test("predicate FAILS SAFE when the base ref EXISTS but git cannot diff against it", () => {
	// The case that a passing test suite hid. An earlier draft grouped both diffs
	// into one `changed="$( git diff …; git diff … )"`. `set -e` does not abort on
	// a non-final failure inside a command substitution, and the substitution
	// takes its LAST command's status — so a first diff that died with
	// `fatal: … no merge base` was swallowed, the result read as empty, and the
	// Go stages were SKIPPED on a branch adding backend code.
	//
	// Every other fail-safe test here exits at the earlier missing-base check, so
	// none of them reach the diffs at all. This one needs a base ref that EXISTS
	// and is unusable: an unrelated root commit.
	const dir = setupRepo({ withBase: false });
	try {
		// `git init` picks the initial branch name from init.defaultBranch, which
		// varies by git version and user config — capture it rather than assume.
		const branch = git(dir, "branch", "--show-current").trim();
		git(dir, "checkout", "-q", "--orphan", "otherroot");
		git(dir, "rm", "-rq", "--cached", ".");
		write(dir, "unrelated.md", "# unrelated\n");
		git(dir, "add", "unrelated.md");
		git(dir, "commit", "-qm", "unrelated root");
		const unrelated = git(dir, "rev-parse", "HEAD").trim();
		git(dir, "update-ref", "refs/remotes/origin/main", unrelated);
		git(dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main");
		git(dir, "checkout", "-q", "-f", branch);

		// Precondition: the ref resolves, so the missing-base guard does NOT fire,
		// but no merge base exists — otherwise this test would pass for the wrong
		// reason and go on hiding the defect it exists to catch.
		assert.ok(git(dir, "rev-parse", "--verify", "origin/main").trim(), "base ref must exist");
		const mb = spawnSync("git", ["merge-base", "origin/main", "HEAD"], { cwd: dir, encoding: "utf8" });
		assert.notEqual(mb.status, 0, "histories must be unrelated for this test to mean anything");

		const r = runPredicate(dir);
		assert.notEqual(r.status, 0, `expected run\nstdout:${r.stdout}\nstderr:${r.stderr}`);
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

test("predicate asks git what changed rather than string-matching paths itself", () => {
	// The pathspec form is why the cases above hold without any parsing: git owns
	// the definition of "under backend/", so deletions, renames, and names
	// containing spaces or newlines need no special handling. A `case`/glob over
	// `git diff --name-only -z` output would have to re-answer all of that, and
	// each of those is a way to under-report a change and skip the build.
	const src = readFileSync(scriptPath, "utf8");
	assert.match(src, /go_paths=\(backend scripts\/ci go\.work go\.work\.sum\)/);
	assert.match(src, /git diff --name-only "\$\{base\}\.\.\.HEAD" -- "\$\{go_paths\[@\]\}"/);
	assert.match(src, /git diff --name-only HEAD -- "\$\{go_paths\[@\]\}"/);
	// No `--diff-filter`: it would hide deletions, and a branch that only removes
	// a backend package still has to compile.
	assert.doesNotMatch(src, /--diff-filter/);
});
