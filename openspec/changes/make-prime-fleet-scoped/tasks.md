## 1. Storage and Domain

- [x] 1.1 Add `domain.PrimeSettings` with validation/defaults for enablement,
      display name, harness/model/effort, instructions/rules, rules file, and
      wake policy.
- [x] 1.2 Add a SQLite migration extending daemon-owned settings with persisted
      Prime settings and allowing projectless sessions only for `kind='prime'`.
- [x] 1.3 Update sqlc queries/generated code and store methods for reading and
      saving Prime settings.
- [x] 1.4 Add storage/domain tests for disabled defaults, restart persistence,
      validation, and non-Prime project ownership enforcement.

## 2. Prime Supervisor and Session Launch

- [x] 2.1 Refactor the Prime supervisor to read persisted settings on each
      reconcile tick instead of using `AO_PRIME_PROJECT_ID` as the activation
      gate.
- [x] 2.2 Change `SpawnPrime` and session-manager launch paths to use an
      AO-managed projectless fleet workspace, with no hidden project row.
- [x] 2.3 Resolve Prime harness/model/effort, display name, prompt rules, and
      wake policy from `PrimeSettings`.
- [x] 2.4 Implement live disable retirement and ensure that disabled Prime stops
      ensure, replacement, and wake attempts immediately.
- [x] 2.5 Add regression tests for zero-project enable, live enable/disable,
      singleton enforcement, project pause/removal independence, restart
      persistence, and replacement/wake behavior.

## 3. API, CLI, and Generated Client

- [x] 3.1 Add daemon API DTOs and routes to read/update fleet Prime settings and
      report legacy environment migration state.
- [x] 3.2 Add CLI controls backed by those routes, including enable/disable and
      a fleet-scoped Prime prompt inspection path.
- [x] 3.3 Update OpenAPI generation and the frontend TypeScript API schema.
- [x] 3.4 Add controller and CLI tests covering shared persistence, validation
      errors, and legacy env not re-enabling disabled Prime.

## 4. Frontend and Documentation

- [x] 4.1 Move Prime controls from project settings to global Settings, including
      the Enable fleet Prime toggle and editable Prime configuration.
- [x] 4.2 Keep the sidebar rendering active Prime as a global top-level session
      with no project nesting.
- [x] 4.3 Surface legacy `AO_PRIME_PROJECT_ID` migration state and operator
      action in global Settings.
- [x] 4.4 Update docs and deployment guidance to retire the operator drop-in
      activation path.
- [x] 4.5 Add frontend tests for global settings round trip, sidebar display,
      and removal of project-owned Prime controls.

## 5. Verification

- [x] 5.1 Run `npm run sqlc`, `npm run api`, backend Go tests, frontend
      typecheck/build, lint, and the repository CI command.
- [x] 5.2 Verify production-style zero-project startup creates Prime only after
      persisted enablement and no longer logs an unknown-project boot warning.
- [x] 5.3 Document the rejected synthetic-project alternative in the PR.

Verification notes:

- `npm run sqlc`, `npm run api`, `cd backend && go test ./...`,
  `npm run frontend:typecheck`, `npm --prefix frontend run build:web`, focused
  Prime frontend tests, and `openspec validate make-prime-fleet-scoped --strict`
  passed locally.
- `npm run lint` ran after branch-owned lint fixes; it still fails on existing
  generated sqlc `dupl`/`errcheck` findings plus a stale sibling-worktree
  `gosec` path outside this branch.
- `npx @redwoodjs/agent-ci run --all` ran and failed in the local runner before
  branch checks could complete because required secrets were missing, non-Linux
  jobs cannot start on this host, `gh` is absent from the runner image, and the
  local runner hit `/usr/bin/git: Permission denied` during checkout.
