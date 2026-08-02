## 1. Pin the non-recycling identity contract with failing tests

- [x] 1.1 Add store tests for `{project}-{num}-{generation}` IDs, stable generation within one database, and distinct identities across simulated database rebuilds
- [x] 1.2 Add store tests for projectless Prime and for refusing empty or malformed generation tokens
- [x] 1.3 Add tmux, workspace, branch, and naming regression coverage for token-bearing IDs and unchanged daemon display names

## 2. Persist and consume the database generation

- [x] 2.1 Add migration `0059_add_session_id_generation.sql` that mints one 64-bit lowercase-hex database-generation token without rewriting existing session IDs
- [x] 2.2 Add the sqlc accessor for `daemon_settings.session_id_generation` and regenerate sqlc output
- [x] 2.3 Compose the validated generation token into every newly created project and projectless-Prime session ID at the store's single creation seam

## 3. Verify cross-surface identity behavior

- [x] 3.1 Prove rebuilt-database sessions derive distinct workspace paths, tmux names, default branches, and Claude project-directory slugs from the new ID
- [x] 3.2 Re-run Claude native-ID persistence and legacy-fallback tests from PR #245 to prove the two identity layers remain orthogonal
- [x] 3.3 Run the focused store/session-manager/workspace/runtime suites with race coverage

## 4. Validate documentation and release gates

- [x] 4.1 Validate `session-identity-schema` with OpenSpec strict validation and keep the nine-surface schema document in sync with the implementation
- [x] 4.2 Run `npm run sqlc`, formatting, and `npm run ci-local`
- [ ] 4.3 Rebase the stack after PR #245 lands, push, open the PR, and complete independent final review
