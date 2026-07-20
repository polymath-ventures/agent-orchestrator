## Why

Every worker spawn today takes whatever the project or role config names: `effectiveHarness` returns the explicit harness, else `worker.agent`, else fails. There is no way to express "run this fleet as 60% claude/opus, 30% codex, 10% claude/haiku" — and no way to keep that ratio honest when one of those buckets is broken.

Two failure modes follow. A fleet cannot be diversified across harnesses and models without an operator manually pinning each spawn, so capacity planning and cost mixing are unavailable. And when a bucket stops working — the agent binary is missing, the runtime refuses the launch — the system has no memory of it: it retries the dead bucket on the next spawn, and the operator learns about it only from a pile of failed sessions. There is also no ceiling on live workers per project, so a busy tracker-intake poll can spawn until the host runs out of resources.

This change makes unpinned allocation an explicit, deterministic policy with a circuit breaker and a hard cap.

## What Changes

- **Weighted worker mix.** Projects gain an optional `workerMix`: a list of `{agent, model, weight}` buckets whose weights must sum to 100. Unpinned worker spawns select a bucket by D'Hondt highest-averages apportionment against the live per-bucket census, so the realized distribution converges on the configured ratio. Selection is deterministic and stateless — same mix plus same running counts always yields the same bucket, with ties breaking to the earliest configured row. When no mix is configured, behavior is unchanged.
- **Candidate health circuit breaker.** A transport-agnostic tracker marks a failing candidate down on the spawn error paths, emits a notification-intent telemetry event on the _transition_ into down, and debits every subsequent skip. A down bucket's share redistributes across the remaining buckets; it is **never silently substituted**. A later successful spawn on that exact candidate recovers it. Down state is in-memory, has no TTL and no auto-recovery — recovery is caller-driven only.
- **Spawn concurrency cap.** An optional per-project ceiling on concurrently-live workers. At the cap, tracker intake **defers** (leaves the issue unclaimed so the next poll retries it) rather than erroring, and deliberately does not trip the existing per-project failure backoff — a capacity condition is not a project fault.
- **Model as a first-class spawn input.** Mix buckets are keyed on `(harness, model)`, but this fork records only harness on a session. Spawn requests gain an optional model, sessions persist it, and `ao spawn` gains `--model`. Without this the per-model census that D'Hondt needs cannot be computed. **BREAKING (internal):** `ports.SpawnConfig` and `SpawnSessionRequest` gain a field; the `sessions` table gains a column.
- **Pinning stays absolute.** An explicit `--agent`/`--model` spawn bypasses selection entirely and never consumes mix share.
- **Settings UI.** A worker-mix card on the project settings form — bucket rows, a live weight total, and a save gate that blocks at any sum other than 100.

Explicitly **not** included: the old fork's `RoutingHarnessForIssueLabels` label-routing convention is excluded from the port. Model prevalidation of bucket pins at config-write time is deferred to the model-management work.

## Capabilities

### New Capabilities

- `worker-mix`: Weighted, deterministic allocation of unpinned worker spawns across `{harness, model, weight}` buckets, including mix validation and the pinned-spawn bypass.
- `candidate-health`: Marking spawn candidates down on failure, skip debiting, transition-only alerting, and explicit recovery — the no-silent-substitution guarantee.
- `spawn-concurrency-cap`: A per-project live-worker ceiling and its deferral (not error) semantics at tracker intake.
- `spawn-model-selection`: Model as an explicit spawn input, persisted on the session record and settable from the CLI and API.

### Modified Capabilities

None. `openspec/specs/` is currently empty — this is the repo's first change, so every capability above is new.

## Impact

**New backend packages.** `backend/internal/domain/workermix.go` and `backend/internal/candidatehealth/` port near-verbatim from the old fork (`~/agent-orchestrator-fscked`) with their test files. `candidatehealth` is genuinely dependency-clean: its only non-stdlib import is `internal/ports`, whose `telemetry.go` is byte-identical across the two forks. `workermix.go` is **not** — its `Validate()` calls `AgentHarness.ModelProvider()`, `ClassifyModelProvider()`, and `ModelProvider.CompatibleWith()`, none of which exist here, so `domain/modelprovider.go` ports alongside it (dropping the `Fugu` provider and `HarnessCodexFugu` cases, neither of which exists in this fork) and `AgentHarness.ModelProvider()` is added to `domain/harness.go`.

**Spawn path.** `session_manager/manager.go` is re-threaded, not transplanted — the old fork's 4,450-line manager is not brought across. Selection hooks where `cfg.Harness == ""` is already detected; bucket-down marking hooks the launch-failure paths that indicate a broken candidate (agent binary not on PATH, runtime create refused) and not the config errors (`ErrMissingHarness`, `ErrUnknownHarness`). Telemetry belongs at the service layer, which already funnels every spawn error through one emitter; the manager has no telemetry dependency.

**Census.** Live-per-bucket counting reuses the existing list-and-filter pattern (`!IsTerminated`); no count query or new index is introduced, matching the current cost profile.

**CLI.** `ao spawn` currently resolves the harness client-side and hard-errors when `worker.agent` is unset, so an unpinned request never reaches the daemon. That resolution moves server-side so a mix-only project is spawnable; the agent-auth preflight becomes mix-aware.

**Storage and generated artifacts.** One new migration adds the session model column, with a matching `sqlc.yaml` override. `npm run sqlc` and `npm run api` regenerate the sqlc output, `openapi.yaml`, and `frontend/src/api/schema.ts` — all CI-enforced and never hand-edited. Project config needs no migration: it is stored as a single JSON blob, so the new field lands on `domain.ProjectConfig` with its own `WithDefaults()`/`Validate()` wired into the parent, following the existing tracker-intake precedent. A non-zero default must be avoided so the "unset config stores SQL NULL" invariant holds.

**Frontend.** A delta on `ProjectSettingsForm.tsx` — a new card following the existing extracted-subcomponent precedent used by the tracker-intake section. Not a form transplant.

**Risk.** The largest surface is the model plumbing, which touches CI-enforced generated files and a migration. The circuit breaker's lack of a TTL is a deliberate inherited property, not an omission: a bucket stays down until a successful spawn recovers it, and state resets on daemon restart. Any auto-recovery would be a new requirement, specified separately.
