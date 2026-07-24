## 1. Role-session seam (pure refactor, no behavior change)

- [x] 1.1 Add a role identity value (kind + optional project id) and a predicate for "this session kind is a role", replacing ad-hoc kind switches
- [x] 1.2 Generalize the session list filter from orchestrator-only to kind-based, and port existing callers
- [x] 1.3 Replace the hand-rolled active-Prime lookup with the generalized filter so both roles resolve their active session the same way
- [x] 1.4 Replace the `"__fleet_prime__"` magic lock key with a role-target-derived lock key
- [x] 1.5 Merge the two replacement-verification functions into one role-aware verifier, keeping per-role retire-notice copy
- [x] 1.6 Confirm the existing service and manager suites pass unchanged, proving this group is behavior-neutral

## 2. Cleanup ownership invariant (MUST land before group 3)

- [x] 2.1 Write failing tests: cleanup must not destroy a terminated role row's workspace when an active session holds that path, and must not destroy a worktree whose canonical role branch an active session holds
- [x] 2.2 Add a single workspace-ownership predicate in the session manager that resolves whether a terminated row still owns its recorded workspace path/branch
- [x] 2.3 Route every teardown path (cleanup, project teardown, retire-for-replacement) through that one predicate
- [x] 2.4 Add a distinct ownership-skip reason so an ownership skip is not reported as a workspace teardown failure
- [x] 2.5 Write failing test then confirm: a terminated role row whose path and branch are held by nobody is still reclaimed

## 3. Teardown resolves a role's repo path (re-scoped after empirical verification)

Original plan was an ignored-path dirtiness probe plus force escalation. Probing real git disproved
the premise: `git worktree remove` removes a worktree containing only ignored files and refuses only
on non-ignored modified/untracked content — exactly what AO's existing probe reports. The real cause
is an unresolvable repo path for projectless Prime rows. See design.md Decision 4.

- [x] 3.1 Probe real git to establish which conditions actually make `git worktree remove` refuse
- [x] 3.2 Write failing tests: teardown must derive the fleet Prime repo path when the row carries none, must prefer a persisted path, and must leave project-owned sessions alone
- [x] 3.3 Derive the repo path from role identity in teardown, reusing the existing helper the restore paths already use
- [x] 3.4 Point Kill, RetireForReplacement, save-and-teardown, and Cleanup at the deriving helper
- [x] 3.5 Add a real-git integration test that a projectless Prime worktree holding the canonical branch is torn down and the branch released

## 4. Reconciliation primitives in the session manager

- [x] 4.1 Write failing tests for reaping a leaked runtime belonging to a terminated role session, and for leaving a live role runtime alone
- [x] 4.2 Expose the existing boot reaper logic as a role-targeted reap callable outside boot reconcile
- [x] 4.3 Write failing test: a terminated role session holding the canonical role branch is released before a replacement is created
- [x] 4.4 Implement branch-keyed stale-role-resource release using the force path from group 3
- [x] 4.5 Add a failing test then fix: an Orchestrator replacement must not silently adopt a dead worktree on the canonical path

## 5. The shared `ReconcileRole` operation

- [x] 5.1 Write failing tests: reconciling a role target whose newest row is terminated produces an active replacement, for both Prime and Orchestrator
- [x] 5.2 Implement the role reconcile operation (resolve active → reap → release stale → create) under the role lock
- [x] 5.3 Rewrite orchestrator spawn and Prime spawn as thin callers of the reconcile operation
- [x] 5.4 Write failing test then converge the supervisor's missing-Prime and unhealthy-Prime paths onto the reconcile operation
- [x] 5.5 Add an idempotence test: reconciling a healthy role target creates nothing and preserves the singleton
- [x] 5.6 Add a concurrency test asserting at most one non-terminated Prime survives competing reconciles

## 6. Restart budget clearing and the Prime relaunch API

- [x] 6.1 Write failing tests for clearing budget/backoff state and for waking the supervisor loop immediately
- [x] 6.2 Add an external clear-and-poke signal to the Prime supervisor, reusing the existing reconcile-poke pattern
- [x] 6.3 Give the Prime service the session dependency it needs to drive reconciliation
- [x] 6.4 Write failing controller test then add the Prime relaunch endpoint, keeping manual Prime spawn and Prime restore rejected
- [x] 6.5 Raise the clear-and-poke signal from Prime settings save so an off/save/on/save cycle reliably restarts Prime
- [x] 6.6 Update the budget-exhausted notification copy to point at the Prime surface / relaunch action
- [x] 6.7 Regenerate the OpenAPI spec and TypeScript client (`npm run api`) and commit the generated output with the DTO change

## 7. Prime presence and relaunch in the supervisor UI

- [x] 7.1 Write failing tests: the Prime nav entry is present when settings report Prime enabled and no live Prime row exists, and absent when Prime is disabled
- [x] 7.2 Drive Prime workspace synthesis and nav visibility from persisted Prime settings, using live rows only to choose the displayed state
- [x] 7.3 ~~Update the existing active-Prime selector test~~ — not needed: `findFleetPrime` ignoring terminated primes is still correct (it keeps navigation off dead terminals); a terminated-inclusive selector proved unnecessary — the recovery surface uses `findFleetPrime` plus `primeSurfaceState`, and the extra selector was removed in review as dead code
- [x] 7.4 Add a Prime relaunch client helper mirroring the existing orchestrator restart helper
- [x] 7.5 Write failing test then add the Prime not-running surface with one primary `Relaunch Prime` action
- [x] 7.6 Write failing test then make an ended Prime terminal offer relaunch instead of the generic restore strip
- [x] 7.7 Correct Prime settings copy to `Enable Prime` and to "Prime supervises globally"

## 8. Orchestrator recovery affordances

- [x] 8.1 Write failing test then give the missing-Orchestrator board state an action that spawns or restarts the Orchestrator
- [x] 8.2 Write failing test then route ended-Orchestrator terminal and restore-unavailable states through the orchestrator spawn/restart path
- [x] 8.3 Assert the recovery flow does not resolve by navigating back to the dead session's terminal

## 9. Verification and gate

Live verification found two real defects that unit tests did not: relaunch tried
to spawn while Prime was disabled (now a 409 `PRIME_DISABLED`), and a settings
save did not reconcile until the next 30s tick (task 6.5 had been checked off
without being implemented).

- [x] 9.1 Run the full local CI-parity gate (`npm run ci-local`)
- [x] 9.2 Exercise the recovery end to end against a running daemon: kill Prime from its terminal, confirm automatic or relaunch recovery with no manual git/tmux work
- [x] 9.3 Exercise the Orchestrator equivalent: exit an Orchestrator from its terminal, relaunch from the UI, then confirm cleanup leaves the live replacement's worktree intact
- [x] 9.4 Drive the Prime not-running surface and relaunch action in the browser and capture the result
- [ ] 9.5 Run `/final-review` and record the verdict as a SHA-pinned commit status
