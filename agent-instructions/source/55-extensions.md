# Repo-Specific Guidance

## Repo layout

- `backend/` contains the Go daemon, Cobra CLI, services, storage, runtime
  adapters, lifecycle/reaper, terminal mux, and tests.
- `frontend/` contains the React web supervisor wired to the generated daemon
  client, plus upstream Electron shell code that this fork preserves for
  compatibility.
- `docs/` contains current architecture and status notes. Start here before
  changing lifecycle, CLI, agents, storage, or daemon behavior.
- `test/` contains external smoke/e2e assets, including the CLI fresh-install
  container check.
- `.github/workflows/` contains CI definitions. Mirror these commands locally
  when possible.

## Fork posture

This fork is web-first. Treat the browser-based supervisor talking to the daemon
over HTTP as the primary product path. Electron behavior is upstream
compatibility surface, not the default assumption for Polymath work. Do not make
frontend correctness depend on `window.ao`, Electron preload APIs, Electron-only
daemon status fields such as a discovered port, or desktop packaging behavior
unless the ticket explicitly targets Electron. For frontend changes, include or
preserve web-mode coverage when the behavior can run in a browser.

Because this fork has both remotes, skills derive the operational GitHub repo
from the checkout's `origin` remote. Do not use `upstream` or bare
`gh repo view` heuristics to choose the issue/PR target. If `origin` is a fork
and `upstream` is the public project, operational GitHub issue/PR commands
target `origin`; `upstream` is only for explicitly upstream-scoped workflows
such as syncs.

## Deploy

This fork deploys with `ops/deploy.sh [ref]`. After landing a PR, deploy the
merged default-branch commit with `ops/deploy.sh <merged-sha>` and trust the
script's built-in verification for the daemon, web supervisor, public URL, and
fresh boot log. Do not rediscover deploy targets during routine
`land-and-deploy`; only inspect deployment configuration when this script is
missing, fails, or the ticket explicitly changes deployment.

## Commands

From the repo root unless noted:

```bash
npm run lint
npm run frontend:typecheck
npm run sqlc
npm run api
npm run agent-ci
```

### Pre-push gate (required before every push)

Run the local CI-parity gate before pushing — it mirrors the remote `format`
and `lint` CI jobs (plus build/vet/test/typecheck) so those violations are
caught locally instead of on a wasted remote CI round-trip:

```bash
npm run ci-local
```

It runs, fail-fast and cheapest-first: `format:check` (prettier `--check
--ignore-unknown` on changed files, matching `.github/workflows/prettier.yml`),
`npm run test:ops`, `gofmt`, `go build`, `go vet`, `go test -race ./...`,
golangci-lint (pinned to the CI version v2.12.2, run via `go run` — no separate
golangci install needed), `npm run frontend:typecheck`, and `npm run
frontend:test` (the renderer vitest suite). `npm run format:check` is the fast
changed-files-only subset if you just need the format check. The browser-mode
Playwright e2e (`frontend/e2e`) stays a manual opt-in step, not part of the
gate; see `docs/local-ci.md`.

The five Go stages are **scoped to the diff** — they are skipped only when the
gate positively establishes the branch touches none of `backend/`, `scripts/ci/`,
a root `go.work`, or a root `go.work.sum`; anything else, including an unusable
base ref, runs them. The reason is printed either way. Remote CI stays unscoped
and remains the real gate. See `docs/local-ci.md` before changing that scoping or
`scripts/ci/`.

Optionally install it as a git `pre-push` hook (per-clone, opt-in) so it runs
automatically on `git push`; bypass a single push with `git push --no-verify`:

```bash
npm run hooks:install
```

For the Agent CI runner, use `npm run agent-ci` instead of direct `npx
@redwoodjs/agent-ci run --all`. The wrapper sets `AGENT_CI_WORKING_DIR` to a
durable per-account/per-repo cache outside `/tmp`, defaulting to
`${XDG_CACHE_HOME:-$HOME/.cache}/agent-ci/agent-orchestrator`. Inspect stale
runner/cache state with `npm run agent-ci:clean -- --dry-run`; only use
`npm run agent-ci:clean -- --force` after confirming the selected paths are not
active or intentionally paused retry state. See `docs/local-ci.md` for the full
cleanup policy, including UID/GID-mapped container-owned files.

Backend-specific checks:

```bash
cd backend
go build ./...
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/aong --help
```

Frontend-specific checks:

```bash
cd frontend
npm run typecheck
npm run build
```

When showing or demoing frontend changes, run `aong preview [url]` from inside
the session so the change renders in the desktop browser panel.

## Distribution

For this fork, the web supervisor is the canonical product direction. Upstream
Electron packaging may remain in the tree for compatibility, but new product
flows should not assume the desktop app is the user's runtime unless the ticket
explicitly says so. The `@aoagents/ao` npm package remains a frozen legacy
on-ramp at `0.10.0`; do not add new features, docs, or flows that treat npm as
the intended install path.

## Coding conventions

Keep changes surgical and tied to the task. Follow existing Go package
boundaries. CLI code should call daemon HTTP routes through shared CLI client
helpers; it should not open SQLite, spawn runtimes, or call adapters directly.

Return usage errors as `usageError` so CLI misuse exits 2; runtime or daemon
failures should exit 1. Preserve API error envelopes and request IDs when
surfacing daemon errors. Use `context.Context` as the first argument for
functions that do I/O or blocking work.

Do not modify already-merged SQLite migrations. Add a new migration instead.
Do not hand-edit generated sqlc code; change queries or migrations and run
`npm run sqlc`. For daemon API contract changes, edit controller DTOs and the
spec generator, then run `npm run api` and commit the generated OpenAPI and
TypeScript schema updates together.

The daemon primary listener stays bound to `127.0.0.1` and unauthenticated. A
second opt-in LAN listener may bind `0.0.0.0` only while explicitly enabled and
only behind bearer-password auth, as documented in
`docs/adr/0001-lan-listener-for-mobile.md`.

All app state belongs under `~/.ao` unless explicitly overridden by
`AO_DATA_DIR` or `AO_RUN_FILE`. Do not rely on Electron default app-data paths.

## Final-review status contract

The clean status is the only machine-readable final-review verdict the merge
gate may consume.

`/final-review` emits its verdict as a GitHub commit status on the reviewed head
SHA, using context `final-review`. A clean review writes `state=success`; a
non-clean, inconclusive, or timed-out review writes `state=failure`. The status
description is the parseable contract: `verdict=<clean|parked>
reviewer_family=<family> head=<full-head-sha>`. A clean review that is parked
only because repo policy requires a human merge still writes
`final-review=success`; the human gate is recorded separately as a current-head
`merge-park` status with `reason=human-required`.

Human merge gates check the `final-review` status on the **current** PR head
SHA. Autonomous-merge paths check the same clean review status and additionally
refuse to merge when a current-head `merge-park` signal exists or when a linked
issue carries the manual non-AO-worker hold marker `no-ao`. If the PR receives
a new push, the old statuses are tied to the old SHA and no longer count. This
replaces any PR-comment protocol; do not use comments or free-form summaries as
the gate.

AO's native review API (`GET /sessions/{id}/reviews`, with states such as
`ineligible` or `needs_review`) is a separate AO reviewer system. It is useful
for AO's own review UI, but it is **not** `/final-review` and must never be read
as the final-review merge verdict.

Repos that carry `ops/final-review-status.mjs` use it as the status helper:
`node ops/final-review-status.mjs set --repo <owner/repo> --sha
<full-head-sha> --verdict <clean|parked> --reviewer-family <family>
--author-family <implementer-family>` after the review loop; add
`--human-merge-required` when a clean review must park for human merge authority.
A clean `set` **requires** one or more `--author-family` values and is
**refused** when `--reviewer-family` matches any of them. Reviewer independence
is enforced here, at write time, so a clean status is independent by
construction. Pass several `--author-family` flags when more than one family
authored the head. Use `node ops/final-review-status.mjs check --repo
<owner/repo> --sha <current-head-sha>` for a human-authorized merge gate, and
add `--mode autonomous --pr <PR-number>` for autonomous merge eligibility. The
`check` command is deliberately family-agnostic because independence was already
enforced at `set` time, so the required `review-passed` merge-queue gate, which
cannot see per-session harness provenance, is never bricked.

## Agent reviewers run in the foreground — AO clarification

The shared "Agent reviewers run in the foreground" rule binds AO unchanged. One
AO-specific clarification: AO's own daemon launch of worker sessions into a TTY
is already blocking/attached and stays that way.
