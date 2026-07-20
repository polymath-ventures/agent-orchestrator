## Context

Agent Orchestrator has no first-class way to stop the fleet. Today an operator
can only kill the daemon (loses in-flight work and orchestrator state) or hand-
edit project config (mutates durable files and races the runtime). This change
ports the old fork's pause system (`~/agent-orchestrator-fscked`, reference GH
#161) to the current upstream-tracking codebase, adapting it to upstream's
package layout while preserving the reference's core axiom: **pause is a bit, not
config surgery.**

Current-state facts that shape the port (from the upstream integration map):

- Migrations live in `backend/internal/storage/sqlite/migrations/` (goose format,
  4-digit prefixes). Highest existing is `0029`; next free is **`0030`**.
- There is **no daemon-global settings table** today. Per-project scalar knobs
  (e.g. `MaxLiveWorkers`) currently live inside the JSON `projects.config` blob
  (`domain.ProjectConfig`).
- The tracker-intake observer (`observe/trackerintake/observer.go`) is the single
  intake decision loop; `Observer.Poll` filters to `Config.TrackerIntake.Enabled`
  projects and `pollProject` calls `o.spawner.Spawn(...)` per new issue.
- Every spawn funnels through `session_manager.Manager.Spawn` (`manager.go:302`),
  which already carries a first-thing `MaxLiveWorkers` cap guard returning the
  sentinel `ErrWorkerConcurrencyCap`.
- The reaper (`observe/reaper/reaper.go`) is a fact-only lifecycle sweeper wired
  via `lifecycle_wiring.go`; it keys on `SessionRecord.IsTerminated`.
- The project list DTO `project.Summary` is built in `project.Service.List`.
- HTTP routes register per-controller under `/api/v1`; the CLI reaches the daemon
  through shared `commandContext.postJSON/getJSON` helpers.
- Queries are sqlc-generated (`backend/sqlc.yaml`); the daemon API is generated
  (`npm run api`).

## Goals / Non-Goals

**Goals:**

- Reversible pause at two independent scopes (global fleet, per-project) with soft
  (drain-at-idle) and hard (immediate-terminate) modes.
- Pause state stored outside the user-authored project config, so a pause/resume
  cycle is byte-stable on config.
- Two authoritative enforcement points: tracker-intake and the spawn path.
- A drain sweeper, separate from the reaper, that terminates drainable workers of
  paused projects and emits drain-complete telemetry.
- Pause/draining state surfaced through the API, CLI, and UI.
- Keep the port near-verbatim to the reference and rebase-friendly for later
  upstream submission (`upstream-candidate`).

**Non-Goals:**

- Pausing or notifying orchestrators. Orchestrators stay alive and idle; alerts
  keep firing. The reference fork's prime/orchestrator pause-notice choreography
  is explicitly **not** ported.
- Any change to the reaper's fact-only contract (drain is a distinct sweeper).
- Persisting the derived `draining` state (it is computed at read time).

## Decisions

### D1: Per-project pause is a dedicated `projects.paused` column, not a JSON config field

The upstream precedent for a per-project scalar (`MaxLiveWorkers`) is a field
inside the `projects.config` JSON blob, and that is the lower-migration path.

**Rejected** — it violates the hard acceptance criterion "pause/resume cycle
leaves project config bytes unchanged." A pause bit inside the config blob means
every pause/resume rewrites `projects.config`, mutating the durable config the
operator authored and races config saves. The reference fork chose a dedicated
column for exactly this reason, and had `UpsertProject` deliberately omit the
column so config saves preserve the bit.

**Decision:** add a dedicated `projects.paused INTEGER NOT NULL DEFAULT 0` column;
the config-write path (`UpsertProject`) does not touch it; only `SetProjectPaused`
writes it. This keeps config byte-stable by construction (prevention over a
detector — Rule 9) and matches the reference verbatim.

### D2: Fleet pause is a singleton `daemon_settings` table, independent of project rows

There is no daemon-global table today. Options: (a) reuse "all projects paused"
as a proxy for fleet pause; (b) a new singleton table.

**Rejected (a)** — "all projects paused" is not the same as "fleet paused": a
project registered *while the fleet is paused* would not be gated, breaking the
acceptance criterion "a project registered during a fleet pause is gated from its
first moment." Deriving a global flag from per-project rows also duplicates a fact
across N rows that must agree (Rule 9: keep each fact in one place).

**Decision:** create `0030_fleet_pause.sql` adding both the `projects.paused`
column and a single-row `daemon_settings(id CHECK(id=1), fleet_paused INTEGER NOT
NULL DEFAULT 0)` table seeded `(1, 0)`. Enforcement reads the global flag
directly, so a newly registered project is gated the instant the flag is set —
the gate lives at the layer that owns the data. One migration file mirrors the
reference (which did both in one migration); a single file keeps the port
reviewable and the rollback atomic.

### D3: `draining` vs `paused` is derived, never stored

The observable state is `running` | `draining(N)` | `paused`, but only the two
persisted bits are authoritative.

**Decision:** compute the state at read time via a `computePauseState(projectPaused,
fleetPaused, liveWorkers)` helper: `running` when neither bit is set;
`draining` + live count when gated with live workers; `paused` when gated with
zero live workers. `liveWorkers` counts non-terminated `KindWorker` sessions for
the project (mirrors the reference `liveWorkersByProject`). No third persisted
field to drift out of sync.

### D4: Enforcement at two points — intake gate + authoritative spawn guard

- **Intake gate** (`observe/trackerintake/observer.go`): short-circuit the whole
  tick when the fleet flag is set (mirrors the reference), and exclude paused
  projects from the enabled-filter in `Poll` (`&& !project.Paused`). A paused
  project keeps its intake config but dispatches nothing. Refusal is silent/no-
  backoff, mirroring the existing `ErrWorkerConcurrencyCap` skip path.
- **Spawn guard** (`session_manager/manager.go`): add a `guardPaused` check
  immediately at the top of `Spawn` (alongside the `MaxLiveWorkers` cap guard,
  before any durable row/workspace is created), returning a new sentinel
  `ErrProjectPaused`. The project record with `.Config` and `.Paused` is already
  loaded at spawn entry, so no extra query. This is authoritative: it also covers
  direct HTTP/CLI spawns that bypass intake.

The spawn guard is the authoritative backstop; the intake gate is the early,
quiet skip so paused projects never even attempt a spawn.

### D5: Guard exemptions — orchestrators, prime tier, and force

Mirror the reference `guardPaused` exemptions: no gating when the spawn is an
orchestrator or prime-tier kind, when a force override is requested, or when the
store is unavailable (fail-open for supervision). This keeps orchestrators alive
and idle under pause (a Non-Goal to pause them) and preserves a manual override.
If upstream's `SpawnConfig` lacks a `Force` field, add one (used only by the
guard); the orchestrator/prime exemption keys on `SpawnConfig.Kind`.

### D6: Drain sweeper is a separate lifecycle component modeled on the reaper

**Decision:** add `observe/drain/drain.go` — a `Sweeper` with a 5s tick
(`DefaultTickInterval`, matching the reaper cadence), wired via a new
`drain_wiring.go` and added to the daemon `lifecycleStack` alongside the reaper
(own goroutine + done channel), started near `daemon.go`'s lifecycle start. Each
tick: read the fleet flag + project list; for each gated project, list its
`KindWorker` sessions, skip terminated, and for each remaining worker terminate it
through the **clean session-teardown path** (the same Kill/terminate path the
reaper/LCM uses — not a raw process kill) iff it is *drainable*; otherwise leave
it (it is still working) to retry next tick. Track a `hadLive` set per project so
the `ao.fleet.drain_complete` telemetry event fires exactly once on the
transition to zero live workers.

Kept separate from the reaper so the reaper stays fact-only by contract.

### D7: Drainability predicate — idle and terminal-equivalent only

Port the reference predicate onto upstream's `domain.Status` (`status.go`):
drainable = `StatusIdle` and the terminal PR-complete state (`StatusMerged` if
present in upstream's enum; confirm the exact constant during implementation).
Everything else — actively working, PR-open/needs-input, and **`StatusNoSignal`**
(broken hook pipeline: can't distinguish idle from working) — is **not** drainable
under soft pause. Hard pause is the escape hatch for `no_signal`/stuck workers.
This is a deliberately conservative predicate that preserves mid-flight work.

### D8: API and CLI mirror the reference surface

- **HTTP** (add to the projects controller + `Register`): `POST
  /projects/{id}/pause`, `POST /projects/{id}/resume`, `GET /fleet`, `POST
  /fleet/pause`, `POST /fleet/resume`; a `?hard=true` query param (`strconv.ParseBool`,
  absent → soft). Responses: `FleetStatusResponse{paused}` for fleet routes; the
  existing project response envelope (now carrying `paused`/`pauseState`/
  `drainingWorkers`) for project routes. Regenerate OpenAPI + TS client
  (`npm run api`).
- **CLI**: `ao pause [project] [--all] [--hard]` and `ao resume [project] [--all]`
  (resume has no hard mode). Route through `commandContext.postJSON` to the routes
  above. Surface pause state in `ao status` (fleet line) and `ao projects`
  (per-project `running`/`draining (N)`/`paused`).

### D9: UI — Global Settings Fleet card + per-project badges

Global Settings gains a "Fleet" card (Running/Paused; Pause / Pause-now-hard with
a confirm dialog / Resume). The project sidebar gains `Draining(N)` / `Paused`
badges and kebab pause/resume actions, all wired to the generated client's new
pause fields and endpoints.

## Risks / Trade-offs

- **Adding a `projects.paused` column touches every projects query** (all SELECT/
  INSERT column lists) and requires an sqlc regen. → Contained, mechanical; mirror
  the reference diff and run `npm run sqlc`. The column is `NOT NULL DEFAULT 0`, so
  existing rows migrate cleanly.
- **`no_signal` workers never drain under soft pause** → by design; document that
  hard pause is the escape hatch. Surfacing `draining(N)` that never reaches zero
  is the operator's signal to hard-pause.
- **A newly created daemon-global table is a new upstream surface** → keep it
  minimal (single row, single flag) and rebase-friendly; it is the smallest thing
  that satisfies the independent-fleet-flag requirement.
- **Force/prime exemptions could let work through during pause** → intentional
  (supervision + manual override). The guard fails open when the store is
  unavailable so a storage blip cannot wedge the orchestrator.
- **Drain drives Kill through the session teardown path** → if that path no-ops
  (e.g. preserved dirty worktree) the worker is counted live and retried next tick,
  never force-killed under soft pause; correct and matches the reference.

## Migration Plan

1. Add `0030_fleet_pause.sql` (projects.paused column + daemon_settings singleton,
   seeded unpaused). Add `queries/daemon_settings.sql`; update `queries/projects.sql`
   column lists. Run `npm run sqlc`.
2. Land storage → pause service → enforcement (intake + spawn guard) → drain
   sweeper → summary threading → API → CLI → UI, each phase TDD-first.
3. Run `npm run api` after the controller/DTO changes; commit generated OpenAPI +
   TS together.
4. **Rollback:** the migration's goose Down drops the table and column; pause is
   additive and defaults to unpaused, so reverting is safe. No data backfill.

## Open Questions

- Exact upstream constant for the terminal/merged drainable status
  (`StatusMerged`?) — resolve by reading `domain/status.go` during the drain phase;
  default to `StatusIdle`-only drainable if no clean terminal-complete constant
  exists, which is strictly safer.
- Whether upstream's `SpawnConfig`/`Kind` already distinguishes a prime tier; if
  not, exempt only orchestrators and rely on the force override for prime-equivalent
  spawns.
- Whether the fleet routes live on the existing projects controller (reference
  choice) or a small new `fleet` controller — pick the smaller diff at
  implementation time; both are idiomatic.
