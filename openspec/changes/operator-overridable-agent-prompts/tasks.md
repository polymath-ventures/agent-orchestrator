## 1. Typed prompt resolution and spawn parity

- [x] 1.1 Add failing domain/config tests for the typed per-project template, daemon-wide environment default, precedence, and invalid active-template errors.
- [x] 1.2 Add failing renderer and session-manager tests for `{issue}` substitution, configured prompt authority, explicit prompt authority, and byte-identical legacy fallbacks.
- [x] 1.3 Implement the shared task-template renderer and wire typed defaults through daemon startup and manual issue spawns.
- [x] 1.4 Add failing tracker-intake tests for project/global precedence and intake/manual parity, then route configured intake through the shared renderer while preserving `BuildIssuePrompt` when unset.

## 2. Operator configuration and inspection surfaces

- [x] 2.1 Add failing CLI/API tests for the per-project `workerTaskPrompt` field and task-template inspection metadata.
- [x] 2.2 Expose the project field through CLI/config DTOs and expose resolved worker template/source alongside the exact system prompt.
- [x] 2.3 Add failing frontend tests, then add the project settings field and resolved task-template block to the prompt inspector.
- [x] 2.4 Regenerate OpenAPI and TypeScript schemas and verify generated-client type checking.

## 3. Documentation and verification

- [x] 3.1 Document `AO_WORKER_TASK_PROMPT`, per-project precedence, `{issue}`, exact replacement, and the append-only system-prompt boundary.
- [x] 3.2 Add the prompt-override mechanism to `docs/fork.md` with sync anchors and issue reference.
- [x] 3.3 Run focused Go/frontend tests, OpenSpec strict validation, `npm run ci-local`, and final independent review.
