## Why

Issue #3 was reopened after PR #17 because the round-1 implementation and archived `weighted-worker-mix` spec were built from prose rather than the reference implementation. The reopened operator contract is now PORT -> FIT -> IMPROVE -> FIX: lift the accumulated reference behavior from `~/agent-orchestrator-fscked`, adapt it to current `main`, preserve verified local improvements, and explicitly document any new deltas.

## What Changes

- Replace the round-1 worker-mix behavior that conflicts with the reference: down buckets must be debit-preserved in the census rather than silently redistributing share, and a model-only unpinned worker spawn must still use the mix to select a harness.
- Serialize worker spawn admission across cap checks, worker-mix census/selection, model reachability validation, and seed-row creation so concurrent spawns cannot breach the cap or double-select the same bucket.
- Keep the current post-#4 model validation engine, but wire worker-mix buckets and spawn-time selections to the reference's three-way policy: definitive unreachable rejects, probe-unavailable/unknown fails open loudly, and spawn refusal names the pin source.
- Extend tracker intake deferral parity so a full worker pool and exhausted worker mix are treated as retryable deferrals without putting the project into genuine failure backoff; once the pool is full in a pass, later matching issues are short-circuited until the next poll.
- Mark the selected worker-mix candidate down on every attributable launch failure path covered by the reference while preserving current improvements around prompt policy, workspace cleanup, launch liveness, and session lifecycle.
- Tighten the worker-mix settings card so save is blocked for row-level invalid buckets as well as bad totals, while continuing to derive agent/model options from the live agent catalog and model availability APIs introduced by issue #4.
- Add the missing first-class CLI path for the worker cap without touching the issue #14 coordination exclusions (`cli/projectconfig*`, project config controller path).
- Record a port manifest and verification checklist with reference/current file:line citations so review can falsify that the reference was actually studied.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `worker-mix`: Correct selection, pinning, down-bucket debit, census, and editor validation behavior to match the reopened parity contract.
- `spawn-concurrency-cap`: Serialize admission and align tracker-intake cap behavior with the reference pass-level deferral.
- `spawn-model-selection`: Require config-write and spawn-time model selection validation for worker-mix buckets and resolved launch tuples, including source-labelled failures.
- `candidate-health`: Preserve the exact candidate-health semantics while wiring all attributable worker-mix launch failures into the tracker.

## Impact

- Backend spawn path: `backend/internal/session_manager/manager.go` and tests.
- Project config/model validation: `backend/internal/service/project/service.go` and tests, reusing the current issue #4 validation engine.
- Tracker intake: `backend/internal/observe/trackerintake/observer.go` and tests.
- CLI/project config surface for max live workers, avoiding the paths reserved for issue #14 coordination.
- Frontend worker-mix settings UI: `frontend/src/renderer/components/WorkerMixFields.tsx`, `ProjectSettingsForm.tsx`, and tests.
- OpenSpec deltas for `worker-mix`, `spawn-concurrency-cap`, `spawn-model-selection`, and `candidate-health`.
