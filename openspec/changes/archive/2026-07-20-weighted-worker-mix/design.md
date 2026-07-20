## Context

This change ports fleet allocation from an earlier fork of this codebase (`~/agent-orchestrator-fscked`), where `domain/workermix.go` and `internal/candidatehealth/` ran in production. The reference implementations are available and carry their own test files, so the design question is not "what algorithm" but "how does it attach to _this_ fork's spawn path", which has diverged.

Current state, as mapped against the fork's `main`:

- `Manager.Spawn` resolves a harness through `effectiveHarness(explicit, kind, cfg)`: explicit wins, else the role override's agent, else an error. `cfg.Harness == ""` inside the manager is therefore already an exact "unpinned" signal.
- The model travels separately, through `effectiveAgentConfig` into `LaunchConfig`, and is **never persisted**. `ports.SpawnConfig`, the `SpawnSessionRequest` DTO, `domain.SessionRecord`, and the `sessions` table all carry harness and no model.
- `ao spawn` resolves the harness **client-side** and hard-errors when `worker.agent` is unset, so an unpinned request never reaches the daemon today.
- There is no live-worker count primitive — census is `ListSessions` plus an in-Go `!IsTerminated` filter — and no concurrency cap of any kind.
- Tracker intake is already an unpinned spawn site and already owns a per-project failure-backoff map.

Two constraints shape everything below. The fork must stay rebase-clean on upstream, so changes are deltas on upstream files rather than transplants. And generated artifacts (`openapi.yaml`, `schema.ts`, sqlc output) are CI-enforced, so any DTO or schema change carries a regeneration step.

## Goals / Non-Goals

**Goals:**

- Port `workermix` and `candidatehealth` close to verbatim, preserving their tested semantics, so the fork inherits proven behavior rather than a reimplementation.
- Attach selection at the one place unpinned-ness is already known, rather than threading a new concept through the spawn path.
- Make the realized spawn distribution provably match the configured ratio, verified by a deterministic test rather than asserted in prose.
- Keep the no-silent-substitution guarantee mechanically true: a down bucket is excluded, never swapped.
- Keep each change small enough to be an upstream-submittable delta.

**Non-Goals:**

- Porting the old fork's 4,450-line session manager. The wiring is rewritten against upstream's current flow.
- The old fork's `RoutingHarnessForIssueLabels` label-routing convention. Excluded deliberately.
- Model prevalidation of bucket pins at config-write time. That arrives with the model-management work.
- Automatic recovery of down candidates — no TTL, no backoff, no half-open probe. See the decision below.
- Persisting candidate health across daemon restarts.

## Decisions

### Selection hooks the manager; telemetry hooks the service

Selection is inserted in `Manager.Spawn` where `cfg.Harness == ""` is already detected, because that is the single point where every spawn path — API, CLI, tracker intake — has converged and where the project config is already loaded.

Candidate-health _telemetry_, however, is emitted at the service layer. The manager has no telemetry dependency in its `Deps`, while `Service.Spawn` already funnels every manager error through one emitter. Adding a sink to the manager purely for this would widen its dependency surface for no gain. **Alternative rejected:** injecting `ports.EventSink` into `Manager.Deps` — simpler to write, but it duplicates an emission point the service already owns and makes the manager harder to test.

### Only launch-attributable failures mark a bucket down

`Manager.Spawn` has seventeen error returns. Only two are evidence that a harness or model is broken: the agent binary not being on `PATH`, and the runtime refusing to create. `ErrMissingHarness` and `ErrUnknownHarness` are configuration errors, and the store/workspace/prompt failures are environmental. Marking down on all of them would take a bucket out of rotation because the disk was full.

**Alternative rejected:** mark down on any `Spawn` error. Cheaper to wire, but it makes the circuit breaker fire on conditions the candidate cannot cause, which is precisely the failure the no-substitution rule exists to prevent.

### Attribution keys on caller-context state, not error identity

The reference implementation distinguishes "the caller gave up" from "the candidate timed out" by checking whether the _caller's_ context is still live, not by inspecting the error. An error wrapping `context.DeadlineExceeded` from a candidate's own startup probe, raised while the caller's context is active, is a real candidate fault.

This is worth calling out because the originating issue describes the rule as "context-cancel/deadline = no-op", which is too broad — implementing that literally would silently stop marking down a whole class of genuine failures. Both branches are pinned by ported tests.

### No TTL on down state — inherited deliberately

Down state persists until an explicit recovery following a successful attempt on that exact candidate. The transition time is recorded but never read for a decision.

This is the one place the design most invites an addition, so the rationale is recorded: a TTL would re-admit a known-broken bucket on a timer, producing a periodic burst of failed spawns and re-alerting — a detector-and-retry loop layered over a state we already know. Recovery driven by a real successful spawn is the property kept by construction. Restart clearing the state is an acceptable consequence: a restart re-probes everything anyway. **Alternative rejected:** exponential backoff with a half-open probe. More machinery, and it converts a quiet correct state into a recurring background failure source.

### Model becomes a first-class spawn input

Buckets are keyed on `(harness, model)`, so the census must count live workers per `(harness, model)`. Harness is on the session row; model is not persisted anywhere. Without persisting it, D'Hondt cannot see its own prior selections and the distribution does not converge.

The change is therefore: add `Model` to `ports.SpawnConfig` and `SpawnSessionRequest`, add a `model` column to `sessions` via a new migration with a matching `sqlc.yaml` override, thread it through `seedRecord`, and add `--model` to `ao spawn`. **Alternative rejected:** deriving each live session's model by re-reading project config at census time. It is wrong whenever config changed after a session launched, which is exactly when the census matters.

### Harness resolution moves server-side

`ao spawn`'s client-side resolution must go, or a mix-only project is rejected before a request is built. The CLI sends what the user pinned — possibly nothing — and the daemon resolves. The agent-auth preflight moves with it, since the client no longer knows which harness will run. This also removes a duplicated resolution rule, leaving one authority.

### Census reuses list-and-filter

Per-bucket counts come from the existing `ListSessions` plus `!IsTerminated`, grouped by `(harness, model)` in Go. No count query, no index on `is_terminated`. This matches the existing cost profile — the manager already does a full project scan to find the active orchestrator — and avoids a schema change whose need is unproven. **Alternative rejected:** a `COUNT ... GROUP BY` sqlc query. Better asymptotically, but it optimizes a scan the codebase already performs per spawn; defer until a profile says otherwise.

### Cap is checked pre-write and deferred at intake

The cap check runs before any durable state is created, so a refusal leaves nothing to roll back. It returns a distinguishable error so tracker intake can tell capacity from failure: intake `continue`s **without** marking the issue seen (so the next poll retries) and **without** setting its failure flag (so the five-minute project backoff does not engage). Being at capacity is a healthy steady state; backing off would stall a project that is simply busy.

### `modelprovider.go` ports alongside `workermix.go`

`WorkerMix.Validate()` calls `AgentHarness.ModelProvider()`, `ClassifyModelProvider()`, and `ModelProvider.CompatibleWith()` — none of which exist in this fork. The claim in the originating issue that `workermix.go` is grep-clean holds for its _imports_ but not for its same-package symbol references; it does not compile as a drop-in.

`domain/modelprovider.go` therefore ports too, dropping the `Fugu` provider and `HarnessCodexFugu` cases (neither harness exists here), and `ModelProvider()` is added to `domain/harness.go`. **Alternative rejected:** dropping the cross-provider check from `Validate()`. It is a smaller port, but it lets a mix pin an Anthropic model to a codex bucket and validate clean, and it would require deleting assertions from the ported test file — weakening a tested guarantee to save one small file.

### Project config needs no migration

Project config is stored as a single JSON blob with whole-struct marshal/unmarshal, so `workerMix` and the cap land on `domain.ProjectConfig` with their own `WithDefaults()`/`Validate()` wired into the parent, following the existing tracker-intake precedent. The defaults must stay zero-valued: `ProjectConfig.IsZero()` uses `reflect.DeepEqual`, and a non-zero default would break the invariant that an unset config stores SQL `NULL`.

### Settings UI is a card delta

A worker-mix card is added to `ProjectSettingsForm.tsx` following the extracted-subcomponent pattern the tracker-intake section already uses, keeping the multi-row editor out of the main form body. Validation follows the file's existing manual-inline convention rather than introducing a schema library. The save gate blocks at any weight sum other than 100, mirroring the backend rule — the backend remains authoritative; the client check is only for feedback.

## Risks / Trade-offs

- **Model plumbing touches CI-enforced generated artifacts** → `npm run sqlc` and `npm run api` run as part of the phase that changes the DTO, and their output is committed in the same commit. The `api-drift` CI job and the embedded-spec test catch a miss.
- **The migration is the only irreversible step** → the column is nullable and additive with a down-migration following the existing ALTER pattern; no existing row is rewritten and old rows read back with an empty model.
- **Duplicating the weight-sum rule in the UI and the backend** → accepted, with the backend as sole authority; the client copy is advisory feedback only and cannot admit an invalid mix.
- **Moving harness resolution server-side changes `ao spawn`'s error surface** → a project with neither a mix nor a worker agent now fails at the daemon rather than in the client. Covered by a spec scenario so the message stays diagnosable.
- **Health state is in-memory** → a restart re-admits every bucket and the first spawn to a still-broken bucket fails once and re-marks it. Accepted: one failed spawn per bucket per restart, in exchange for no persistence layer.
- **D'Hondt converges over a run of spawns, not on any single one** → the acceptance test asserts the ratio over a full apportionment cycle, not per call.

## Migration Plan

Land in the order the dependencies force: the two ported packages first (no upstream file touched, fully testable in isolation), then model plumbing plus migration and regeneration, then manager wiring and candidate-health attachment, then the cap and its intake deferral, then the settings card. Each step keeps the tree green, and every step before the last is inert to a user until the mix is configured — the feature is off by default and unconfigured projects follow the existing path unchanged, which is the rollback story.

## Open Questions

None blocking. Two deferred by decision: model prevalidation of bucket pins at config-write time belongs to the model-management work, and per-bucket census via a SQL aggregate is deferred until a profile justifies it.
