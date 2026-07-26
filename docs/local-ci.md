# Local CI And Agent CI State

This repo has two local CI entrypoints:

```bash
npm run ci-local
npm run agent-ci
```

`npm run ci-local` is the required pre-push parity gate. It mirrors the remote
format/lint/build/test/typecheck jobs and does not use `@redwoodjs/agent-ci`.

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
