## Why

Today the effective system prompt each agent role (worker, orchestrator, reviewer) runs with is
partly assembled from prompt content baked into the Go daemon, so an operator cannot fully see or
control what an agent was told. Automating a fleet that ran well under manual operation requires
carrying the operator's judgment to every role, which is impossible when any part of a role's prompt
is a black box: the operator must be able to read the exact, fully-assembled prompt for each role per
project, and to override or extend it, or they are supervising agents whose instructions they cannot
inspect.

## What Changes

- Add a per-project, per-role operator instructions pointer (an `InstructionsFile`-style override)
  for the worker, orchestrator, and reviewer roles. When set, its contents are injected **verbatim**
  into that role's assembled prompt on the next spawn, extending (not replacing) the existing
  upstream instruction surfaces rather than introducing a parallel assembly path.
- Make the injection **fail-closed**: a configured-but-missing, empty, or oversized instructions
  file fails the spawn loudly with a clear error. There is no silent fallback to defaults when an
  override is configured — a misconfiguration never degrades to a different, unseen prompt.
- Add a read-only **effective-prompt visibility** surface that renders the exact, fully-assembled
  prompt a given role receives for a given project — base scaffold plus every injected instruction
  source — exposed through a daemon API route and an `ao` CLI command (`ao role prompt <project>
  <role>`), and inspectable from the supervisor UI.
- Establish the invariant that **no role receives prompt content the operator cannot inspect**: every
  constituent piece of a role's prompt is reachable through the visibility surface.
- Explicitly out of scope / NOT ported from the old fork: the `behavior/` directory machinery,
  behavior-version hashing and convergence, and `agent-instructions/` polyscribe assembly. Doctrine
  lives in operator-controlled files, not as product-embedded policy prose.

## Capabilities

### New Capabilities
- `role-instructions`: Per-project, per-role operator-controlled instruction overrides injected
  verbatim into role prompt assembly (fail-closed on misconfiguration), plus a read-only
  effective-prompt visibility surface (daemon API, `ao` CLI, and supervisor UI) that renders the
  exact fully-assembled prompt each role receives.

### Modified Capabilities
<!-- No existing capability spec covers role prompt assembly; this is net-new behavior. -->

## Impact

- **Backend (Go):** the role/prompt assembly path in the session manager and orchestrator/reviewer
  launch code (extends the existing instruction surface with the operator override injection point);
  a new fail-closed loader for per-role instructions files; project settings/storage carrying the
  per-role instruction pointers; a new daemon HTTP route returning an assembled prompt for a
  `(project, role)` pair.
- **CLI (Go/Cobra):** a new `ao role prompt <project> <role>` command that calls the daemon route
  (per repo convention, the CLI reads the assembled prompt from the daemon; it does not build prompts
  itself); usage errors returned as `usageError`.
- **API contract:** new controller DTO(s) and OpenAPI/TypeScript schema regeneration for the
  effective-prompt route.
- **Frontend (Electron/React):** a read-only settings inspector surface that renders the assembled
  prompt per role and lets the operator set each role's instructions-file override per project.
- **Storage:** a new migration for the per-role instruction pointer fields (no edits to already-
  merged migrations).
- **Upstream:** the audit, the visibility surface, and the per-role instructions extension are all
  intended as upstream-candidate work; keep the change rebase-clean and idiomatic to upstream's
  existing mechanisms.
