## 1. Snapshot convention & runner scaffold

- [ ] 1.1 Establish the known snapshot path `ops/project-config/<project>.json` (add the directory with a `.gitkeep` or README noting the convention and the `fork-only` scope).
- [ ] 1.2 Create `ops/config-drift-check.mjs` scaffold: resolve the `ao` binary via `AO_BIN` (default `ao`), resolve the snapshot dir, and enumerate tracked projects as exactly the `*.json` files present in the snapshot dir (D2). No behavior yet beyond enumeration.

## 2. Drift-check core (TDD)

- [ ] 2.1 Write failing tests: given a stub `ao` and a snapshot dir, the runner runs `ao project config diff <project> <snapshot>` once per snapshot; exits zero when all stubs report in-sync; exits nonzero and names each drifted project (with the stub's diff output) when any drifts.
- [ ] 2.2 Write failing test: an early drifting project does NOT stop the runner from checking every remaining project before it exits (aggregate, don't fail-fast).
- [ ] 2.3 Write failing test: the runner only ever invokes `config diff` — never `config apply` — and touches no snapshot file in check mode.
- [ ] 2.4 Write failing test: a per-project usage/setup error (exit 2 from the stub) is reported distinctly from drift, not conflated with it.
- [ ] 2.5 Implement the check mode to pass 2.1–2.4: loop snapshots, shell out to `config diff`, aggregate exit codes (nonzero if any project drifts or errors), print a concise per-project report.

## 3. Deliberate refresh mode (TDD)

- [ ] 3.1 Write failing tests: `config-drift-check.mjs --refresh <project>` writes `ao project config export <project>` to that project's snapshot file; after refresh a `diff` against the refreshed file reports in-sync; refreshing an already-in-sync project leaves the file byte-unchanged (no spurious diff).
- [ ] 3.2 Implement `--refresh <project>` mode to pass 3.1 (export → write snapshot; no `apply`, no live-config mutation).

## 4. Scheduling via systemd (TDD)

- [ ] 4.1 Add `ops/ao-config-drift.service` (`Type=oneshot`, `ExecStart` runs `config-drift-check.mjs`, sets `AO_BIN`) and `ops/ao-config-drift.timer` (conservative `OnBootSec` + `OnUnitInactiveSec` cadence, `WantedBy=timers.target`), mirroring the `ao-tmux-claim` pair (D6).
- [ ] 4.2 Extend `ops/ao-systemd-units.test.mjs` to cover the new unit pair (valid `[Unit]/[Service]/[Timer]/[Install]` structure, service→timer linkage, oneshot type).

## 5. Wire-up, snapshots & docs

- [ ] 5.1 Generate the initial committed snapshot(s) via `--refresh` for the tracked project(s) on the ops host and commit them under `ops/project-config/` (or document the one-time bootstrap step if no live daemon is available in-session).
- [ ] 5.2 Document the convention in the appropriate ops/docs location: snapshot path, how the scheduled drift check surfaces drift (nonzero exit + journal, no self-heal), and the refresh-then-commit workflow for intentional changes.
- [ ] 5.3 Run the full gate: `npm run lint`, the ops mjs tests, `go build ./...` (no Go change expected), and confirm the change stays `fork-only` (no upstream-bound edits).
