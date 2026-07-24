## Context

AO has two **role sessions** — the fleet-wide Prime singleton and the per-project Orchestrator. Both
are durable, both live on a canonical role branch (`ao/prime`, `ao/<project>-orchestrator`), and both
are supposed to be present whenever they are desired. Neither is currently driven by a desired-state
loop over _all_ role rows.

Current state, as read from the tree:

- Prime and Orchestrator duplicate spawn, active-session lookup, replacement verification, retire
  notices, and workspace-path strategy. The only genuinely shared pieces are
  `Manager.RetireForReplacement`, `newestSession`, and the branch-naming switch. Prime borrows the
  orchestrator project lock through the magic key `"__fleet_prime__"`.
- `SpawnOrchestrator` and `SpawnPrime` both consider only **active** rows. The Prime supervisor's two
  paths (missing vs unhealthy) differ _only_ in a `clean` flag, and `clean` retires active rows. So
  **no code path anywhere retires a terminated role row**, which is exactly the residue that causes
  the outage.
- The two roles then fail _differently_ on that residue. Prime's workspace path is per-session, so a
  replacement misses the existing-worktree check and hits git's
  `branch is already checked out in another worktree` — surfaced as a 409 and burning a restart-budget
  slot per attempt. The Orchestrator's path is canonical per-project, so the replacement silently
  **adopts** the dead worktree instead of failing.
- Teardown builds its workspace handle from the session row alone, taking the repo path only from
  `WorkspaceRepoPath`. A projectless Prime has no project id to fall back on, so an empty field makes
  the workspace layer fail with `project id is required`, which cleanup reports as the generic
  `workspace teardown failed`. See Decision 4 — an earlier reading blamed ignored `.claude/` residue,
  which direct probing of git disproved.
- `Manager.Cleanup` iterates every terminated row and destroys the workspace recorded on that row
  **with no check that a live session now owns that path**. Because Orchestrator role workspaces are
  canonical and therefore _shared across successive role rows_, a historical terminated row points at
  the live replacement's worktree. The only thing preventing cleanup from deleting it is that a live
  role worktree is _usually_ dirty, so git refuses — a clean one is already exposed today.
- `reconcileReap` correctly kills runtimes that outlived their terminated row, but it is reachable
  only from the one-shot boot `Reconcile`. A tmux session leaked by an agent `/exit` survives until the
  next daemon restart.
- The restart budget lives in an in-goroutine struct with no external door. It is cleared only by a
  healthy Prime observation or by settings being disabled _at tick time_, which is why the operator's
  workaround was "hold Prime disabled long enough for a tick".
- Prime has no relaunch door at all: both public entry points are deliberately barred
  (`PRIME_MANUAL_SPAWN_FORBIDDEN`, `PRIME_MANUAL_RESTORE_FORBIDDEN`), and the Prime controller mounts
  only settings and prompt.
- In the renderer, Prime's existence is derived from a live session row at three layers (workspace
  synthesis, the active-prime selector, and the sidebar gate). With no live row, Prime vanishes from
  navigation entirely, and a dead Prime terminal offers the generic restore strip that the backend
  rejects with 403.

## Goals / Non-Goals

**Goals:**

- One reconciliation contract that both role kinds go through, so Prime and Orchestrator cannot drift
  into divergent recovery behavior.
- A desired role session becomes present without human git/tmux surgery, regardless of whether the
  newest role row is active, terminated, stale, dirty, or leaking a runtime.
- Historical terminated role rows can neither block a replacement nor destroy the live one.
- An explicit user action can force reconciliation now and clear budget-paused state.
- The supervisor UI reflects _desired_ role state, not merely observed live rows.

**Non-Goals:**

- Changing normal worker session restore or cleanup semantics.
- Removing the Prime restart budget; it stays as crash-loop protection.
- Unifying Prime's workspace path layout with the Orchestrator's canonical layout (see Decision 2).
- Model/harness selector behavior, tracked separately.

## Decisions

### Decision 1 — One `ReconcileRole` entry point keyed on a `RoleTarget`

Introduce a role identity value (`kind` + optional `projectID`) and a single service operation that
takes a desired role target and makes it true: resolve the active row, release stale resources, reap
leaked runtimes, then create the replacement under the existing role lock. `SpawnOrchestrator`,
`SpawnPrime`, the supervisor's missing and unhealthy paths, Prime settings save, and the new relaunch
endpoint all become thin callers.

_Alternative considered — fix each role in place._ Rejected: the ticket's constraint is explicit, and
the two roles already fail differently on identical residue. Two implementations of "recover a role
session" is how we got a bug that reproduces one way for Prime and the opposite way for the
Orchestrator.

_Alternative considered — a generic "reconcile any session" loop._ Rejected as larger than the
problem. Worker sessions are not desired-state; they are spawned on demand and are correctly allowed
to stay terminated. Only role sessions carry a canonical branch and a "should exist" property.

### Decision 2 — Key stale-resource release on the canonical role **branch**, not the workspace path

The thing git actually refuses on is the branch, and both roles have a canonical branch while only the
Orchestrator has a canonical path. Reconciliation therefore asks "who holds `ao/prime` /
`ao/<project>-orchestrator`?" and releases that holder when it is not a live role session.

_Alternative considered — make Prime's workspace path canonical like the Orchestrator's._ Rejected as
a larger and riskier change: it alters on-disk layout for existing installs, needs a migration for
in-flight Prime worktrees, and would _not_ fix the Orchestrator half, which already has a canonical
path and still fails. Branch-keying fixes both roles with no layout change. It also means the
Orchestrator's silent-adoption path and Prime's hard-failure path converge on the same check.

### Decision 3 — Workspace destruction is gated by a single ownership predicate in the manager

Rather than adding a "is this path live?" check at each teardown call site, the manager gets one
predicate that answers whether a given terminated row still owns the workspace recorded on it, and
every teardown path (`Cleanup`, project teardown, retire-for-replacement) consults it. A role row
whose recorded path or branch is currently held by an active session does not own that workspace and
is skipped, with the skip reported as a distinct, non-alarming reason rather than the generic
`workspace teardown failed`.

_Alternative considered — clear or quarantine the historical row's workspace metadata at replacement
time_ (one of the ticket's two decision defaults). Rejected: it mutates history to make a read-time
question easy, loses the audit trail of where a session actually ran, and needs a storage migration.
The ownership question is cheaply derivable from live state, and deriving it keeps the fact in one
place instead of requiring every future writer to remember to scrub.

_Why a predicate and not "cleanup simply never touches role workspaces":_ Prime's per-session paths
must still be reclaimed once they are genuinely nobody's, or they leak forever. Ownership is the
correct invariant; "role" is not.

### Decision 4 — Teardown derives a role's repo path from role identity

**Corrected after empirical verification.** This decision originally proposed teaching the dirtiness
probe about version-control-ignored paths and escalating role teardown to the force path. Probing real
git (2.51.0) disproved the premise:

| worktree contains               | `git worktree remove` |
| ------------------------------- | --------------------- |
| ignored files only (`.claude/`) | **removes**           |
| untracked non-ignored file      | refuses               |
| modified tracked file           | refuses               |

Git refuses only on modified-or-untracked **non-ignored** content, which is exactly what AO's existing
`status --porcelain` probe reports. There is no mismatch to fix, and adding `--ignored` would make AO
refuse teardown _more_ often than git does — a regression, not a fix.

The real cause of the reported `workspace teardown failed` is narrower. Teardown builds its workspace
handle from the session row alone, taking the repo path only from `WorkspaceRepoPath`. A projectless
Prime has no project id to resolve a repo through, so when that field is empty the workspace layer
fails with `project id is required` — a plain error, which cleanup renders as the generic
`workspace teardown failed`. The row is skipped, the stale worktree keeps holding `ao/prime`, and every
replacement spawn hits `branch is already checked out in another worktree` until the restart budget is
spent. That is the reported outage, end to end.

The fix is correspondingly small: teardown derives the repo path from the role identity when the row
does not carry one. The derivation already exists and is already used by the restore paths; teardown
simply was not using it. A persisted path still wins, so this is a fallback rather than an override —
one fact, derived in one place, instead of a field every writer must remember to populate.

_Alternative considered — always force-destroy role workspaces._ Rejected: it treats the symptom, and
force-destroying on cleanup risks discarding real work if a role workspace ever holds any.

_Alternative considered — backfill `WorkspaceRepoPath` on existing rows via a migration._ Rejected:
it repairs today's rows but leaves the derivation absent, so the next row written without the field
reintroduces the bug. Deriving at read time keeps the property true by construction.

**Ordering note.** Decision 3 still lands first, but the reason is not the one originally recorded.
Ignored residue was never the accidental guard — the guard is that live role worktrees are _usually
dirty_, so git refuses. A clean live Orchestrator worktree is therefore already exposed to deletion
today. That makes Decision 3 more urgent than first assessed, not less.

### Decision 5 — Reap leaked runtimes on the reconcile path, reusing the existing reaper

The boot reconcile's reaper logic is correct; the defect is that it runs once. Reconciliation calls
the same logic for the target role before creating a replacement.

_Alternative considered — a periodic background reaper._ Rejected per the operating principles: a
polling loop runs forever, adds a failure surface, and is the classic "second piece of code that
cleans up after the first". Reaping at the moment a replacement is created fixes the cause at the
point the state actually matters.

### Decision 6 — The restart budget stays process-local but gains an explicit clear + immediate poke

The supervisor exposes a signal that (a) clears the budget/backoff and (b) wakes the loop immediately.
Prime settings save and the relaunch endpoint both raise it.

_Alternative considered — persist the budget in storage and clear it with a write._ Rejected: the
budget exists to damp a tight in-process crash loop; persisting it adds a migration and makes the
protection survive restarts, which is the opposite of what a crash-loop damper wants. The existing
in-repo reconcile-poke pattern is reused rather than invented.

### Decision 7 — A dedicated Prime relaunch operation, not a relaxation of the spawn/restore bans

`PRIME_MANUAL_SPAWN_FORBIDDEN` and `PRIME_MANUAL_RESTORE_FORBIDDEN` exist to keep Prime a
supervisor-managed singleton, and they stay. Relaunch is a distinct, idempotent "make the desired
state true now" operation that routes through `ReconcileRole`, so it cannot create a second Prime.

_Alternative considered — allow manual spawn for `kind=prime`._ Rejected: it reopens the
double-Prime hazard those guards were added to close, and it would let the UI create Prime through a
path the supervisor does not know about.

### Decision 8 — Prime UI presence derives from persisted settings

The renderer reads Prime enablement from Prime settings and renders the navigation entry and route
whenever Prime is enabled, using live session rows only to decide _which state_ to show (running,
starting, not running, capped). The not-running state offers one primary `Relaunch Prime` action, and
Prime's ended terminal routes to relaunch rather than the generic restore strip.

_Alternative considered — synthesize a placeholder Prime session row in the workspace projection._
Rejected: fabricated rows leak into every selector that filters sessions, and the ticket's constraint
is precisely that the UI must not treat a live row as the source of truth for whether Prime is
desired.

## Risks / Trade-offs

- **[Cleanup ownership check silently retains a workspace that really is garbage]** → The skip reason
  is distinct and logged, and reclaiming remains possible once the live owner terminates; a retained
  worktree is recoverable, a deleted live one is not.
- **[Force-escalated teardown removes something an operator wanted]** → Escalation is scoped to role
  workspaces only, and only after the ownership predicate has cleared the path as unowned.
- **[Branch-keyed release races a concurrently starting role session]** → Release happens inside the
  existing per-role lock that already serialises replacement, so a competing reconcile blocks rather
  than interleaves.
- **[Clearing the restart budget on user action re-enables a tight crash loop]** → The clear is only
  reachable from an explicit user-initiated relaunch or settings save, not from the automatic path,
  so an unattended crash loop still damps as before.
- **[Prime nav driven by settings shows an entry the daemon cannot satisfy]** → That is the intended
  behavior; the route explains the state and offers the recovery action, which is strictly better than
  Prime disappearing with no affordance.
- **[The refactor in phase 1 touches the session list filter used by non-role callers]** → It is a
  pure rename/generalisation with no behavior change, covered by the existing service tests before any
  behavioral phase builds on it.
- **[Existing frontend assertion contradicts the new behavior]** → `findFleetPrime`'s "ignores
  terminated prime sessions" test encodes the old model and is updated deliberately as part of the
  Prime-presence phase, not incidentally.

## Migration Plan

No storage migration and no API removals. The Prime relaunch endpoint is additive, so the generated
OpenAPI and TypeScript client are regenerated but no existing client call changes shape. Rollback is a
plain revert; nothing persists new state that an older daemon would misread.

## Open Questions

None blocking. The ticket's two decision defaults for the historical-row hazard were resolved in
Decision 3 (skip via an ownership predicate rather than quarantining row metadata), and the ambiguity
about whether to unify Prime's workspace layout was resolved in Decision 2 (branch-keyed release, no
layout change).
