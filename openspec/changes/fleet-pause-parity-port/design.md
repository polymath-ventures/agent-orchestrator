## Context

Round-1 (#5 / PR #33) shipped fleet + per-project pause/drain by rebuilding from
prose rather than porting the old fork's bug-fix corpus
(`~/agent-orchestrator-fscked`). The operator ruling for the reopen is
**PORT → FIT → IMPROVE → FIX**: lift the reference's refined behavior, adapt only
the seams, preserve the pieces already verified better, and apply only the
deltas the reference never had.

A verified gap analysis (every claim checked in code on both sides) found the
current tree already at parity on storage/migrations, the drain sweeper, guard
exemptions, sidebar badges, and API codes. The issue comment's heading says
"1 must, 6 should" but contains **six numbered gaps total**: GAP 1 (must) and
GAPS 2–6 (should, including the CLI help/validation gap). This change implements
all six numbered gaps. Each fix below cites reference `file:line` so "studied
the reference" is falsifiable.

## Goals / Non-Goals

**Goals:**
- Fleet-wide hard pause is a true emergency stop (workers **and** orchestrators).
- Pause enforcement fails closed on a storage read error.
- The Fleet card can escalate an in-progress drain to a hard stop, shows
  `Draining (N)` with a 15s self-refresh, and confirms the true blast radius.
- `ao pause` documents drain semantics and rejects a blank project id.
- Every history-mined lesson is demonstrably preserved (verification checklist).

**Non-Goals:**
- No storage/migration changes (0030 stays; no new migration).
- No API DTO / generated-client changes (draining count is already surfaced on
  the workspace summary).
- **Not porting** the reference's per-project orchestrator-replacement
  supervisor — see Decisions.

## Decisions

### D1 — Thread `includeOrchestrators` through the existing inlined hard-drain (GAP 1, must)
Reference keeps the fan-out in the session service (`service/session/service.go:958`,
`:973`) and passes `true` on the fleet path (`service/project/pause.go:67-71`) and
`false` per-project (`:30-33`). The current tree already inlined the drain into
`service/project/pause.go:137` (`hardDrain`, worker-only filter at `:150`). The
**smallest** parity fix is to add an `includeOrchestrators bool` parameter to
that private helper, change the filter to skip orchestrators only when the flag
is false, and pass `true` from `SetFleetPaused` (`:93-95`) and `false` from
`SetProjectPaused` (`:122-126`). *Alternative rejected:* re-introducing a
separate `HardDrain` verb on the session service — larger surface, an interface
change, no behavioral benefit over threading the flag.

### D2 — Enforcement paths fail closed; display paths stay fail-open (GAP 2, should)
Reference errors the spawn on a read failure (`service/session/service.go:417-421`)
and aborts the intake tick (`observe/trackerintake/observer.go:144-146`). Current
swallows the error with `err == nil && fleetPaused` at the spawn guard
(`session_manager/manager.go:557`) and intake (`observer.go:135`). Fix: return
the wrapped error in both. The read-model helpers
(`service/project/pause.go` pauseState, `service/project/service.go`) intentionally
fail open for display and are **left unchanged** — a storage blip must not wedge
the UI, and they never gate work.

### D3 — Fleet card: escalate + Draining(N) + 15s refresh + true confirm copy (GAPS 3/4/5)
The workspace summary already carries `pauseState` and `drainingWorkers`
(`types/workspace.ts:183,335-337`) and `useWorkspaceQuery` already self-polls at
15s (`hooks/useWorkspaceQuery.ts:55-56,86`), so no backend/type work is needed.
In `FleetSection.tsx`: (a) hoist the "Pause now (hard)" button out of the
`!paused` branch so it renders while paused/draining (mirror
`FleetPauseSection.tsx:124-126`); (b) add `refetchInterval: 15_000` to the
fleet-status query and derive the `Draining (N)` aggregate from
`useWorkspaceQuery` (mirror `FleetPauseSection.tsx:22,44-57`); (c) rewrite the
`ConfirmDialog` description to name orchestrators + drain guidance (mirror
`FleetPauseSection.tsx:59-66`). Keep the target's `ConfirmDialog` mechanism
(an IMPROVE over the reference's native `window.confirm`); only the copy/behavior
reaches parity.

### D4 — CLI help + blank-id validation (GAP 6, should)
Port the reference `Long` drain-semantics help (`cli/pause.go:20-31`) and the
blank-id `usageError` guard + `TrimSpace` on use (`:57-60,87`) into the current
`cli/pause.go` (help missing at `:18-21`; args validator at `:47-52` checks only
length). Returning `usageError` yields exit 2 per repo convention.

### D5 — Reject porting the per-project orchestrator-replacement supervisor (history lesson #5)
The reference skips unhealthy-orchestrator replacement for paused projects
(`daemon/orchestrator_supervisor.go`). The current tree has **no**
`orchestrator_supervisor.go` / `startOrchestratorSupervisor` at all — only a
prime supervisor, which correctly stays active during pause. With no
auto-replacement path, the "skip replacement while paused" guard has nothing to
guard: a paused project's drain cannot be undone by replacement, so the lesson's
intent holds vacuously. Porting an entire supervisor subsystem is outside the
verified gap list and would be over-engineering (rule 9). Rejected, documented
here and in the PR.

## Risks / Trade-offs

- **[Emergency stop kills orchestrators fleet-wide]** → This is the intended
  behavior; per-project hard pause still spares orchestrators, and the UI confirm
  now states the blast radius so it is not issued by accident.
- **[Fail-closed spawn/intake could halt work during a real storage outage]** →
  Correct posture for a safety gate: during a storage outage we prefer not
  spawning to silently un-pausing. Display paths stay fail-open so the UI stays
  legible.
- **[Test enshrines old worker-only behavior]** → `pause_test.go` currently
  asserts fleet hard pause spares orchestrators; that test is inverted to match
  the emergency-stop requirement.

## History-mined verification checklist (must survive in the ported code)

Verified HOLDS in the current tree (cited, no change needed): (1) pause bit
outside config / `UpsertProject` omits it; (2) fleet flag is a singleton, not a
fan-out; (3) `spawn --force` skips admission but drain has no force exemption;
(4) drain sweeper separate from the fact-only reaper; (6) hard-drain best-effort
aggregated errors; (7) `?hard=` via `strconv.ParseBool`; (8) drain goroutine
awaited on the boot-failure teardown path via `lcStack.Stop()`; (9) alerts stay
on during pause. (5) is addressed by D5.

## Open Questions

None — all decisions are settled above.
