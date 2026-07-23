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
	assert.match(src, /go test -race \.\/\.\.\./); // build-test uses -race; the gate must too
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
	assert.match(golangci, /GOLANGCI_LINT_CACHE=/);
	assert.match(golangci, /show-toplevel|\/backend\/\.cache/);
});
