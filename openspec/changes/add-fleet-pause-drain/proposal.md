## Why

There is no safe way to stop the fleet. When an operator needs to halt agent
work — to investigate a problem, ship a risky change, or cut costs — the only
options today are killing the daemon (loses in-flight work and orchestrator
state) or editing project config (mutates durable files and races the
runtime). We need a first-class, reversible pause that stops new work without
destroying work already underway, at both fleet-wide and per-project scope.

## What Changes

- Introduce a **pause bit stored outside project config**, so a pause/resume
  cycle leaves every project's config file byte-for-byte identical. Pause state
  lives in daemon-owned storage, not in the user-authored config.
- Add **two pause scopes**: a single global **fleet** flag and a per-**project**
  flag. Either being set gates a project. The fleet flag is independent of
  project rows, so a project registered _while the fleet is paused_ is gated
  from its first moment.
- Add **two pause modes**:
  - **Soft pause** — stop _new_ work only: gate tracker intake and guard the
    spawn path, then let a **drain sweeper** terminate each worker as it reaches
    an idle/terminal state. Mid-flight work is preserved and finishes normally.
  - **Hard pause** — terminate live workers immediately (UI confirms first).
- Wire **two authoritative enforcement points**: the tracker-intake observer
  (no new intake when paused) and the session/spawn path (no new spawns when
  paused). Both check fleet OR project pause.
- Add a **drain sweeper** — a lifecycle component, separate from the existing
  fact-only reaper — that periodically terminates drainable workers belonging to
  paused projects and emits drain-progress/complete telemetry.
- Thread **pause state and draining count** through the project/workspace
  summary so the frontend can render fleet and per-project status.
- Add **daemon HTTP routes** and **CLI commands** to set/clear fleet and project
  pause (soft/hard), and surface pause state in `status`/`stop` output.
- Add **UI**: a Global Settings "Fleet" card (Running / Paused; Pause /
  Pause-now-hard with confirm / Resume) and per-project sidebar badges
  (`Draining(N)` / `Paused`) with kebab pause/resume actions.
- Orchestrators are **not** paused: they stay alive and idle, and alerts keep
  firing. This change deliberately does **not** port the old fork's
  prime/orchestrator pause-notice choreography.

## Capabilities

### New Capabilities

- `fleet-pause`: Reversible fleet-wide and per-project pause/resume with soft
  (drain-at-idle) and hard (immediate-terminate) modes; out-of-config pause
  storage; intake + spawn enforcement; a drain sweeper; and pause/draining state
  surfaced through the API, CLI, and UI.

### Modified Capabilities

<!-- No existing spec's requirements change; enforcement is wired into intake/spawn code paths that are not spec-governed. -->

## Impact

- **Storage**: two new SQLite migrations — a `paused` column on the projects
  table and a single-row daemon-settings `fleet_paused` flag (fresh migration
  numbers, not the old fork's `0032`). New sqlc queries for reading/writing both.
- **Backend services**: new pause service (set/read fleet + project pause, hard-
  drain fan-out); new drain lifecycle sweeper package; enforcement hooks in the
  tracker-intake observer and the authoritative spawn/session path.
- **API**: new daemon HTTP routes + DTOs for pause/resume; project/workspace
  summary DTO gains pause-state and draining-count fields. Regenerate OpenAPI +
  TS client.
- **CLI**: new `pause`/`resume` command surface; `status`/`stop` show pause
  state.
- **Frontend**: Global Settings Fleet card; per-project pause badges and kebab
  actions wired to the generated client.
- **Not touched**: user project-config files (byte-stable across pause/resume);
  orchestrator lifecycle; the existing reaper (drain is a separate sweeper).
- **Upstream-candidate**: keep packages near-verbatim to the reference port and
  rebase-friendly for later upstream submission.
