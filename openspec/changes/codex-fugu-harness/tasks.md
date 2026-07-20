## 1. Adapter parameterization (TDD)

- [x] 1.1 Write failing tests: fugu manifest reports ID `codex-fugu` / name `Codex Fugu`, and the unparameterized Codex adapter still reports `codex` / `Codex`
- [x] 1.2 Add the five optional fields (manifestID, manifestName, manifestDescription, binaryName, hookAgentToken) to `codex.Plugin` with Codex-defaulting accessors
- [x] 1.3 Add `codex.NewFugu()` and point `Manifest()` at the accessors
- [x] 1.4 Generalize `ResolveCodexBinary` into `ResolveAgentBinary(ctx, binaryName)`, keeping the Windows npm-shim/native-exe indirection gated on `binaryName == "codex"`; update the cached `agentBinary` helper and `install.go`'s `ResolveBinary`
- [x] 1.5 Confirm the existing Codex adapter tests still pass unmodified — this is the plain-Codex regression guard

## 2. Wrapper flag (TDD)

- [x] 2.1 Write failing exact-argv tests: `--no-update` immediately follows the binary on launch and precedes `resume` on restore; and is absent for plain Codex
- [x] 2.2 Implement `appendWrapperFlags`, emitting `--no-update` only when the adapter ID is `codex-fugu`
- [x] 2.3 Wire it into `GetLaunchCommand` and `GetRestoreCommand` (restructure restore so the flag precedes the `resume` subcommand; bump the slice capacity). `DoctorLaunchProbes()` stays Codex-only — this fork has no `exec` probe path

## 3. Hook token routing (TDD)

- [x] 3.1 Write a failing test that a fugu launch command's managed hook flags invoke `ao hooks codex-fugu`
- [x] 3.2 Split `appendSessionHookFlags` into `appendSessionHookFlagsFor(cmd, agentToken)`, keeping the existing no-arg form as a `"codex"` shim
- [x] 3.3 Call the token-aware form from launch and restore
- [x] 3.4 Add the `codex-fugu` entry to `activitydispatch.Derivers` mapped to Codex's deriver

## 4. Shared-login auth fallback (TDD)

- [x] 4.1 Write failing tests with scripted binary fakes: (a) fugu profile error + codex logged in → authorized; (b) fugu profile error + codex not logged in → not authorized; (c) fallback exits clean with unrecognizable output → not authorized; (d) unrelated fugu failure → codex is never consulted
- [x] 4.2 Refactor the login probe into a reusable `loginStatusForBinary` returning status, output text, a `failed` bool for a non-zero exit, and a probe error (a bool, not an error: a non-zero exit is a signal about login state, not a failure to propagate)
- [x] 4.3 Implement the narrow fallback in `AuthStatus`, gated on adapter ID `codex-fugu` **and** the `--profile only applies` substring

## 5. Domain and registry wiring

- [x] 5.1 Add `HarnessCodexFugu = "codex-fugu"` and the `AllHarnesses` entry in `domain/harness.go`; verify whether `RequiresRuntimeOwnerPIDAuth` exists in this fork and include fugu only if it does. Leave `ReviewerHarness` untouched
- [x] 5.2 Register `codex.NewFugu()` in `registry.Constructors()` directly after `codex.New()`
- [x] 5.3 Add the `{domain.HarnessCodexFugu, "codex-fugu"}` row to the harness table in `daemon/wiring_test.go` and confirm it resolves

## 6. Persistence

- [x] 6.1 Add `codex-fugu` to the harness list in `migrate_test.go` and watch it fail against the current CHECK constraint
- [x] 6.2 Extend `0007_allow_implemented_harnesses.sql` in place — both the Up and Down `replace()` strings — and confirm the test passes and the constraint actually widened (a byte mismatch no-ops silently)

## 7. API, CLI, and docs

- [x] 7.1 Add `codex-fugu` to the `enum:` struct tag on `SpawnSessionRequest.Harness` in `httpd/controllers/dto.go`
- [x] 7.2 Run `npm run api` and commit the regenerated `openapi.yaml` and `frontend/src/api/schema.ts` together — do not hand-edit either
- [x] 7.3 Update the `--harness` help string in `cli/spawn.go`
- [x] 7.4 Add the `{Name: "codex-fugu", BinaryName: "codex-fugu", VersionArg: "--version"}` entry to `doctorHarnesses` in `cli/doctor.go` and update `doctor_test.go`
- [x] 7.5 Update `skillassets/using-ao/commands/spawn.md`. README agents row and the landing marquee deliberately skipped — public marketing surfaces for a binary nobody outside the fleet can install (see design.md)

## 8. Frontend

- [x] 8.1 Add `"codex-fugu"` to `renderer/lib/agent-options.ts`, after `"codex"`
- [x] 8.2 Add `| "codex-fugu"` to the `AgentProvider` union and the matching `case` in `toAgentProvider` in `renderer/types/workspace.ts`; extend `workspace.test.ts`
- [x] 8.3 ~~Landing marquee entry~~ — intentionally skipped; see design.md

## 9. Verification and gates

- [x] 9.1 Backend: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...`
- [x] 9.2 Frontend: `npm run typecheck`, `npm test` (there is no frontend `build` script)
- [x] 9.3 Root: `npm run lint`, and confirm `npm run api` output is committed and clean
- [x] 9.4 Exercise the real surface: start the daemon, confirm `codex-fugu` appears in the agent catalog and in `ao doctor`, and attempt a spawn. Record exactly what was and was not verified live — the binary is absent on this host, so the live-spawn gap must be stated plainly in the PR rather than implied as passing
- [x] 9.5 In the PR description, name the rejected alternative (a separate `codexfugu` adapter package) and why parameterization won; state the fork-only status; and record the `fugu-ultra` manual-only ruling as a constraint inherited by GH #3 and GH #4
