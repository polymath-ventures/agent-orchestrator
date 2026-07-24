## Why

Prime and project Orchestrators are durable role sessions meant to stay available on canonical role
branches, but their lifecycle is not a desired-state loop — it is two independent spawn paths that
only ever consider _active_ rows. When a role session is killed from its terminal, the row is marked
terminated while its worktree still holds the canonical role branch, its tmux runtime may still be
alive, and ignored `.claude/` residue defeats normal worktree teardown. Recovery then requires
manual `git worktree` surgery, manual tmux cleanup, and a daemon redeploy. Two live outages have
already been traced to this: Prime exhausted its restart budget against
`branch is already checked out in another worktree: "ao/prime"`, and the `coachclaw` Orchestrator
silently adopted a dead worktree while older terminated rows kept pointing at the live replacement's
canonical path.

That last detail makes this urgent rather than merely annoying. Cleanup already iterates every
terminated row and destroys its recorded workspace path with **no check that a live session now owns
that path**; today the only thing preventing it from deleting a live Orchestrator's worktree is the
very teardown bug we need to fix. The two defects mask each other, so they must be resolved together
and in the right order.

## What Changes

- Introduce a **shared role-session reconciliation contract** covering both fleet Prime and project
  Orchestrators, replacing the per-role duplication of spawn, active-session lookup, replacement
  verification, and desired-state driving. Prime stops borrowing the orchestrator project lock
  through a magic key.
- Reconciliation becomes **desired-state**: when a role session is desired and none is active, AO
  launches one even if the newest role row is terminated — releasing stale terminated resources that
  still own the canonical role branch or path before creating the replacement.
- **Leaked runtimes are reaped on the reconcile path**, not only during the one-shot boot reconcile,
  so a tmux session left alive by an agent `/exit` no longer outlives its terminated row.
- **Workspace teardown learns about ignored residue.** Dirtiness detection accounts for ignored
  paths, and role-path teardown escalates to the force path instead of failing with
  `workspace teardown failed`.
- **Cleanup gains a live-owner invariant**: a terminated row whose workspace path or branch is
  currently owned by an active session is skipped rather than destroyed. This lands _before_ the
  teardown fix above, since the teardown failure is what currently masks the hazard.
- The Prime supervisor's **missing-session and unhealthy-session paths converge** on the single
  reconcile operation, and the restart budget becomes **clearable by explicit user action** while
  remaining a protection against tight crash loops.
- New **Prime relaunch operation** exposed over the daemon API, callable by the supervisor, by Prime
  settings save, and by a user-facing action — triggering immediate reconciliation instead of waiting
  for a supervisor tick or a backoff window. Prime relaunch stays distinct from generic session
  restore, which remains forbidden for Prime.
- **Prime presence in the UI is driven by persisted settings, not by a live session row.** Prime stays
  in the left navigation while enabled, and its route gains an empty/error state explaining that Prime
  is enabled but not running, with one primary `Relaunch Prime` action. A dead Prime terminal offers
  Prime relaunch rather than the generic restore strip that the backend rejects.
- **Dead Orchestrator paths route through Orchestrator spawn/restart** rather than generic worker
  restore assumptions, and a missing Orchestrator gets an actionable affordance instead of a
  button-less warning.
- Corrected copy: the restart-budget notification stops directing the user to inspect an active Prime
  that may not exist, and Prime settings read `Enable Prime` / "Prime supervises globally".

No breaking changes to worker session semantics; normal worker restore behavior is untouched.

## Capabilities

### New Capabilities

- `role-session-reconciliation`: The desired-state lifecycle contract shared by fleet Prime and
  project Orchestrators — how a desired role session is reconciled into existence, how stale
  terminated role resources (canonical branch, canonical worktree path, leaked runtime, ignored
  residue) are released before a replacement is created, and the safety invariant that historical
  terminated role rows can neither block nor destroy the live replacement.

### Modified Capabilities

- `fleet-prime-settings`: Prime's lifecycle requirements change. Ensuring Prime is defined in terms
  of the shared reconciliation contract rather than a bespoke spawn path; an explicit user-initiated
  relaunch operation is added and is required to clear restart-budget-paused state; Prime's presence
  in the supervisor UI is required to follow persisted settings rather than the existence of a live
  session row.

## Impact

- **Daemon services and lifecycle**: `service/session` (role spawn, active-role filtering, role
  locks, replacement verification), `session_manager` (role workspace creation, retire-for-replacement,
  runtime reap, cleanup), `daemon/prime_supervisor` (desired-state loop, restart budget, notification
  copy), `service/prime` (gains a session dependency it does not have today).
- **Workspace adapter**: `adapters/workspace/gitworktree` — dirtiness detection, teardown escalation,
  and branch-conflict handling on canonical role branches.
- **API contract**: a new Prime relaunch endpoint on the Prime controller. Requires controller DTO
  changes plus regenerated OpenAPI and TypeScript schema (`npm run api`).
- **Frontend supervisor**: workspace projection and role selection helpers, sidebar navigation,
  sessions board empty/missing states, terminal ended-state affordances, restore-unavailable dialog,
  a new Prime relaunch helper alongside the existing Orchestrator spawn/restart helpers, and Prime
  settings copy.
- **Risk**: the cleanup live-owner invariant guards against live worktree deletion. It is a
  prerequisite for the teardown-escalation work, not a follow-up to it.
- **Tracking**: GH #133, bead `ao-2aw`.
