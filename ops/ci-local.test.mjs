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
	assert.match(src, /frontend:test/); // frontend vitest unit suite (frontend.yml "Run vitest suite")
	assert.match(src, /test:ops/); // ops-units job (go.yml + frontend.yml)
});

test("frontend CI prepares the pinned browser runtime before vitest", () => {
	const workflow = read("../.github/workflows/frontend.yml");
	const prepareAt = workflow.indexOf("browser-runtime:prepare");
	const vitestAt = workflow.indexOf("npx vitest run");
	assert.ok(prepareAt > 0, "frontend CI should prepare the pinned browser runtime");
	assert.ok(vitestAt > prepareAt, "runtime preparation should precede the vitest suite");
});

test("ci-local runs the ops unit tests, unconditionally", () => {
	// go.yml's `ops-units` job and frontend.yml both run `npm run test:ops`; the
	// gate claimed parity with every CI job while mirroring neither. It matters
	// here specifically: the coverage for the Go-stage scoping lives in
	// ops/*.test.mjs, so without this the gate could not verify the change that
	// teaches it to skip work.
	const src = read("../scripts/ci/ci-local.sh");
	const at = src.indexOf("npm run test:ops");
	assert.ok(at > 0, "the gate should run the ops unit tests");
	// Node-only and seconds long, so it stays outside the Go-scoped block — its
	// inputs are the workflow and script files the Go predicate ignores.
	const scopedAt = src.indexOf("go-stages-skippable.sh");
	assert.ok(at < scopedAt, "ops tests are cheap; run them before the Go stages");
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
	// Anchor to the executable lines, never to prose: comments above the block
	// name both the predicate and the stages, and matching those would let this
	// pass while the real structure was wrong.
	const scopedAt = src.indexOf('if [ "$skip_go" = 1 ]; then');
	assert.ok(scopedAt > 0, "the gate should branch on the scope decision");
	const closedAt = src.indexOf("\nfi\n", scopedAt);
	assert.ok(closedAt > scopedAt, "the scoped block should be closed with a bare `fi`");
	assert.ok(
		src.indexOf("bash scripts/ci/go-stages-skippable.sh") > 0,
		"the decision should come from the predicate script",
	);

	for (const stage of ["gofmt", "go build", "go vet", "go test -race", "golangci.sh"]) {
		const at = src.indexOf(stage, scopedAt);
		assert.ok(at > scopedAt && at < closedAt, `${stage} should sit inside the scoped block`);
	}
	// Prettier is already changed-files-scoped on its own and runs first so a
	// cheap miss fails fast; the frontend typecheck and vitest are not Go stages.
	assert.ok(src.indexOf("format-check.sh") < scopedAt, "prettier stays unconditional and first");
	assert.ok(src.indexOf("frontend:typecheck") > closedAt, "frontend typecheck stays unconditional");
	assert.ok(src.indexOf("frontend:test") > closedAt, "frontend vitest stays unconditional");

	// The skip must be visible in the output, never silent — a silently skipped
	// race suite is indistinguishable from a passing one.
	assert.match(src, /go stages skipped/);
});

test("ci-local runs the frontend vitest suite, unconditionally, after the typecheck", () => {
	// frontend.yml runs the renderer vitest suite ("Run vitest suite": `npx vitest
	// run`) on top of the typecheck. The gate mirrored only the typecheck, so a
	// frontend unit regression (the #220 SessionView.test.tsx failures) passed the
	// local gate and only broke on remote CI — the wasted round-trip this gate
	// exists to prevent. It runs unconditionally like the typecheck (vitest is
	// fast; ao-7ra's decision default prefers that over fragile frontend scoping)
	// and sits after the typecheck so a cheaper type error fails first.
	const src = read("../scripts/ci/ci-local.sh");
	const typecheckAt = src.indexOf("frontend:typecheck");
	const vitestAt = src.indexOf("frontend:test");
	assert.ok(vitestAt > 0, "the gate should run the frontend vitest suite");
	assert.ok(vitestAt > typecheckAt, "vitest runs after the cheaper typecheck");
	// Outside the Go-scoped block, so it always runs.
	const closedAt = src.indexOf("\nfi\n", src.indexOf('if [ "$skip_go" = 1 ]; then'));
	assert.ok(vitestAt > closedAt, "vitest is unconditional, not gated on the Go scope");
	// Anchor to the WHOLE executable line, not just the token: the gate must fail
	// closed on a vitest failure, so a later `|| true` or `|| :` that swallowed
	// the exit code (defeating the whole point) has to trip this test.
	assert.match(src, /\nrun "frontend vitest" npm run frontend:test\n/);
	// And the delegated script must actually be the non-watch vitest run — a
	// `vitest` watch invocation would hang the gate instead of gating it.
	const fe = JSON.parse(read("../frontend/package.json"));
	assert.match(fe.scripts.test, /^vitest run\b/);
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

test("the Go-stage predicate is self-contained and leaves the Prettier gate alone", () => {
	// An earlier draft of this change factored the changed-set derivation into a
	// script shared by both scoped stages. It was a net loss: the two consumers
	// want OPPOSITE policies on deletions (Prettier cannot check a path that no
	// longer exists; the Go stages must compile a branch that removed a package)
	// and OPPOSITE policies on a missing base ref (Prettier degrades to the
	// working tree; the Go stages must not). Both defects review found came from
	// that coupling, so the predicate answers its own question and format-check.sh
	// is untouched by this change.
	const predicate = read("../scripts/ci/go-stages-skippable.sh");
	assert.doesNotMatch(predicate, /changed-files\.sh/);
	assert.match(predicate, /go_paths=\(/);

	// format-check.sh keeps its own derivation, --diff-filter=d included: correct
	// for Prettier, wrong for the Go stages.
	const fmt = read("../scripts/ci/format-check.sh");
	assert.match(fmt, /--diff-filter=d/);
	assert.doesNotMatch(fmt, /go-stages-skippable|changed-files/);
});

test("package.json wires the gate scripts", () => {
	const pkg = JSON.parse(read("../package.json"));
	assert.equal(pkg.scripts["format:check"], "bash scripts/ci/format-check.sh");
	assert.equal(pkg.scripts["ci-local"], "bash scripts/ci/ci-local.sh");
	// The gate calls `npm run frontend:test`; keep it single-sourced here, the
	// same way frontend:typecheck delegates into the frontend workspace.
	assert.equal(pkg.scripts["frontend:test"], "npm --prefix frontend run test");
	assert.match(pkg.scripts["hooks:install"], /core\.hooksPath .githooks/);
	// lint shares the single golangci source with the gate.
	assert.match(pkg.scripts.lint, /scripts\/ci\/golangci\.sh/);
});

test("the optional pre-push hook runs the aggregator", () => {
	const hook = read("../.githooks/pre-push");
	assert.match(hook, /npm run ci-local/);
});

test("ci-local honours the hook's force signal for a push that is not HEAD", () => {
	// The predicate reasons about the checked-out HEAD, but git will push a ref
	// you are not standing on. The hook detects that and sets CI_LOCAL_FORCE_GO;
	// the gate has to actually consult it, or the scoping silently evaluates the
	// wrong commit. Behaviour is pinned in ops/ci-pre-push-hook.test.mjs.
	const src = read("../scripts/ci/ci-local.sh");
	// Executable lines only — a comment naming CI_LOCAL_FORCE_GO sits above both.
	const forceAt = src.indexOf('if [ -n "${CI_LOCAL_FORCE_GO:-}" ]; then');
	assert.ok(forceAt > 0, "the gate should test the force signal");
	// It must be tested BEFORE the predicate runs, or a skippable verdict wins.
	assert.ok(forceAt < src.indexOf("bash scripts/ci/go-stages-skippable.sh"), "force must short-circuit the predicate");
	assert.match(read("../.githooks/pre-push"), /CI_LOCAL_FORCE_GO=1/);
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
