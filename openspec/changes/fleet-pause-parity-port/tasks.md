## 1. Fleet hard pause terminates orchestrators (GAP 1, must)

- [x] 1.1 Invert `pause_test.go` (`TestSetFleetPausedHardDrainsWorkersNotOrchestrators`) to a failing test asserting a fleet hard pause kills workers AND orchestrators, plus a test that per-project hard pause still spares the orchestrator
- [x] 1.2 Add `includeOrchestrators bool` to `service/project/pause.go` `hardDrain` and change the kind filter to skip orchestrators only when the flag is false
- [x] 1.3 Pass `true` from `SetFleetPaused` and `false` from `SetProjectPaused`; run `go test ./internal/service/project/...` green

## 2. Pause enforcement fails closed (GAP 2, should)

- [x] 2.1 Add failing tests: spawn guard refuses when `GetFleetPaused` errors; intake observer aborts the tick when `GetFleetPaused` errors
- [x] 2.2 In `session_manager/manager.go` `guardPaused`, return the wrapped read error instead of `err == nil && fleetPaused`; fix the comment
- [x] 2.3 In `observe/trackerintake/observer.go`, return the read error instead of swallowing it; fix the comment
- [x] 2.4 Confirm the read-only display helpers remain fail-open (unchanged); run the affected package tests green

## 3. Fleet card: escalate, Draining(N), 15s refresh, confirm copy (GAPS 3/4/5)

- [x] 3.1 Add/port failing `FleetSection.test.tsx` cases: hard button available while paused (escalation), Draining(N) aggregate across mixed pauseState workspaces, confirm copy names orchestrators
- [x] 3.2 Hoist "Pause now (hard)" out of the `!paused` branch so it renders while paused/draining
- [x] 3.3 Add `refetchInterval: 15_000` to the fleet-status query and derive `Draining (N)` from `useWorkspaceQuery` (`pauseState === "draining"` -> sum `drainingWorkers`)
- [x] 3.4 Rewrite the ConfirmDialog description to state the true blast radius (workers AND orchestrators, use normal pause to drain); run frontend test + typecheck green

## 4. CLI help + blank-id validation (GAP 6, should)

- [x] 4.1 Add a failing CLI test: `ao pause "  "` returns a usage error (exit 2) and does not contact the daemon
- [x] 4.2 Add the `Long` drain-semantics help text to the `pause` command; reject blank/whitespace project id with `usageError` in the args validator; `TrimSpace` the id on use
- [x] 4.3 Run `go test ./internal/cli/...` green

## 5. History verification + gate

- [x] 5.1 Re-confirm the 8 HOLDS history-mined lessons still pass after the edits (build + full backend test); confirm lesson #5 is addressed by the D5 rejection
- [ ] 5.2 Full CI gate: `go build ./...`, `go test -race ./...`, `go vet ./...`, `npm run lint`, `npm run frontend:typecheck`, frontend build, sqlc/api drift check
- [ ] 5.3 Fold the OpenSpec archive of `fleet-pause-parity-port` into this same PR (updates canonical `specs/fleet-pause`), then re-validate
