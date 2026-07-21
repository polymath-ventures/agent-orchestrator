## 1. Shared fail-closed per-role instructions loader

- [x] 1.1 Write failing tests for a generalized per-role rules loader in `backend/internal/session_manager`: given `(inlineRules, rulesFile, projectPath)` it returns the content-preserving inline+file merge; a missing/unreadable file returns a hard error; an empty/whitespace-only file returns a hard error; a file exceeding a max-size bound returns a hard error naming the limit and actual size.
- [x] 1.2 Generalize `buildProjectRules` into a role-agnostic loader (`LoadRoleRules`) that satisfies 1.1, adding the new empty and oversized checks; keep the existing repo-relative path validation (`projectRelativeFile` / `validateRepoRelative`).
- [x] 1.3 Re-point the existing worker `## Project Rules` injection at the generalized loader; confirm existing worker tests still pass (behavior preserved except the new empty/oversized guards).

## 2. Per-role config fields, migration, and CLI flags

- [x] 2.1 Write failing tests: `domain.ProjectConfig` round-trips new fields `OrchestratorRulesFile`, `ReviewerRules`, `ReviewerRulesFile`; `validateRepoRelative` rejects an absolute/escaping path for each new `*File` field.
- [x] 2.2 Add the three fields to `domain.ProjectConfig` with validation parity to `AgentRulesFile`.
- [x] 2.3 ~~Add a new SQLite migration for the new config columns~~ — **not needed**: `ProjectConfig` persists as a single JSON blob (`marshalProjectConfig` → `json.Marshal`), so new fields need no migration or sqlc change (smallest-change per Merit rule).
- [x] 2.4 Write failing tests then add `ao project` flags (`--orchestrator-rules-file`, `--reviewer-rules`, `--reviewer-rules-file`) that set the fields via the existing config DTO → `PUT /api/v1/projects/{id}/config` path (CLI ships config only, does not read files or assemble).

## 3. Orchestrator and reviewer prompt injection

- [x] 3.1 Write a failing test that an orchestrator spawn injects `OrchestratorRulesFile` contents (content-preserving) under `## Project-Specific Orchestrator Rules`, and fails closed when the file is missing/empty/oversized.
- [x] 3.2 Wire the orchestrator assembly (`buildSystemPromptText` orchestrator branch) to load via the shared loader, merging inline `OrchestratorRules` + `OrchestratorRulesFile`.
- [x] 3.3 Write a failing test that a reviewer launch injects `ReviewerRules`/`ReviewerRulesFile` into the reviewer system prompt and fails closed on misconfiguration.
- [x] 3.4 Thread resolved reviewer rules through `review.LaunchSpec` and inject them in `reviewTexts` at the reviewer system-prompt position, loading via the shared loader daemon-side; keep the rest of the reviewer prompt unchanged.

## 4. Effective-prompt visibility daemon route

- [x] 4.1 Write a failing controller/integration test: `GET` assembled prompt for `(project, role)` returns the full recomputed prompt including base scaffold + operator override for worker, orchestrator, and reviewer; an unknown role is a client error; a misconfigured override returns the same fail-closed error a spawn raises.
- [x] 4.2 Add a read-only daemon route (`GET /api/v1/projects/{id}/roles/{role}/prompt`) that recomputes the assembled prompt from current `ProjectConfig` using the existing assembly functions (worker/orchestrator via session_manager, reviewer via review, composed by the `roleprompt` package) — no reliance on the ephemeral `system.md` artifact.
- [x] 4.3 Add the controller DTO and regenerate the API contract (`npm run api`); commit generated OpenAPI + TypeScript schema together.

## 5. `ao role prompt` CLI command

- [x] 5.1 Write failing tests: `ao role prompt <project> <role>` prints the assembled prompt from the daemon route for a valid role; an unknown/invalid role or missing project exits with a usage error (exit 2); daemon/runtime failures exit 1 preserving the error envelope/request ID.
- [x] 5.2 Implement the `ao role prompt` Cobra command calling the daemon route via the shared CLI client helper (no local prompt assembly, no direct storage/adapter access).

## 6. Supervisor UI inspector

- [x] 6.1 Add a read-only role-prompt inspector in the frontend that fetches the assembled prompt per `(project, role)` from the generated client and renders it read-only, surfacing the fail-closed error when the override is misconfigured.
- [x] 6.2 Add per-role override controls (set/clear inline rules and rules-file pointer for worker/orchestrator/reviewer) wired to the project-config update path; changes take effect on next spawn.
- [x] 6.3 `npm run frontend:typecheck` and `npm run build:web` pass; inspector verified rendering the live assembled prompt via a headless (Playwright) drive of the settings panel against a running daemon.

## 7. Verification and docs

- [x] 7.1 Full backend gate: `go build ./...`, `go test ./...`, `go test -race ./...`, `go vet ./...` green.
- [x] 7.2 Repo gate: `npm run lint` (my packages clean), `npm run frontend:typecheck`, `npm run api` (clean tree), format check green.
- [x] 7.3 End-to-end verify: set an override file per role on a project, confirmed its content appears in `ao role prompt <project> <role>`; pointed a role at a missing file and confirmed both the CLI (exit 1) and the visibility route (HTTP 422) fail loudly with a clear error.
- [x] 7.4 Update `docs/` architecture notes for the new role-instructions surface and visibility route; note the operator-inspection-vs-`systemPromptGuard` boundary.
