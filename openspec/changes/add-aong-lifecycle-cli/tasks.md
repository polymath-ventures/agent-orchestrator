## 1. Package skeleton and testing seam

- [x] 1.1 Add `backend/internal/aongcli` with a `Deps` struct (`Out`, `Err`, `Executable`, `LookPath`, `RunCommand(ctx, name, args...) ([]byte, error)`) and `withDefaults`, mirroring `backend/internal/cli`'s shape.
- [x] 1.2 Add `usageError`/`ExitCode` (2 for misuse, 1 for runtime failure) and a Cobra root command with `SilenceUsage`/`SilenceErrors` and a flag-error func that tags misuse — failing test first for both exit codes.
- [x] 1.3 Add `backend/cmd/aong/main.go` wiring `aongcli.Execute()` to the process exit code.

## 2. ao resolution

- [x] 2.1 Failing test: sibling `ao` beside the running binary is preferred over a different `ao` on `PATH`.
- [x] 2.2 Failing test: falls back to `PATH` when no sibling exists; fails naming both searched locations when neither has one.
- [x] 2.3 Implement `resolveAO` using `Executable()` + `LookPath`, and a `runAO` helper that surfaces the invoked command and its output verbatim on failure.

## 3. Environment detection

- [x] 3.1 Failing tests for the three classifications: `systemd` (systemctl present, ≥1 AO unit loaded), `plain` (no systemctl — no invocation attempted), `plain` (systemctl present, no AO unit loaded).
- [x] 3.2 Implement detection by asking the user manager for each AO unit's `LoadState` via `systemctl --user show <unit> -P LoadState`, treating anything other than `not-found` as loaded.

## 4. Read-only verbs

- [x] 4.1 Failing tests for `aong status`: delegates to `ao status`; adds a unit-state section only under `systemd`; reports the detected environment under `plain`; does not fail when the daemon is stopped.
- [x] 4.2 Implement `aong status` (pass through `ao status` output, then loaded-unit `ActiveState` lines under `systemd`).

## 5. Work-control verbs

- [x] 5.1 Failing tests asserting recorded argv: `drain` → `ao pause --all`; `stop-work` → `ao pause --all --hard`; `resume` → `ao resume --all`.
- [x] 5.2 Failing test: `drain` and `stop-work` output states the fleet stays gated until `aong resume`.
- [x] 5.3 Implement the three verbs plus their help text; assert no `pause` verb is registered.

## 6. Daemon verbs

- [x] 6.1 Failing tests for `aong stop`: invokes `ao stop`, invokes no session-termination command, and its output states that agent sessions keep running and names `aong shutdown`.
- [x] 6.2 Failing tests for `aong shutdown`: ready daemon → `ao pause --all --hard` before `ao stop`; failed stop-work → `ao stop` never invoked and the command fails; non-ready daemon → no fleet pause invoked, `ao stop` invoked.
- [x] 6.3 Implement `aong stop` and `aong shutdown`, reading daemon readiness from `ao status --json`'s `state` field.

## 7. Start verb

- [x] 7.1 Failing tests for `aong start`: loaded units are started via `systemctl --user start` and reported; unloaded units are skipped without error; `plain` environment fails with a message naming how a daemon is started there.
- [x] 7.2 Implement `aong start`.

## 8. Build, docs, and verification

- [x] 8.1 Ensure the release build produces `aong` beside `ao`; update the build/release workflow and any packaging list that enumerates binaries.
- [x] 8.2 Document `aong` in `docs/fork.md`: the verb set, the porcelain rule, and which environments are verified versus untested.
- [x] 8.3 Run `npm run ci-local` green, then exercise the built binary on this host: `aong status` against the live fleet and `aong start` in a no-`systemctl` environment, recording the exact commands and output in the PR.
