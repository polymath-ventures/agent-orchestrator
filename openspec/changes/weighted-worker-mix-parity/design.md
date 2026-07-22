## Context

Issue #3 was reopened by operator order on 2026-07-21 because PR #17 shipped a round-1 implementation without using the reference fork as the source of truth. The active ticket reading order is now the strategy revision, the verified gap list, then the history-mined edge cases. The archived `weighted-worker-mix` change remains useful as baseline context, but it is not terminal and some of its requirements are now known to be wrong.

Reference evidence checked in this pass:

- Spawn admission in the reference holds `spawnMu` across cap check, mix census/selection, model validation, seed-row creation, and the early launch setup (`agent-orchestrator-fscked/backend/internal/session_manager/manager.go:548-663`).
- Current `main` performs cap and mix selection without an equivalent serialization guard (`backend/internal/session_manager/manager.go:341-412`).
- Reference mix census counts every live worker by bucket and folds down-bucket skip debit into the census (`agent-orchestrator-fscked/backend/internal/session_manager/manager.go:1062-1090`).
- Current `main` counts only `MixSelected` rows in `mixCensus`, and narrows down buckets before census, which redistributes their share (`backend/internal/session_manager/manager.go:797-859`).
- Reference config-write validation checks worker-mix bucket models through a three-way validator (`agent-orchestrator-fscked/backend/internal/service/project/service.go:703-735`).
- Current `main` already has a stronger post-#4 selection validator for configured model selections, including worker-mix buckets (`backend/internal/service/project/service.go:654-786`), so this work should reuse it rather than porting the older API shape.
- Reference tracker intake memoizes a full worker pool during a poll pass and treats allocator deferrals as retryable (`agent-orchestrator-fscked/backend/internal/observe/trackerintake/observer.go:232-299`).
- Current `main` treats cap and pause as deferrals but keeps attempting later issues in the same pass and does not handle worker-mix exhaustion as a deferral (`backend/internal/observe/trackerintake/observer.go:221-242`).
- Reference launch failure paths mark a selected mix bucket down for workspace provisioning, launch command, binary, runtime, launch-process, and prompt-delivery failures (`agent-orchestrator-fscked/backend/internal/session_manager/manager.go:707-816`).
- Current `main` marks down some launch paths but misses `prepareWorkspace`, prompt-delivery, and after-start prompt delivery (`backend/internal/session_manager/manager.go:458-554`).
- Current UI already derives bucket agent/model controls from dynamic catalog and model availability (`frontend/src/renderer/components/WorkerMixFields.tsx:186-212`), but save blocking still only checks weight totals (`frontend/src/renderer/components/WorkerMixFields.tsx:84-89`, `ProjectSettingsForm.tsx:280`).

## Goals / Non-Goals

**Goals:**

- Implement the reopened parity requirements by porting reference behavior first, adapting only seams needed by current `main`.
- Preserve current improvements from #4/#6 and later main, especially the agent/model availability engine and the richer `ValidateModelSelection`/`ValidateSpawnSelection` APIs.
- Make every implemented requirement traceable to reference file:line evidence or to an explicit "current main is already better" decision.
- Keep the fix rebase-friendly and avoid touching the issue #14 reserved paths: `cli/projectconfig*` and project config controller code.

**Non-Goals:**

- No capacity dashboard.
- No tracker label routing.
- No behavior-version convergence.
- No attention system.
- No usage-based scheduling.
- No Slack behavior change.
- No churn in byte-faithful `domain/workermix.go` or `candidatehealth` core unless a failing test proves a parity problem there.

## Decisions

1. **Serialize admission in `session_manager.Manager.Spawn`.** Add a spawn admission mutex or port the reference guard so the live-worker cap, worker-mix census/selection, down-bucket debit, spawn-time model validation, and session seed-row creation are a single critical section. Alternative considered: only serialize the cap check. Rejected because the reopened gap covers both cap breach and double-selected buckets; the census and seed-row write must be protected together.

2. **Count actual live worker buckets, not only `MixSelected` rows.** Rework census to use each live worker's persisted `(harness, model, effort)` tuple and fold candidate-health skip debit into the count before D'Hondt selection. Alternative considered: keep `MixSelected` so explicit pins never affect distribution. Rejected because the operator reopened this as a verified parity gap; actual live capacity in a bucket is the authoritative distribution input.

3. **Debit-preserve down buckets.** Keep down buckets in the configured mix and account for their skip debit in the census; if a down bucket is selected, refuse loudly rather than silently reallocating its share. Alternative considered: current survivor-only selection. Rejected because it redistributes a broken bucket's allocation to healthy buckets and hides the outage's capacity cost.

4. **Reuse current model validation APIs.** The reference used `ValidateModel`/`ValidateSpawnModel`, but current `main` has richer `ValidateModelSelection` and `ValidateSpawnSelection` paths that cover model plus effort and avoid paid probes at spawn. Port the policy, not the exact interface. Alternative considered: transplant the old service code. Rejected because it would regress issue #4's availability engine.

5. **Model-only worker spawn selects harness from the mix.** Treat an explicit harness as the mix bypass. Treat an explicit model without a harness as an override of the selected bucket model after the mix chooses the harness. Alternative considered: current "any model pin bypasses mix" behavior. Rejected by gap 9 and reference lines `manager.go:577-592`.

6. **Only candidate-attributable failures mark a bucket down.** Port the reference's broad launch-path coverage, while preserving the current distinction for non-candidate errors such as prompt/config failures. The final implementation must document each mark-down path in tests.

7. **Patch UI validation, not the whole card.** Current UI already satisfies the dynamic catalog requirement. The remaining UI change is row-level client-side save blocking for empty agents and bad weights, plus regression tests.

## Risks / Trade-offs

- **Risk: serialized spawn admission reduces parallel spawn throughput.** Mitigation: the protected section should end once the seed row captures the admitted bucket; expensive runtime startup should stay outside the lock when safe, matching the reference's unlock point.
- **Risk: census behavior change may affect existing tests that assumed pinned workers do not consume share.** Mitigation: update the spec and tests to the reopened contract and call out the intentional behavior change in the PR.
- **Risk: reference and current main use different model validation shapes.** Mitigation: add tests around current interfaces rather than forcing the old interface back in.
- **Risk: issue #14 may overlap CLI/config surfaces.** Mitigation: avoid `cli/projectconfig*` and project config controller paths; add only the narrow cap flag where coordination permits, or explicitly defer it if the safe path is blocked.
