# Local CI And Agent CI State

This repo has two local CI entrypoints:

```bash
npm run ci-local
npm run agent-ci
```

`npm run ci-local` is the required pre-push parity gate. It mirrors the remote
format/ops-units/lint/build/test/typecheck jobs and the frontend renderer vitest
suite, and does not use `@redwoodjs/agent-ci`.

Its five Go stages (`gofmt`, `go build`, `go vet`, `go test -race ./...`,
golangci-lint) are scoped to the diff by `scripts/ci/go-stages-skippable.sh`:
they are skipped **only** when the predicate positively establishes that the
branch touches none of `backend/`, `scripts/ci/`, a root `go.work`, or a root
`go.work.sum`. Anything else runs them, with the reason printed either way. That
trigger set is a superset of the Go build's real inputs, all of which live under
`backend/` except a workspace file, which the toolchain searches parent
directories for.

The predicate exits `0` for "skippable" and non-zero for everything else,
including its own failures. The asymmetry is the point: an unchanged branch whose
base ref is missing, ambiguous, unusable, or whose default branch is unknown
(`refs/remotes/origin/HEAD` unset — fix with `git remote set-head origin -a`)
**runs** the stages rather than being skipped, because a skipped race suite and a
passing one look identical in the output. "Otherwise skipped" would be the wrong
summary: the skip requires a positive determination, never merely the absence of
a detected change.

"Touches" means committed on the branch, staged, or edited in the tracked working
tree. Untracked files and paths marked `assume-unchanged`/`skip-worktree` are
invisible to it, exactly as they are to `git diff` — none of them can reach a
commit without becoming visible, so none can escape to the remote unbuilt.

**Accepted limitation — the hook gates the working tree, not the pushed ref.**
The Go stages compile whatever is checked out, so `git push origin other-branch`
while standing somewhere else has never verified the commit being pushed, before
this scoping or after it. The `pre-push` hook does read the ref tuples git gives
it and forces the full gate whenever a pushed SHA is not `HEAD`, which keeps that
case no worse than it was — but forcing runs the stages against `HEAD`, not
against the pushed commit. Closing that properly means checking out each pushed
SHA or refusing non-`HEAD` pushes, which is a separate change. Remote CI is the
gate that actually sees every pushed ref.

**Accepted limitation:** the predicate trusts local remote-tracking metadata. If
`origin/HEAD` is set but points somewhere wrong, or `origin/main` is stale, the
comparison happens against that stale point and a genuine change can look like no
change. Detecting it would need to contact the remote, which is precisely what a
fast local gate must not do. `git fetch` and `git remote set-head origin -a` are
the fix; remote CI is the backstop.

Worth keeping in proportion: remote CI is unscoped, so the blast radius of a
false skip here is a wasted remote round-trip and a lost local early warning, not
a broken default branch.

The predicate asks git directly, via pathspecs on the merge-base diff with the
default branch and on the tracked working-tree edits. Letting git decide what
"under `backend/`" means is what keeps it honest: deletions, renames, and paths
containing spaces or newlines need no special handling, and a branch that only
_removes_ a backend package is still correctly seen as changing Go.

It deliberately does **not** share a changed-set helper with the Prettier stage,
though an earlier draft did. The two want opposite policies on the same two
questions, and the coupling produced a defect on each:

- **Deletions.** Prettier cannot check a path that no longer exists, so it filters
  deletions out; the Go stages must compile a branch that deleted a package.
- **A missing base ref.** Prettier degrades to the working tree alone, so the
  format gate still works in an offline or single-branch clone — missing a few
  committed files there is a small, visible miss. The Go predicate must not
  degrade: a branch whose backend changes are already committed has a clean
  working tree, so that fallback would report nothing and skip the build on code
  that may not compile. It treats a missing base ref as undecidable and runs.

Remote CI is deliberately unscoped — it is the real gate, it runs on fresh
machines, and it is not what grows a developer's `~/.cache/go-build`. The local
build/vet/test invocations also pass `-trimpath`, which stops absolute source
paths from being baked into compiled output and keeps each live worktree from
caching its own copy of every first-party package.

## Frontend stages

After the Go block the gate runs two frontend stages, both unconditional (they
are Node-only and fast, and neither hangs off the Go scope predicate):

1. `npm run frontend:typecheck` — the frontend `tsc --noEmit`.
2. `npm run frontend:test` — the renderer **vitest** unit suite (`vitest run`),
   mirroring frontend.yml's "Run vitest suite" step.

The vitest stage was added after the GH #220 upstream sync: an
inspector-default-state regression passed `ci-local` (which typechecked the
frontend but never ran its unit tests) and only broke on remote CI — six
`SessionView.test.tsx` failures plus browser-mode e2e specs — exactly the wasted
round-trip this gate exists to prevent. It runs **unconditionally** rather than
behind frontend-touched scoping: vitest is fast, and running it always is
simpler and less fragile than another scope predicate (the diff-scoping added
value only for the minutes-long Go race suite). It sits **after** the typecheck
so a cheaper type error fails first.

### Browser-mode e2e is a manual opt-in step, not part of the gate

The browser-mode Playwright e2e (`frontend/e2e`, `npm run test:e2e`) is
**deliberately excluded** from `ci-local`. Unlike vitest it is expensive and
host-sensitive:

- It builds the production web bundle (`npm run build:web`) and starts a
  same-origin server per run (`frontend/playwright.config.ts`, 180s timeout).
- It needs a Playwright browser download (`npx playwright install --with-deps
chromium`).
- Its port is hardcoded to `5173` in `frontend/playwright.config.ts`
  (`baseURL`, `AO_WEB_PUBLIC_URL`, and `webServer.port`), which is frequently
  taken on shared hosts. There is no port override today; free the port first,
  or rely on remote CI.

Making every push pay that cost would break the gate's fail-fast, cheapest-first
contract for little local benefit — remote CI (`.github/workflows/frontend.yml`)
runs the browser-mode e2e on every PR that touches its path filters
(`frontend/**`, `ops/**`, `scripts/ci/**`, `package.json`, and the workflow
itself) and is the real gate. Run it manually when a change plausibly affects
the browser flow:

```bash
cd frontend
npm run test:e2e
```

If e2e is ever promoted into `ci-local`, it must first gain a configurable port
(an `AO_WEB_PORT`-style override threaded through `playwright.config.ts`) and
handle the built-bundle prerequisite; that is out of scope for the vitest stage.

`npm run agent-ci` is the repo-approved wrapper around `@redwoodjs/agent-ci`.
Use it instead of invoking `npx @redwoodjs/agent-ci run --all` directly. The
wrapper defaults durable runner state to:

```bash
${XDG_CACHE_HOME:-$HOME/.cache}/agent-ci/agent-orchestrator
```

On the orchestrator host that resolves to:

```bash
/home/orchestrator/.cache/agent-ci/agent-orchestrator
```

The wrapper preserves an explicit `AGENT_CI_WORKING_DIR` override for operators
who need a different cache root.

## Cleanup Policy

Run cleanup in dry-run mode first:

```bash
npm run agent-ci:clean -- --dry-run
```

Then, after reviewing the selected paths:

```bash
npm run agent-ci:clean -- --force
```

The cleanup command prunes only stale state under the resolved
`AGENT_CI_WORKING_DIR`. It refuses `/tmp` and other unsafe roots, because the
goal is to keep durable Agent CI state out of temporary host storage rather than
paper over the old default.

After the first successful wrapper run, remove any legacy `/tmp/agent-ci*`
trees manually once you have confirmed no active or intentionally paused retry
state remains there. The repo cleanup command intentionally refuses those
temporary roots.

Agent CI already prunes many stale run workspaces and dependency snapshots when
a run starts. This explicit cleanup command is a dry-run-visible backstop for
abandoned workdirs, interrupted hosts, and cache families that are not covered
by that startup prune. Cleanup covers these Agent CI workdir areas:

- `runs/*`
- `cache/toolcache`
- `cache/npm-cache`
- `cache/pnpm-store`
- `cache/yarn-cache`
- `cache/bun-cache`
- `cache/playwright`
- `cache/remote-workflows`
- `cache/dtu`
- `cache/node-modules-v2`
- `runner`

Unexpected top-level workdir entries and unexpected `cache/*` entries are
reported under `not covered by cleanup` and are never deleted automatically.

By default, paths must be older than 14 days before they are selected. Recent
runs and caches are preserved, including directories with recent descendant
files.

Paused retry state is preserved indefinitely only when a run directory contains
Agent CI's `signals/paused` marker. `detached.json` alone is not a preservation
marker. Abort or resolve paused retry state before pruning it.

Files written by runner containers may appear on the host as UID/GID `1001`
and may display as `jt:coachclaw` on this machine. Treat that as numeric
container ownership, not as evidence that the `jt` account touched the files.
If normal deletion fails, remove only the selected path under the Agent CI
workdir with the explicit helper:

```bash
npm run agent-ci:clean -- --force --docker-root-helper
```

The helper mounts only the resolved Agent CI workdir and removes the already
selected relative paths from inside that mount. Do not key cleanup behavior on
the host account name.

Agent CI also writes run logs and result metadata under
`${XDG_STATE_HOME:-$HOME/.local/state}/agent-ci/logs`. That state directory is
outside `AGENT_CI_WORKING_DIR`; Agent CI's own state-log cleanup handles it.

## Upstream Candidate

This repo wrapper is intentionally small and local. The upstream
`@redwoodjs/agent-ci` improvement to consider is changing the normal Linux
default away from `/tmp` toward an XDG cache directory, and expanding its clean
command so stale runner workspaces, package-manager caches, toolcache,
Playwright/browser caches, diagnostics, and paused retry state have a clear
operator policy.
