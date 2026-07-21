## 1. Audit Existing Model Work

- [x] 1.1 Compare current `main` model/session/worker-mix code with the old fork reference and list which issue #4 acceptance bullets are already satisfied.
- [x] 1.2 Identify the smallest remaining code paths needed for shared model resolution, compatibility errors, availability state, reviewer pins, and UI/API exposure.

## 2. Harness-Aware Resolution

- [x] 2.1 Add focused tests for the model-resolution cascade, including per-harness overrides, scalar compatibility filtering, explicit spawn override precedence, and the claude-code default.
- [x] 2.2 Extend `domain.AgentConfig` with per-harness model/effort entries and validation for unknown harnesses, invalid effort, whitespace, and provider mismatches.
- [x] 2.3 Move launch model resolution into a shared resolver used by worker, orchestrator, worker-mix fallback, restore, model-health enumeration, and reviewer launches.
- [x] 2.4 Map cross-provider explicit spawn mismatches to a 400 before session seed-row or worktree creation.

## 3. Session and CLI/API Surface

- [x] 3.1 Verify `ao spawn --model` sends the request field, trims the persisted resolved model, and leaves project defaults untouched.
- [x] 3.2 Verify restore always launches with the persisted model for non-legacy sessions and preserves empty-model harness-default semantics.
- [x] 3.3 Regenerate OpenAPI and TypeScript schema after any DTO or config shape changes.
- [x] 3.4 Update CLI help and generated skill asset docs for model pins and per-harness config fields.

## 4. Model Availability

- [x] 4.1 Add a model-validation capability to agent adapters that can probe models, starting with Claude Code and Codex.
- [x] 4.2 Add a model-health service/store for configured model pins with reason codes `not-probed`, `no-capability`, `probe-unavailable`, and cached unreachable/recovered verdicts.
- [x] 4.3 Ensure the spawn path reads cached model-health state only and performs no live provider/network probe.
- [x] 4.4 Add background revalidation with `model_unreachable` and `model_recovered` notification transitions.

## 5. Reviewer Model Pins

- [x] 5.1 Extend reviewer config with an agent-config delta for reviewer model pins.
- [x] 5.2 Add tests proving reviewer model pins reach the selected reviewer adapter and cross-provider reviewer pins fail validation.

## 6. Settings UI

- [x] 6.1 Add compact per-harness model/default controls and reviewer model controls to the existing `ProjectSettingsForm`.
- [x] 6.2 Surface model availability reason codes without implying missing probe data is a hard spawn failure.
- [x] 6.3 Verify settings saves preserve unrelated hidden config and render cleanly on desktop/mobile-sized viewports.

## 7. Verification

- [x] 7.1 Run backend unit tests covering domain config, session spawn/restore, model health, reviewer launch, and HTTP error mapping.
- [x] 7.2 Run generated-code checks: `npm run sqlc` if queries/migrations changed and `npm run api` if API contracts changed.
- [x] 7.3 Run frontend typecheck/build and relevant settings UI tests.
- [x] 7.4 Run full repo gates before push: lint, backend build/test/vet, frontend typecheck/build, and agent CI where feasible. Agent CI was attempted but blocked by local runner secrets/tooling (`GITHUB_TOKEN`/release signing secrets, `/usr/bin/git` permission, missing `gh`).
