// Coverage for the aggregated local pre-push gate (scripts/ci/ci-local.sh) and
// its wiring. The gate's whole job is CI parity, so these assertions pin that it
// mirrors every remote job and that the golangci-lint version stays locked to
// the one the CI action pins (a mismatch passes locally but fails CI, or vice
// versa — the exact failure mode issue #105 exists to prevent).
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

test("ci-local aggregator mirrors every CI parity job", () => {
	const src = read("../scripts/ci/ci-local.sh");
	assert.match(src, /format-check\.sh/); // prettier format job
	assert.match(src, /gofmt/); // go.yml format step
	assert.match(src, /go build/); // build
	assert.match(src, /go vet/); // vet
	// Anchor to the executable `run` line, not the explanatory comment, so
	// dropping -race from the actual command would fail this test.
	assert.match(src, /run "go test -race" bash -c '[^']*go test -trimpath -race \.\/\.\.\./); // build-test parity
	assert.match(src, /golangci\.sh/); // golangci-lint (pinned, single-sourced)
	assert.match(src, /frontend:typecheck/); // frontend typecheck
});

test("ci-local runs the format check before the slower go steps (fail cheap first)", () => {
	const src = read("../scripts/ci/ci-local.sh");
	assert.ok(
		src.indexOf("format-check.sh") < src.indexOf("go build"),
		"format check should run before go build so a cheap miss fails fast",
	);
});

test("ci-local checks for its toolchain before running", () => {
	const src = read("../scripts/ci/ci-local.sh");
	// Everything the gate shells out to must be present up front, so a missing
	// tool fails with a clear message instead of a confusing mid-gate error.
	for (const tool of ["git", "go", "node", "npm", "npx"]) {
		assert.match(src, new RegExp(`need ${tool}\\b`));
	}
});

test("ci-local scopes the Go stages to the diff, and only the Go stages", () => {
	// The gate's expensive half is Go-only, so it hangs off the scope predicate;
	// everything else stays unconditional. Getting this boundary wrong in either
	// direction is a real failure: skipping too much lets a break through, and
	// skipping too little is the waste this exists to remove.
	const src = read("../scripts/ci/ci-local.sh");
	const scopedAt = src.indexOf("go-stages-skippable.sh");
	assert.ok(scopedAt > 0, "the gate should consult the Go-stage scope predicate");
	const closedAt = src.indexOf("\nfi\n", scopedAt);
	assert.ok(closedAt > scopedAt, "the scoped block should be closed with a bare `fi`");

	for (const stage of ["gofmt", "go build", "go vet", "go test -race", "golangci.sh"]) {
		const at = src.indexOf(stage, scopedAt);
		assert.ok(at > scopedAt && at < closedAt, `${stage} should sit inside the scoped block`);
	}
	// Prettier is already changed-files-scoped on its own and runs first so a
	// cheap miss fails fast; the frontend typecheck is not a Go stage.
	assert.ok(src.indexOf("format-check.sh") < scopedAt, "prettier stays unconditional and first");
	assert.ok(src.indexOf("frontend:typecheck") > closedAt, "frontend typecheck stays unconditional");

	// The skip must be visible in the output, never silent — a silently skipped
	// race suite is indistinguishable from a passing one.
	assert.match(src, /go stages skipped/);
});

test("ci-local passes -trimpath so worktrees share cache entries for their own packages", () => {
	// Without -trimpath the absolute source path is baked into compiled output, so
	// each worktree gets its own copy of all 113 first-party packages. Measured: an
	// identical module built from a second directory adds 12 cache entries without
	// the flag and 3 with it.
	const src = read("../scripts/ci/ci-local.sh");
	assert.match(src, /go build -trimpath \.\/\.\.\./);
	assert.match(src, /go vet -trimpath \.\/\.\.\./);
	assert.match(src, /go test -trimpath -race \.\/\.\.\./);

	// Per-command, deliberately not an exported GOFLAGS: that would propagate into
	// golangci.sh, which `npm run lint` shares and whose per-worktree cache exists
	// specifically to fix a stale absolute-path bug (#105). Perturbing a
	// path-rewriting flag through it invites that bug back, and would make the
	// gate's invocation differ from the one `npm run lint` makes.
	// Anchor to an assignment, so prose about GOFLAGS does not satisfy the check
	// while `export GOFLAGS=…` or an inline `GOFLAGS=… go build` would trip it.
	assert.doesNotMatch(src, /GOFLAGS=/);
	assert.doesNotMatch(read("../scripts/ci/golangci.sh"), /trimpath/);
});

test("the changed-set derivation lives in exactly one place", () => {
	// format-check.sh and the Go-stage predicate must never disagree about what
	// the branch changed, so neither derives it itself.
	const shared = read("../scripts/ci/changed-files.sh");
	assert.match(shared, /git diff --name-only -z/);
	for (const consumer of ["format-check.sh", "go-stages-skippable.sh"]) {
		const src = read(`../scripts/ci/${consumer}`);
		// The derivation itself, not prose mentioning `git diff` — both consumers
		// carry comments explaining why the shared helper is invoked the way it is.
		assert.doesNotMatch(
			src,
			/git diff --name-only/,
			`${consumer} should consume changed-files.sh, not re-derive the set`,
		);
		assert.match(src, /changed-files\.sh/);
	}
	// Deletions must survive into the shared set: a branch that only removes a
	// backend package still has to be compiled. Anchored to the invocation, since
	// the file's own comment explains why the filter was dropped. The behavioral
	// proof is the delete-only case in ops/ci-go-scope.test.mjs; this just keeps
	// the filter from being reinstated in a passing-looking way.
	assert.doesNotMatch(shared, /git diff --name-only -z --diff-filter/);
});

test("package.json wires the gate scripts", () => {
	const pkg = JSON.parse(read("../package.json"));
	assert.equal(pkg.scripts["format:check"], "bash scripts/ci/format-check.sh");
	assert.equal(pkg.scripts["ci-local"], "bash scripts/ci/ci-local.sh");
	assert.match(pkg.scripts["hooks:install"], /core\.hooksPath .githooks/);
	// lint shares the single golangci source with the gate.
	assert.match(pkg.scripts.lint, /scripts\/ci\/golangci\.sh/);
});

test("the optional pre-push hook runs the aggregator", () => {
	const hook = read("../.githooks/pre-push");
	assert.match(hook, /npm run ci-local/);
});

test("local golangci-lint pin matches the CI action pin", () => {
	const golangci = read("../scripts/ci/golangci.sh");
	const wf = read("../.github/workflows/go.yml");
	const ciVersion = wf.match(/version:\s*(v2\.\d+\.\d+)/)?.[1];
	assert.ok(ciVersion, "could not find a golangci-lint version in go.yml");
	const pinned = ciVersion.replace(/\./g, "\\.");
	assert.match(golangci, new RegExp(`golangci-lint@${pinned}`));
});

test("golangci.sh isolates the cache per-worktree to avoid stale sibling-worktree noise", () => {
	// A shared user-level golangci cache gets poisoned by absolute paths from
	// sibling worktrees, which leak stale gen/*.sql.go findings CI never sees.
	// Pinning the cache under the worktree makes that cross-worktree collision
	// impossible by construction.
	const golangci = read("../scripts/ci/golangci.sh");
	assert.match(golangci, /GOLANGCI_LINT_CACHE=/); // cache is pinned...
	assert.match(golangci, /show-toplevel/); // ...to a worktree-derived root...
	assert.match(golangci, /\/backend\/\.cache/); // ...under this worktree only.
});
