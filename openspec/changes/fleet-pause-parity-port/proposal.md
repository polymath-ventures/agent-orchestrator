## Why

The fleet pause/drain feature (#5, PR #33) was reimplemented in round 1 without
studying the accumulated bug-fix corpus in the old fork
(`~/agent-orchestrator-fscked`). A verified gap analysis — every claim checked
in code on both sides — found the current tree diverges from the reference's
refined behavior in ways that matter for an _emergency stop_: a fleet-wide hard
pause cannot actually halt everything, and pause enforcement silently unpauses
the fleet on a storage error. This change closes those parity gaps so pause is
trustworthy when an operator most needs it.

## What Changes

- **Fleet-wide hard pause becomes a true emergency stop**: it terminates live
  workers **and orchestrators** across every project. Per-project hard pause is
  unchanged — it still spares orchestrators so supervision continues.
- **Pause enforcement fails closed**: the spawn guard and the tracker-intake
  observer now abort on a fleet-flag read error instead of proceeding as if the
  fleet were running. A storage blip can no longer silently un-pause the fleet.
  (Read-only _display_ paths still fail open to "not paused" so a blip never
  wedges the UI.)
- **The Fleet card can escalate an in-progress drain to a hard stop**: the
  "Pause now (hard)" control stays available while paused/draining, and the
  card shows a `Draining (N)` lifecycle state with a 15s self-refresh.
- **The hard-pause confirmation states the true blast radius** — it names that
  orchestrators are terminated too, matching the emergency-stop semantics.
- **The `ao pause` CLI documents drain semantics** in its help and **rejects a
  blank project id** as a usage error (exit 2) instead of forwarding whitespace
  to the daemon.

## Capabilities

### New Capabilities

<!-- No new capability; this refines the existing fleet-pause behavior. -->

### Modified Capabilities

- `fleet-pause`: the "Hard pause terminates live workers immediately" and
  "Orchestrators and privileged spawns are exempt from pause" requirements are
  refined so a **fleet hard pause also terminates orchestrators**; a new
  requirement makes **pause enforcement fail closed**; and the API/CLI/UI
  control requirement gains **drain escalation, blast-radius confirmation, and
  CLI help + blank-id validation**.

## Impact

- **Backend**: `service/project/pause.go` (parameterize the hard-drain fan-out
  with `includeOrchestrators`, pass `true` on the fleet path and `false` on the
  per-project path); `session_manager/manager.go` and
  `observe/trackerintake/observer.go` (fail-closed fleet-flag reads);
  `cli/pause.go` (help text + blank-id `usageError`).
- **Frontend**: `renderer/components/FleetSection.tsx` (escalate control,
  `Draining (N)` + 15s refresh via `useWorkspaceQuery`, corrected confirm copy).
- **Not touched**: storage/migrations (`0030_fleet_pause` stays), the drain
  sweeper semantics, the API DTOs and generated client, project config files
  (still byte-stable across pause/resume), and observability/alerting (stays on
  during pause). No new SQLite migration is required.
- **Rejected (out of scope, with reasoning)**: porting the reference's
  per-project orchestrator-replacement supervisor. The current tree has no such
  auto-replacement path, so the reference's "skip replacement for paused
  projects" guard has nothing to guard; adding an entire supervisor subsystem is
  outside the verified gap list and would be over-engineering.
- **Upstream-candidate**: keep the diff minimal and rebase-friendly.
