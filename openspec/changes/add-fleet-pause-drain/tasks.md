## 1. Storage: pause bits (migration + queries + store)

- [x] 1.1 Add migration `0030_fleet_pause.sql` (goose): `ALTER TABLE projects ADD COLUMN paused INTEGER NOT NULL DEFAULT 0`; `CREATE TABLE daemon_settings(id INTEGER PRIMARY KEY CHECK(id=1), fleet_paused INTEGER NOT NULL DEFAULT 0)`; seed `INSERT INTO daemon_settings VALUES (1,0)`; matching Down that drops the table and column. Header comment notes `UpsertProject` deliberately omits `paused`.
- [x] 1.2 Add `queries/daemon_settings.sql`: `GetFleetPaused` (`SELECT fleet_paused FROM daemon_settings WHERE id=1`) and `SetFleetPaused` (`UPDATE daemon_settings SET fleet_paused=? WHERE id=1`).
- [x] 1.3 Update `queries/projects.sql`: add `paused` to every SELECT column list; add `SetProjectPaused :execrows` (`UPDATE projects SET paused=? WHERE id=?`); confirm `UpsertProject` does NOT include `paused`.
- [x] 1.4 Run `npm run sqlc`; add store wrappers `SetProjectPaused(ctx,id,paused)`, `GetFleetPaused(ctx)`, `SetFleetPaused(ctx,paused)` (writers take the write mutex). Failing test first: round-trip each bit through the store; assert a project INSERT/upsert leaves `paused` untouched.

## 2. Pause service + derived state

- [x] 2.1 Add `PauseState` type + `PauseStateRunning|Draining|Paused` constants and `computePauseState(projectPaused, fleetPaused, liveWorkers)` in the project service types; unit-test the three transitions incl. draining count.
- [x] 2.2 Add `liveWorkersByProject` (count non-terminated `KindWorker` sessions per project) and a `withPauseState` read-model fan-in; test against a fake session list.
- [x] 2.3 Implement `SetProjectPaused(ctx,id,paused,hard)` (404 on missing/archived; writes only the bit; on `paused&&hard` calls session hard-drain for the project, orchestrators excluded), `FleetPaused(ctx)`, and `SetFleetPaused(ctx,paused,hard)` (on `paused&&hard` fan out hard-drain across all projects, orchestrators included). Table-test each incl. error envelopes.

## 3. Enforcement: spawn guard

- [x] 3.1 Define sentinel `ErrProjectPaused` alongside the existing spawn sentinels in `session_manager/manager.go`.
- [x] 3.2 Add `guardPaused(ctx, project, cfg)` and call it at the top of `Manager.Spawn` (before any durable row/workspace), mirroring the `MaxLiveWorkers` cap guard: no-op for orchestrator/prime kinds, force override, or nil store; else return `ErrProjectPaused` when the project bit OR the fleet flag is set (with a `scope` of `project`/`fleet`). Add a `Force` field to `SpawnConfig` if absent.
- [x] 3.3 Failing tests: worker spawn refused when project paused; refused when fleet paused; allowed for orchestrator kind; allowed with force; allowed when neither flag set.

## 4. Enforcement: tracker-intake gate

- [x] 4.1 Add `GetFleetPaused` to the intake observer's store interface; short-circuit the whole `Poll` tick when the fleet flag is set (quiet debug log, no backoff).
- [x] 4.2 Exclude paused projects from the enabled-filter (`Config.TrackerIntake.Enabled && !project.Paused`) so a paused project intakes nothing.
- [x] 4.3 Failing tests: fleet-paused tick dispatches zero spawns; a paused project is skipped while an unpaused sibling still intakes.

## 5. Drain sweeper

- [x] 5.1 Add `observe/drain/drain.go`: `Sweeper` with `Store`(ListProjects, GetFleetPaused) + `Sessions`(List, Kill) interfaces, 5s `DefaultTickInterval`, injected telemetry/logger/clock, and a per-project `hadLive` set.
- [x] 5.2 Implement `drainable(status)` — true only for `StatusIdle` and the terminal-complete state (confirm `StatusMerged` in `domain/status.go`; `no_signal` NOT drainable). Unit-test the predicate across all statuses.
- [x] 5.3 Implement `Tick`/`drainProject`: skip ungated projects (clear `hadLive`); for gated projects list `KindWorker` sessions, skip terminated, `Kill` drainable ones through the clean teardown path, count live vs drained; emit `ao.fleet.drain_complete` telemetry once on the transition to zero live.
- [x] 5.4 Wire the sweeper via `drain_wiring.go` into the daemon `lifecycleStack` (own goroutine + done channel), started alongside the reaper; ensure clean shutdown on ctx cancel. Test: a paused project's idle worker is killed; a working worker is left; completion telemetry fires exactly once.

## 6. Summary threading (API read model)

- [x] 6.1 Add `Paused`/`PauseState`/`DrainingWorkers` (json `paused`/`pauseState`/`drainingWorkers,omitempty`) to `project.Summary` and the project detail DTO; populate them in `Service.List` and the detail read via `withPauseState`.
- [x] 6.2 Test: list/detail reflect running/draining(N)/paused correctly.

## 7. Daemon HTTP API

- [x] 7.1 Add routes to the projects controller `Register`: `POST /projects/{id}/pause`, `POST /projects/{id}/resume`, `GET /fleet`, `POST /fleet/pause`, `POST /fleet/resume`; parse `?hard` via `strconv.ParseBool` (absent → soft).
- [x] 7.2 Add `FleetStatusResponse{paused}` DTO and the `?hard` param spec; project pause/resume reuse the project response envelope. Preserve error envelopes/request IDs.
- [x] 7.3 Run `npm run api`; commit generated OpenAPI + TS client together. Handler tests: pause/resume/fleet round-trips, hard param parsing, 501 when the manager is nil.

## 8. CLI

- [x] 8.1 Add `ao pause [project] [--all] [--hard]` and `ao resume [project] [--all]` (resume has no `--hard`); at most one project arg; `--all` and a project id are mutually exclusive. Route via `postJSON` to the pause/resume routes with a `?hard=true` query when hard.
- [x] 8.2 Surface pause state in `ao status` (fleet line) and `ao projects` list/detail (`running` / `draining (N)` / `paused` via a shared formatter). Return `usageError` for misuse (exit 2).
- [x] 8.3 Tests: fleet pause/resume, project soft/hard pause, status/list rendering, and usage errors.

## 9. Frontend UI

- [x] 9.1 Global Settings "Fleet" card: Running/Paused indicator; Pause, Pause-now-hard (confirm dialog), Resume actions wired to the generated client.
- [x] 9.2 Per-project sidebar `Draining(N)` / `Paused` badges + kebab pause/resume, reading the new summary fields.
- [x] 9.3 `npm run frontend:typecheck` + `npm run build`; drive the UI via `ao preview` to confirm pause/resume/hard-confirm render and act correctly.

## 10. End-to-end verification + docs

- [x] 10.1 Live daemon check: fleet pause ⇒ no intake/spawns anywhere; running workers finish and drain at idle; resume restores operation. Capture the commands + output.
- [x] 10.2 Live check: hard pause kills live workers immediately; per-project pause scoped to one project; a project registered during a fleet pause is gated from its first moment; pause/resume leaves a project's config bytes unchanged (diff before/after).
- [x] 10.3 Full CI: `go build ./...`, `go test ./...`, `go vet ./...`, `npm run lint`, `npm run frontend:typecheck`, format check. Update docs if lifecycle/CLI/API notes need it.
