## Why

An agent can sit wedged for hours — the reported case was one stuck behind a
leaked `curl` for eight — and nothing surfaces it. `ao doctor` reports on the
daemon, the tools, and the harnesses, but never on whether the work is actually
moving.

A previous attempt walked each pane's process tree to find what was holding the
foreground. It was removed, and #178 records why it cannot work: AO launches
every pane as `sh -c '<agent argv>; exec $SHELL -i'`, so `#{pane_pid}` is a
wrapping shell; ppid establishes ancestry, not foreground ownership, so a leaked
`curl` and a healthy long-lived MCP server look identical; and a non-interactive
`sh -c` enables no job control, so the wrapper, the agent, and every grandchild
share one process group and `tpgid` cannot disambiguate them either. Two macOS
portability bugs turned up in it along the way (`ps -o etimes=` is Linux-only,
`ps -o comm` returns a full path on macOS), which is a fair signal about how much
surface the heuristic was accumulating.

The operator's real question was never "what process is running" but **"is this
session wedged?"** — and AO already records the authoritative answer.
`domain.Activity.LastActivityAt` is the daemon's own record — deliberately the
moment the current state was entered, not the last signal received — and it is
already on the session listing the removed check was fetching. For an active
session it therefore measures how long the agent has been active WITHOUT
finishing a turn, which is exactly the wedge signature: a healthy agent
transitions to idle or waiting_input between turns, while one blocked on a
leaked `curl` never leaves active.

## What Changes

- Add a `sessions-idle` check to `ao doctor` that warns when a session the
  daemon records as **active** has been in that state, with no transition to any
  other, for longer than a fixed threshold — naming each such session, how long
  it has been stuck, and a command to inspect or end it.
- Only the active state warns. `idle` means the agent finished a turn;
  `waiting_input` and `blocked` mean it is paused on the user and are routinely
  left that way overnight, so warning on them would make the check noise.
- The signal comes from the daemon's session listing over the existing loopback
  API. The check performs no process inspection: no `ps`, no `tmux`, no
  process-tree walk, and no platform-specific tool invocation, so it works the
  same on macOS and Linux by construction.
- The check is read-only and never touches `supervise.sock`. It cannot fail
  `ao doctor`: an unreachable daemon means the signal is unavailable, not that
  the machine is unhealthy.

## Capabilities

### New Capabilities

- `doctor-wedged-sessions`: `ao doctor` surfaces sessions that have gone silent,
  derived from AO's own activity records rather than from process inspection.

### Modified Capabilities

<!-- None. No existing spec's requirements change. -->

## Impact

- **New**: a check in `backend/internal/cli/doctor_sessions.go` plus its tests.
- **Changed**: `runDoctor` gains one check in the Core section.
- **Unchanged**: the daemon, the API surface (the check reads the existing
  `GET /api/v1/sessions`), storage, and the frontend. No API contract change, so
  no `npm run api` regeneration.

## Non-goals

- Reporting a specific offending pid. That is what could not be determined
  reliably, and it is not what the operator needs to act on.
- Killing or remediating anything. This is a diagnostic.
