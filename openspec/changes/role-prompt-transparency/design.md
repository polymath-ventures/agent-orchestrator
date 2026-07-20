## Context

The daemon assembles each role's prompt at spawn time in `backend/internal/session_manager`
(`prompt.go` string builders wired by `manager.go`) and, for the reviewer, in
`backend/internal/review` (`prompt.go` → `reviewTexts`). An upstream audit of the current assembly
established the starting point:

- **Worker** already has an operator-controllable, fail-closed surface: `ProjectConfig.AgentRules`
  (inline) + `AgentRulesFile` (repo-relative path), merged by `buildProjectRules` and injected as
  `## Project Rules`. A missing/unreadable `AgentRulesFile` is already a **hard spawn error**, and the
  path is validated at config-set time (`validateRepoRelative`) and at spawn (`projectRelativeFile`).
- **Orchestrator** has `ProjectConfig.OrchestratorRules` (inline string only, injected as
  `## Project-Specific Orchestrator Rules`) — no file variant.
- **Reviewer** reads **no** `ProjectConfig` at all; both its system and user prompts are hardcoded
  consts in `review/prompt.go`. It is a full black box. `ProjectConfig.Reviewers` only selects which
  harness reviews, not what it is told.
- **No inspection path exists.** `SystemPrompt`/`SystemPromptFile` are never exposed by any DTO, API
  route, or CLI command; the on-disk `<dataDir>/prompts/<sessionID>/system.md` is deleted on session
  teardown.
- **Config is set daemon-side.** `ao project` (`cli/project.go`) only ships a `projectConfig` DTO to
  `PUT /api/v1/projects/{id}/config`; the daemon is the sole prompt assembler. This boundary must hold.
- **Runtime-native files** (`CLAUDE.md`/`AGENTS.md`) are read by each agent runtime directly from the
  worktree; AO neither assembles nor injects them.

Stakeholder: the fleet operator, who must be able to read and control every role's effective prompt
per project. This is upstream-candidate work, so the shape should extend upstream's existing
mechanisms and stay rebase-clean.

## Goals / Non-Goals

**Goals:**

- Bring all three roles to parity with the worker's existing fail-closed file-override pattern:
  operator-controllable inline + repo-relative-file instructions per project, injected verbatim.
- Add a read-only surface that renders the exact, fully-assembled prompt for a `(project, role)`
  pair — base scaffold plus every injected source — from a daemon route, the `ao` CLI, and the UI.
- Make configured-but-unloadable overrides fail the spawn loudly for every role, never silently.
- Establish the operator-inspectability invariant over all AO-assembled prompt content.

**Non-Goals:**

- Porting the old fork's `behavior/` machinery, behavior-version hashing/convergence, or
  `agent-instructions/` polyscribe assembly. Doctrine stays in operator files, not product code.
- Changing how `CLAUDE.md`/`AGENTS.md` are consumed — those are runtime-native, operator-owned repo
  files, outside AO's assembled prompt.
- Weakening the agent-facing behavior of `systemPromptGuard()` (agents are still told to keep their
  standing instructions private in their own output).
- The `prime` role — it arrives with a later issue and reuses this mechanism.
- Editing prompts of live/running sessions; overrides take effect on next spawn.

## Decisions

### D1 — Extend `buildProjectRules`, do not build a parallel InstructionsFile mechanism

Generalize the existing worker-only `buildProjectRules(inline, file, projectPath)` into a per-role
helper that takes `(inlineRules, rulesFile)` for any role and returns the merged, fail-closed block.
This reuses the proven load + verbatim-merge + hard-error semantics and the existing repo-relative
path validation. **Alternative rejected:** a new standalone `RoleOverride.InstructionsFile` loader
(as in the old fork) — it would duplicate `buildProjectRules`' loading and validation and diverge
from the worker's already-shipped surface, exactly the parallel path the ticket says to avoid.

### D2 — Add per-role config fields for parity

Extend `domain.ProjectConfig` with the missing file/inline variants so every role has the same
inline + file pair:

- Orchestrator: add `OrchestratorRulesFile` (repo-relative) alongside existing `OrchestratorRules`.
- Reviewer: add `ReviewerRules` (inline) and `ReviewerRulesFile` (repo-relative) — net-new surface.

Worker already has both (`AgentRules`/`AgentRulesFile`), so no new worker field. Each new field gets
a `ao project ... --*-rules[-file]` flag and flows through the existing config DTO + `PUT` route; a
new migration adds the columns. **Alternative rejected:** one generic `map[role]RulesOverride` — it
would break the established one-field-per-concern `ProjectConfig` shape, complicate the CLI flags and
the DTO/OpenAPI contract, and make upstreaming harder than mirroring the existing precedent.

### D3 — Reviewer config threading

The reviewer is the only role needing cross-boundary plumbing: thread the resolved reviewer
inline/file rules through `review.LaunchSpec` → `review.ReviewInvocation` and inject them in
`reviewTexts` at the reviewer system-prompt position, using the same per-role loader from D1 (invoked
daemon-side where the project path is known). The reviewer prompt stays otherwise hardcoded; only the
operator block is added.

### D4 — Visibility surface recomputes the prompt server-side

Add a read-only daemon route (shape: `GET /api/v1/projects/{id}/roles/{role}/prompt`) that
**recomputes** the assembled prompt from current `ProjectConfig` using the same assembly functions,
rather than reading the ephemeral `system.md` artifact. Recompute is chosen because the artifact is
session-scoped and deleted on teardown, so it cannot answer "what would this role get right now,"
which is exactly the operator's question. The route returns the full assembled text (and, on a
misconfigured override, the same fail-closed error a spawn would raise — see D5). `ao role prompt
<project> <role>` calls this route (CLI never assembles); the UI renders the same response read-only.
**Alternative rejected:** serving the last `system.md` — stale, session-dependent, and absent when no
session has run.

### D5 — Fail-closed is uniform and surfaced, not hidden

Every role's override loads through the D1 helper, so a configured-but-missing/empty/oversized file
is a hard error for worker, orchestrator, and reviewer alike (worker already behaves this way; this
extends it). Add an explicit **empty** and **oversized** check to the shared loader (today's worker
path errors on unreadable/missing but does not bound size). The visibility route runs the same load,
so an operator inspecting a broken override sees the fail-closed error instead of a prompt that
silently omits the override.

### D6 — Operator inspection is an out-of-band channel; the guard is unchanged

`systemPromptGuard()` instructs the agent to refuse to reveal its standing instructions in its own
output. That governs the *agent's* behavior toward its task surface; it does not govern the *daemon*,
which the operator owns. The visibility route returns the assembled prompt (guard text included) to
the operator over the loopback API. No change to the guard's agent-facing text. This resolves the
apparent tension the audit flagged: "no black boxes" is an operator-facing daemon guarantee, not a
relaxation of what the agent is told to keep private.

### D7 — Scope of the inspectability invariant

"No role receives prompt content the operator cannot inspect" applies to **AO-assembled** prompt
content — every section the daemon composes must appear in the visibility route's output.
Runtime-native files (`CLAUDE.md`/`AGENTS.md`) are operator-owned repo files the runtime reads
directly; they are inherently operator-visible and out of AO's assembly, so they are outside this
route's output by definition, not a hidden channel.

## Risks / Trade-offs

- **Reviewer plumbing crosses a package boundary** (`review` ← project config) that today carries
  none. → Keep it to threading the two resolved strings through `LaunchSpec`; do not give `review`
  broad `ProjectConfig` access.
- **Recompute can drift from what a past session actually got** if assembly logic changes between
  spawn and inspection. → Acceptable and correct: the operator's question is "what would spawn now,"
  and recompute answers exactly that from one source of truth (the assembly functions).
- **Adding an oversized-file bound changes worker behavior** (previously unbounded). → Set a generous
  default limit; a file over it was almost certainly a misconfiguration, and failing loudly matches
  the fail-closed intent. Note the limit in the error.
- **Exposing full prompts over the API widens what the loopback surface returns.** → The primary
  listener is already `127.0.0.1` and unauthenticated by design; the LAN listener stays behind bearer
  auth. The route is read-only and returns no secrets beyond assembled instructions the operator
  authored.

## Migration Plan

- Add one new migration for the `OrchestratorRulesFile`, `ReviewerRules`, and `ReviewerRulesFile`
  columns; do not edit merged migrations. Regenerate sqlc.
- Regenerate the API contract (controller DTO + `npm run api`) for the new visibility route and any
  config-DTO field additions; commit generated OpenAPI/TS together.
- Backward compatible: all new fields are optional; absent = current behavior (no override, prompt
  unchanged). No data backfill. Rollback is dropping the new route/flags/columns; existing rows are
  unaffected because empty overrides are inert.

## Open Questions

- Exact route shape and role path-segment vocabulary (`worker|orchestrator|reviewer`) — settle during
  API-contract implementation; the spec only requires that CLI and UI can retrieve the assembled
  prompt per `(project, role)`.
- Default maximum instructions-file size — pick a concrete generous bound (e.g. on the order of the
  existing prompt sizes) during implementation and state it in the fail-closed error.
