# Repo-Specific Guidance

## Repo layout

- `backend/` contains the Go daemon, Cobra CLI, services, storage, runtime
  adapters, lifecycle/reaper, terminal mux, and tests.
- `frontend/` contains the Electron and React supervisor wired to the generated
  daemon client.
- `docs/` contains current architecture and status notes. Start here before
  changing lifecycle, CLI, agents, storage, or daemon behavior.
- `test/` contains external smoke/e2e assets, including the CLI fresh-install
  container check.
- `.github/workflows/` contains CI definitions. Mirror these commands locally
  when possible.

## Commands

From the repo root unless noted:

```bash
npm run lint
npm run frontend:typecheck
npm run sqlc
npm run api
npx @redwoodjs/agent-ci run --all
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
`gofmt`, `go build`, `go vet`, `go test -race ./...`, golangci-lint (pinned to
the CI version v2.12.2, run via `go run` — no separate golangci install needed),
and `npm run frontend:typecheck`. `npm run format:check` is the fast
changed-files-only subset if you just need the format check.

Optionally install it as a git `pre-push` hook (per-clone, opt-in) so it runs
automatically on `git push`; bypass a single push with `git push --no-verify`:

```bash
npm run hooks:install
```

Backend-specific checks:

```bash
cd backend
go build ./...
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/ao start
```

Frontend-specific checks:

```bash
cd frontend
npm run typecheck
npm run build
```

When showing or demoing frontend changes, run `ao preview [url]` from inside
the session so the change renders in the desktop browser panel.

## Distribution

The desktop app from GitHub Releases is the canonical install path. The
`@aoagents/ao` npm package remains a frozen legacy on-ramp at `0.10.0`; do not
add new features, docs, or flows that treat npm as the intended install path.

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
