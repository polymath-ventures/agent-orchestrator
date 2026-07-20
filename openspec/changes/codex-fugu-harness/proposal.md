## Why

We actively run the `codex-fugu` harness family on this fleet, and GH #3 (weighted
worker mix) cannot configure or validate fugu buckets until the harness exists as a
first-class `AgentHarness`. This ports the harness from the old fork
(`~/agent-orchestrator-fscked`) onto the clean fork so #3 unblocks and operators can
spawn fugu workers from the CLI and UI today.

**Fork-only.** `codex-fugu` is a Polymath-internal binary. This change must never
appear in an upstream PR.

## What Changes

- **Parameterize the existing Codex adapter instead of adding a new one.** The
  `codex.Plugin` gains five optional string fields (manifest id/name/description,
  binary name, hook token), each read through an accessor that falls back to the
  Codex defaults. A zero-valued `Plugin` stays byte-for-byte plain Codex; a new
  `codex.NewFugu()` constructor returns the fugu-populated one. Fugu therefore
  inherits Codex's flags, hooks, activity model, and prompt delivery for free.
- **Generalize binary resolution.** `ResolveCodexBinary` becomes
  `ResolveAgentBinary(ctx, binaryName)`. The Windows npm-shim → native-exe
  indirection stays gated on `binaryName == "codex"`.
- **Suppress the fugu wrapper's update prompt.** `codex-fugu` is an auto-updating
  wrapper that blocks on a prompt. A `--no-update` flag is emitted as the *first*
  argument — before any subcommand — on the launch and restore commands. (This
  fork has no `exec`-style probe path, so there is no third site.)
- **Route fugu's hooks to its own token.** `appendSessionHookFlags` splits into
  `appendSessionHookFlagsFor(cmd, agentToken)` so fugu sessions emit
  `ao hooks codex-fugu …` rather than colliding with Codex's callbacks.
- **Fall back to the shared Codex login for auth.** `codex-fugu login status` fails
  with an `--profile only applies` error because fugu has no login of its own; auth
  lives in the shared Codex credential. On that specific error only, probe the plain
  `codex` binary instead.
- **Register the harness everywhere the fork enumerates harnesses**: the
  `HarnessCodexFugu` domain constant, the adapter registry, the activity-dispatch
  deriver map, the SQLite `sessions.harness` CHECK, the spawn API enum, the CLI
  `--harness` help, the doctor probe table, and the three frontend harness lists.

Not breaking: every change is additive, and the Codex path is preserved by the
accessor-fallback design rather than by a parallel code path.

## Capabilities

### New Capabilities

- `codex-fugu-harness`: the `codex-fugu` agent harness — a Codex-compatible worker
  harness that launches the `codex-fugu` binary, suppresses its wrapper update
  prompt, reports activity under its own hook token, and resolves authorization
  through the shared Codex login.

### Modified Capabilities

None. `openspec/specs/` carries no existing capability whose requirements change.

## Impact

**Backend**

- `backend/internal/adapters/agent/codex/` — `codex.go`, `hooks.go`, `install.go`
  (parameterization, wrapper flag, auth fallback, binary resolution).
- `backend/internal/domain/harness.go` — `HarnessCodexFugu` + `AllHarnesses`.
  `ReviewerHarness` is deliberately **not** widened.
- `backend/internal/adapters/agent/registry/registry.go` — `codex.NewFugu()`.
- `backend/internal/adapters/agent/activitydispatch/dispatch.go` — deriver entry.
  Required: the adapter installs `ao hooks` callbacks, and a missing entry means its
  activity is silently never reported.
- `backend/internal/storage/sqlite/migrations/0027_allow_codex_fugu_harness.sql` —
  a **new** migration widening the `sessions.harness` CHECK (Up and Down). Not an
  in-place edit of `0007`: goose tracks applied migrations by version, so an edit to
  already-applied `0007` would never re-run on existing installs. `0007`'s stale
  header note is corrected to point future additions at a new migration.
- `backend/internal/httpd/controllers/dto.go` — spawn enum tag; `openapi.yaml` and
  `frontend/src/api/schema.ts` regenerate from it.
- `backend/internal/cli/spawn.go`, `backend/internal/cli/doctor.go`,
  `backend/internal/skillassets/using-ao/commands/spawn.md`.

**Frontend**

- `frontend/src/renderer/lib/agent-options.ts`,
  `frontend/src/renderer/types/workspace.ts` (union **and** `toAgentProvider` case).

`README.md` and `frontend/src/landing/components/LandingAgentsBar.tsx` are
deliberately **not** touched: they are public marketing surfaces for a binary
nobody outside the fleet can install, and high-churn upstream files (see
`design.md`).

**Deliberately out of scope** — each has zero consumers in this fork today, so
building it now would be speculative surface:

- `ProviderFugu` / `ClassifyModelProvider`. `domain/modelprovider.go` does not exist
  in this fork; nothing classifies a provider from a model name. Arrives with GH #4.
- `knownModelsForHarness` entries for `fugu` / `fugu-ultra`. There is no model
  catalog in this fork yet. Arrives with GH #4.
- `agent:fugu` routing labels and worker-mix wiring. Belongs to GH #3, in flight.

**The `fugu-ultra` manual-only ruling.** The originating issue asks that `fugu-ultra`
never be selected by mix or intake. In this fork that property holds *by
construction*: there is no mix, no intake defaulting, and no per-harness model pin
for anything to select it. Adding an exclusion list or a detector now would be a
guard over machinery that does not exist. The ruling is instead recorded here as a
constraint GH #3 and GH #4 must honor when they build the selecting machinery.
