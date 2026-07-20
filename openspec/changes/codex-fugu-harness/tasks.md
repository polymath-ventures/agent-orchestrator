## 1. Adapter parameterization (TDD)

- [ ] 1.1 Write failing tests: fugu manifest reports ID `codex-fugu` / name `Codex Fugu`, and the unparameterized Codex adapter still reports `codex` / `Codex`
- [ ] 1.2 Add the five optional fields (manifestID, manifestName, manifestDescription, binaryName, hookAgentToken) to `codex.Plugin` with Codex-defaulting accessors
- [ ] 1.3 Add `codex.NewFugu()` and point `Manifest()` at the accessors
- [ ] 1.4 Generalize `ResolveCodexBinary` into `ResolveAgentBinary(ctx, binaryName)`, keeping the Windows npm-shim/native-exe indirection gated on `binaryName == "codex"`; update the cached `agentBinary` helper and `install.go`'s `ResolveBinary`
- [ ] 1.5 Confirm the existing Codex adapter tests still pass unmodified — this is the plain-Codex regression guard

## 2. Wrapper flag (TDD)

- [ ] 2.1 Write failing exact-argv tests: `--no-update` immediately follows the binary on launch, precedes `resume` on restore, and precedes `exec` in probe args; and is absent for plain Codex
- [ ] 2.2 Implement `appendWrapperFlags`, emitting `--no-update` only when the adapter ID is `codex-fugu`
- [ ] 2.3 Wire it into `GetLaunchCommand`, `GetRestoreCommand`, and the probe argument builder

## 3. Hook token routing (TDD)

- [ ] 3.1 Write a failing test that a fugu launch command's managed hook flags invoke `ao hooks codex-fugu`
- [ ] 3.2 Split `appendSessionHookFlags` into `appendSessionHookFlagsFor(cmd, agentToken)`, keeping the existing no-arg form as a `"codex"` shim
- [ ] 3.3 Call the token-aware form from launch and restore
- [ ] 3.4 Add the `codex-fugu` entry to `activitydispatch.Derivers` mapped to Codex's deriver

## 4. Shared-login auth fallback (TDD)

- [ ] 4.1 Write failing tests with scripted binary fakes: (a) fugu profile error + codex logged in → authorized; (b) fugu profile error + codex not logged in → not authorized; (c) fallback exits clean with unrecognizable output → not authorized; (d) unrelated fugu failure → codex is never consulted
- [ ] 4.2 Refactor the login probe into a reusable `loginStatusForBinary` returning status, output text, command error, and probe error
- [ ] 4.3 Implement the narrow fallback in `AuthStatus`, gated on adapter ID `codex-fugu` **and** the `--profile only applies` substring

## 5. Domain and registry wiring

- [ ] 5.1 Add `HarnessCodexFugu = "codex-fugu"` and the `AllHarnesses` entry in `domain/harness.go`; verify whether `RequiresRuntimeOwnerPIDAuth` exists in this fork and include fugu only if it does. Leave `ReviewerHarness` untouched
- [ ] 5.2 Register `codex.NewFugu()` in `registry.Constructors()` directly after `codex.New()`
- [ ] 5.3 Add the `{domain.HarnessCodexFugu, "codex-fugu"}` row to the harness table in `daemon/wiring_test.go` and confirm it resolves

## 6. Persistence

- [ ] 6.1 Add `codex-fugu` to the harness list in `migrate_test.go` and watch it fail against the current CHECK constraint
- [ ] 6.2 Extend `0007_allow_implemented_harnesses.sql` in place — both the Up and Down `replace()` strings — and confirm the test passes and the constraint actually widened (a byte mismatch no-ops silently)

## 7. API, CLI, and docs

- [ ] 7.1 Add `codex-fugu` to the `enum:` struct tag on `SpawnSessionRequest.Harness` in `httpd/controllers/dto.go`
- [ ] 7.2 Run `npm run api` and commit the regenerated `openapi.yaml` and `frontend/src/api/schema.ts` together — do not hand-edit either
- [ ] 7.3 Update the `--harness` help string in `cli/spawn.go`
- [ ] 7.4 Add the `{Name: "codex-fugu", BinaryName: "codex-fugu", VersionArg: "--version"}` entry to `doctorHarnesses` in `cli/doctor.go` and update `doctor_test.go`
- [ ] 7.5 Update `skillassets/using-ao/commands/spawn.md` and the README agents row

## 8. Frontend

- [ ] 8.1 Add `"codex-fugu"` to `renderer/lib/agent-options.ts`, after `"codex"`
- [ ] 8.2 Add `| "codex-fugu"` to the `AgentProvider` union and the matching `case` in `toAgentProvider` in `renderer/types/workspace.ts`; extend `workspace.test.ts`
- [ ] 8.3 Add the Codex Fugu entry to `landing/components/LandingAgentsBar.tsx`

## 9. Verification and gates

- [ ] 9.1 Backend: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...`
- [ ] 9.2 Frontend: `npm run typecheck`, `npm run build`
- [ ] 9.3 Root: `npm run lint`, and confirm `npm run api` output is committed and clean
- [ ] 9.4 Exercise the real surface: start the daemon, confirm `codex-fugu` appears in the agent catalog and in `ao doctor`, and attempt a spawn. Record exactly what was and was not verified live — the binary is absent on this host, so the live-spawn gap must be stated plainly in the PR rather than implied as passing
- [ ] 9.5 In the PR description, name the rejected alternative (a separate `codexfugu` adapter package) and why parameterization won; state the fork-only status; and record the `fugu-ultra` manual-only ruling as a constraint inherited by GH #3 and GH #4
