## Why

Prime is a fleet-wide singleton, but its activation, configuration, session row,
and workspace are currently owned by an arbitrary registered project through
`AO_PRIME_PROJECT_ID`. Production with an empty project registry therefore
cannot start Prime at all. Prime needs one fleet-level source of truth and a
global settings toggle, not a hidden host project.

## What Changes

- Add durable fleet-level Prime settings with an enable/disable toggle,
  harness/model/effort, display name, rules, rules file, and wake policy.
- Add global daemon API, CLI, and Settings UI controls over the same persisted
  Prime settings.
- Make Prime sessions projectless in persistence and API read models while
  preserving the active singleton index and the existing replacement,
  backoff, wake-safety, and sidebar behavior.
- Add an AO-managed fleet workspace for Prime that is independent of the
  project registry; no hidden or synthetic project is created.
- Make enabling reconcile a singleton immediately and disabling retire the
  active Prime and stop future supervisor work without restarting AO.
- Move Prime prompt/config ownership out of `ProjectConfig`; legacy project
  Prime fields become ignored compatibility data until explicitly migrated or
  removed.
- Retire `AO_PRIME_PROJECT_ID` as an activation mechanism. Detect old
  environment/drop-in activation and expose an explicit migration warning
  rather than silently enabling persisted Prime.
- Keep fleet hard pause as the explicit emergency stop that can terminate
  Prime; project pause/removal has no effect on Prime lifecycle.

## Capabilities

### New Capabilities

- `fleet-prime-settings`: fleet-owned Prime configuration, projectless
  persistence/workspace, live enable/disable reconciliation, and legacy
  activation migration.

### Modified Capabilities

- `role-instructions`: Prime rules and effective-prompt inspection become
  fleet-scoped while project roles remain project-scoped.
- `fleet-pause`: clarify that project pause is irrelevant to Prime and only the
  explicit fleet hard pause terminates it.

## Impact

- **Storage:** new `daemon_settings` Prime config state and a migration making
  `sessions.project_id` nullable only for `kind='prime'`; project-bound session
  invariants remain enforced for workers/orchestrators.
- **Domain/service:** typed `PrimeSettings`, fleet settings service, projectless
  `SpawnPrime`, and live reconcile/retire behavior.
- **Workspace/session manager:** AO-managed fleet workspace and projectless
  prompt/env/identity paths for Prime.
- **HTTP/CLI/UI:** fleet Prime GET/PUT/enable/disable surfaces and a global
  Settings section; project settings stop presenting Prime-owned fields.
- **Operations:** `AO_PRIME_PROJECT_ID` drop-ins are detected as legacy
  configuration and require explicit operator migration/removal.
