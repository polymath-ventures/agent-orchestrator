## 1. Port the model-provider dependency

- [ ] 1.1 Port `backend/internal/domain/modelprovider.go` from the old fork, dropping the `ProviderFugu` case and any harness constants absent from this fork
- [ ] 1.2 Port its test file and add the `ModelProvider()` method to `domain/harness.go`
- [ ] 1.3 Verify `go build ./...` and `go test ./internal/domain/...` pass before touching workermix

## 2. Port the worker mix domain package

- [ ] 2.1 Port `workermix_test.go` from the old fork first, excluding any `RoutingHarnessForIssueLabels` cases — this is the failing-test step
- [ ] 2.2 Port `backend/internal/domain/workermix.go`, excluding `issueRoutingLabelHarnesses` and `RoutingHarnessForIssueLabels`
- [ ] 2.3 Add a convergence test asserting a 60/30/10 mix apportions exactly 6/3/1 over ten successive selections
- [ ] 2.4 Add a determinism test asserting identical (mix, census) inputs return the same bucket, and a tie test asserting the earliest row wins
- [ ] 2.5 Verify `go test ./internal/domain/...` and `go vet ./...` pass

## 3. Port the candidate health package

- [ ] 3.1 Port `candidatehealth_test.go` from the old fork verbatim — failing-test step
- [ ] 3.2 Port `backend/internal/candidatehealth/candidatehealth.go` verbatim; confirm `internal/ports` is its only non-stdlib import
- [ ] 3.3 Confirm the two attribution tests pass unmodified: canceled caller context is a no-op, and a wrapped deadline error with a live caller context marks down
- [ ] 3.4 Verify `go test -race ./internal/candidatehealth/...` passes

## 4. Make model a first-class spawn input

- [x] 4.1 Write failing tests for model round-tripping through spawn, persistence, and read-back
- [x] 4.2 Add `Model` to `ports.SpawnConfig` and to the `SpawnSessionRequest` DTO
- [x] 4.3 Add migration `0025` adding a nullable `model` column to `sessions`, with the down-migration ALTER following the `0019` pattern
- [x] 4.4 Run `npm run sqlc` (no `sqlc.yaml` override needed: `model` is a plain `string`, unlike the columns that map to named `internal/domain` types)
- [x] 4.5 Add `Model` to `domain.SessionRecord` and thread it through `seedRecord` and `effectiveAgentConfig`
- [x] 4.6 Run `npm run api` and commit the regenerated `openapi.yaml` and `frontend/src/api/schema.ts` in the same commit
- [x] 4.7 Verify pre-existing sessions read back with an empty model, and that the `api-drift` CI job passes locally

## 5. Move harness resolution server-side

- [x] 5.1 Write a failing test asserting a mix-only project (no `worker.agent`) is spawnable
- [x] 5.2 Remove `resolveSpawnHarness`'s client-side resolution from `cli/spawn.go` so an unpinned request transmits an empty harness
- [x] 5.3 Add the `--model` flag to `ao spawn` and plumb it into the request body
- [x] 5.4 Move the agent-auth preflight so it runs against the daemon-resolved harness
- [x] 5.5 Verify the unresolvable case (no mix, no worker agent) still fails with a diagnosable error, now raised by the daemon

## 6. Wire selection into the spawn path

- [x] 6.1 Add `workerMix` to `domain.ProjectConfig` with its own `WithDefaults()`/`Validate()` wired into the parent, keeping the default zero-valued
- [x] 6.2 Write a failing test asserting an unpinned spawn on a configured mix selects by weight and a pinned spawn bypasses it
- [x] 6.3 Implement the live per-bucket census via `ListSessions` filtered on `!IsTerminated`, grouped by `(harness, model)`
- [x] 6.4 Insert selection in `Manager.Spawn` at the existing `cfg.Harness == ""` branch
- [x] 6.5 Verify pinned spawns neither consume mix share nor consult the mix

## 7. Attach candidate health to the spawn path

- [x] 7.1 Write failing tests asserting mark-down on the two launch-attributable failures only, and no mark-down on config or environmental errors
- [x] 7.2 Mark the selected bucket down on the agent-binary-missing and runtime-create-refused paths, passing the attempt context so caller cancellation is excluded
- [x] 7.3 Recover the bucket on a successful spawn of that exact candidate
- [x] 7.4 Emit candidate-health telemetry from the service layer alongside the existing spawn emitters, not from the manager
- [x] 7.5 Verify a down bucket's share redistributes and that an all-buckets-down mix fails loudly instead of substituting

## 8. Spawn concurrency cap

- [ ] 8.1 Write failing tests for cap refusal, for orchestrator sessions not counting, and for capacity freeing on termination
- [ ] 8.2 Add the cap to `domain.ProjectConfig` with validation, defaulting to unset/unbounded
- [ ] 8.3 Enforce the cap in `Manager.Spawn` before any durable write, returning a distinguishable capacity error
- [ ] 8.4 Verify a cap refusal marks no candidate down and emits no candidate-down event

## 9. Tracker intake deferral

- [ ] 9.1 Write a failing test asserting a capped intake leaves the issue unclaimed and does not enter project backoff
- [ ] 9.2 Handle the capacity error in the intake observer: continue without marking the issue seen and without setting the failure flag
- [ ] 9.3 Verify a genuine spawn failure still triggers the existing five-minute backoff unchanged
- [ ] 9.4 Verify a deferred issue is picked up on a later poll once capacity frees

## 10. Settings UI

- [ ] 10.1 Add a `WorkerMixFields` subcomponent following the extracted-subcomponent pattern used by the tracker-intake section
- [ ] 10.2 Add the worker-mix card to `ProjectSettingsForm.tsx` with bucket rows and a live weight total
- [ ] 10.3 Gate save on the weight sum equalling 100, using the file's existing manual inline-validation convention
- [ ] 10.4 Add the cap control to the same card
- [ ] 10.5 Extend `ProjectSettingsForm.test.tsx` to cover the save gate and the live total
- [ ] 10.6 Verify `npm run frontend:typecheck` and `npm run lint` pass

## 11. Full verification

- [ ] 11.1 Run `go build ./...`, `go test ./...`, `go test -race ./...`, and `go vet ./...` from `backend/`
- [ ] 11.2 Run `npm run lint`, `npm run frontend:typecheck`, and confirm `npm run api` and `npm run sqlc` produce no diff
- [ ] 11.3 Exercise the feature end to end against a running daemon: configure a 60/30/10 mix, spawn repeatedly unpinned, and confirm the realized ratio
- [ ] 11.4 Exercise a bucket outage live: break one bucket's binary, confirm it is marked down and alerted, confirm redistribution, then confirm recovery on a later success
- [ ] 11.5 Exercise the cap live: fill a project to its cap and confirm intake defers and later resumes
- [ ] 11.6 Render the settings card via `ao preview` and confirm the save gate behaves
