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
	assert.match(src, /npm run lint/); // go test + golangci-lint (pinned)
	assert.match(src, /frontend:typecheck/); // frontend typecheck
});

test("ci-local checks for its toolchain before running", () => {
	const src = read("../scripts/ci/ci-local.sh");
	assert.match(src, /need go/);
	assert.match(src, /need node/);
});

test("package.json wires the gate scripts", () => {
	const pkg = JSON.parse(read("../package.json"));
	assert.equal(pkg.scripts["format:check"], "bash scripts/ci/format-check.sh");
	assert.equal(pkg.scripts["ci-local"], "bash scripts/ci/ci-local.sh");
	assert.match(pkg.scripts["hooks:install"], /core\.hooksPath .githooks/);
});

test("the optional pre-push hook runs the aggregator", () => {
	const hook = read("../.githooks/pre-push");
	assert.match(hook, /npm run ci-local/);
});

test("local golangci-lint pin matches the CI action pin", () => {
	const pkg = JSON.parse(read("../package.json"));
	const wf = read("../.github/workflows/go.yml");
	const ciVersion = wf.match(/version:\s*(v2\.\d+\.\d+)/)?.[1];
	assert.ok(ciVersion, "could not find a golangci-lint version in go.yml");
	const pinned = ciVersion.replace(/\./g, "\\.");
	assert.match(pkg.scripts.lint, new RegExp(`golangci-lint@${pinned}`));
});

test("lint isolates the golangci cache per-worktree to avoid stale sibling-worktree noise", () => {
	// A shared user-level golangci cache gets poisoned by absolute paths from
	// sibling worktrees, which leak stale gen/*.sql.go findings CI never sees.
	// Pinning the cache under the worktree makes that cross-worktree collision
	// impossible by construction.
	const pkg = JSON.parse(read("../package.json"));
	assert.match(pkg.scripts.lint, /GOLANGCI_LINT_CACHE=/);
	assert.match(pkg.scripts.lint, /\$PWD/);
});
