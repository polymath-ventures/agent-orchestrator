## 1. Spawn Admission Serialization

- [x] 1.1 Add failing concurrency tests proving cap check, worker-mix selection, and seed-row creation are serialized under concurrent worker spawns (ref `agent-orchestrator-fscked/backend/internal/session_manager/manager.go:548-663`; current `backend/internal/session_manager/manager.go:341-412`).
- [x] 1.2 Port/adapt the reference spawn admission lock so expensive runtime launch remains outside the lock when safe, but admission state cannot race.
- [x] 1.3 Verify pinned/orchestrator behavior is unchanged and capacity refusals create no session rows or workspaces.

## 2. Worker Mix Census And Down-Bucket Debit

- [x] 2.1 Add failing tests proving census counts all live workers by persisted `(harness, model, effort)`, including pinned workers in configured buckets (ref `agent-orchestrator-fscked/backend/internal/session_manager/manager.go:1062-1080`; current `backend/internal/session_manager/manager.go:831-859`).
- [x] 2.2 Add failing tests proving down-bucket skip debit is folded into the census instead of removing the bucket before selection (ref `agent-orchestrator-fscked/backend/internal/session_manager/manager.go:582,1082-1090`; current `backend/internal/session_manager/manager.go:797-820`).
- [x] 2.3 Port/adapt census and skip-debit behavior without changing `domain/workermix.go` unless a regression test proves the core selector itself is wrong.

## 3. Spawn Model Selection Parity

- [x] 3.1 Add failing tests proving a model-only worker spawn on a mix project uses the mix to select a harness, then launches the explicit model (ref `agent-orchestrator-fscked/backend/internal/session_manager/manager.go:577-592`; current `backend/internal/session_manager/manager.go:696-733`).
- [x] 3.2 Add failing tests proving spawn-time validation rejects definitive unreachable resolved tuples before durable state and names the selection source (ref `agent-orchestrator-fscked/backend/internal/session_manager/manager.go:626,1005-1045`; current `backend/internal/session_manager/manager.go:762-785`).
- [x] 3.3 Adapt the current #4 validation engine rather than porting the old validator interface, preserving fail-open for unknown/probe-unavailable verdicts.

## 4. Candidate Health Wiring

- [x] 4.1 Add failing tests for mark-down on selected worker-mix candidate failures in workspace preparation, missing binary launch command, runtime creation, launch liveness, and after-start prompt delivery (ref `agent-orchestrator-fscked/backend/internal/session_manager/manager.go:707-816`; current `backend/internal/session_manager/manager.go:458-554`).
- [x] 4.2 Implement the missing mark-down calls before rollback can obscure the caller context, while preserving non-candidate refusals as no-ops.
- [x] 4.3 Verify successful launch recovers the exact candidate and does not recover similar buckets.

## 5. Tracker Intake Deferrals

- [x] 5.1 Add failing tests proving worker cap deferral memoizes `workerPoolFull` for the rest of the poll pass (ref `agent-orchestrator-fscked/backend/internal/observe/trackerintake/observer.go:232-299`; current `backend/internal/observe/trackerintake/observer.go:221-242`).
- [x] 5.2 Add failing tests proving `WORKER_MIX_EXHAUSTED` is treated as a retryable deferral rather than a genuine spawn failure/backoff.
- [x] 5.3 Implement the intake deferral changes without restoring ruled-out tracker label routing.

## 6. Config And UI Validation

- [x] 6.1 Add/adjust project config tests proving worker-mix buckets use current `ValidateModelSelection` semantics: definitive unreachable rejects, unknown/unavailable warns and stores.
- [x] 6.2 Add frontend tests proving empty worker-mix agents and invalid row weights block Save changes before submission.
- [x] 6.3 Implement the row-level UI save gate while preserving dynamic agent/model catalog behavior.
- [x] 6.4 Add the safe first-class CLI cap flag if it can be done without touching issue #14 reserved paths; otherwise record the exact blocker in the PR. Not implemented: the safe CLI path is `backend/internal/cli/projectconfig*`, explicitly reserved to #14 by the reopened issue.

## 7. Verification And Handoff

- [x] 7.1 Run focused backend tests for session manager, project service, tracker intake, and domain worker mix.
- [x] 7.2 Run frontend worker-mix settings tests and typecheck.
- [x] 7.3 Run `openspec validate --all --strict --no-interactive`.
- [ ] 7.4 Update the PR body with the history-mined edge-case checklist, citing implementation file:line evidence or explicit non-applicability for each item.
