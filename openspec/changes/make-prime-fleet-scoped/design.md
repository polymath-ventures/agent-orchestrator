## Context

Prime is already modeled as a fleet singleton at the session layer, but the
daemon still starts and configures it through `AO_PRIME_PROJECT_ID` and a
registered project record. That leaves production unable to start Prime when the
project registry is empty, and it lets unrelated project lifecycle actions
implicitly control a fleet-level session. Current Prime settings are split
between process environment (`AO_PRIME_PROJECT_ID`,
`AO_PRIME_DISPLAY_NAME`) and `ProjectConfig` fields (`prime`, `primeRules`,
`primeRulesFile`).

The existing durable fleet-pause implementation established the right storage
owner for daemon-global state: the single `daemon_settings` row. This change
extends that daemon-owned contract instead of creating a synthetic project or a
second settings source.

## Goals / Non-Goals

**Goals:**

- Persist one fleet-level `PrimeSettings` contract in daemon storage, disabled
  by default.
- Expose live enable/disable and configuration through daemon API, CLI, and the
  global Settings UI.
- Spawn and supervise Prime with no registered projects by using an AO-managed
  projectless workspace.
- Keep Prime's active singleton invariant, replacement budget, wake safety, and
  top-level sidebar identity.
- Make legacy `AO_PRIME_PROJECT_ID` activation visible as migration state
  without silently enabling persisted Prime.

**Non-Goals:**

- Multiple Prime instances or project-owned Prime.
- A hidden project row used only to satisfy existing session assumptions.
- A redesign of worker/orchestrator project ownership.
- New Prime authority to claim issues, dispatch workers, merge PRs, or command
  sessions.

## Decisions

### Store Prime settings in `daemon_settings`

Add Prime columns (or a typed JSON column plus generated accessors) to the
single-row `daemon_settings` table and expose them through a typed
`domain.PrimeSettings` value. The store must seed fresh installations with
`enabled=false` and validated daemon defaults for optional fields such as display
name and wake policy.

Alternative considered: store Prime settings in a dedicated `prime_settings`
table. That would work, but it adds another singleton table without improving
the ownership model. `daemon_settings` already represents fleet-global daemon
state and keeps settings reads/writes in one place.

### Make the supervisor settings-driven and live-reconciling

Replace the boot-only environment gate with a supervisor loop that reads
persisted Prime settings each tick. When disabled, it retires any active Prime
and skips future ensure, replacement, and wake work. When enabled, it ensures
one active Prime immediately, including on a zero-project daemon. Invalid
settings fail loud in the settings write path; supervisor reads treat store
errors as a skipped tick and log the failure.

Alternative considered: restart the daemon after settings changes. That keeps
less state in memory but fails the live toggle requirement and keeps production
operations coupled to service restarts.

### Use a projectless fleet workspace, not a synthetic project

Introduce a small `PrimeWorkspace` resolution path under AO-managed data, for
example `<AO_DATA_DIR>/prime/workspace`, with a deterministic branch/name
contract for the harness. Prime session records become nullable only for
`kind='prime'`; worker and orchestrator rows continue to require a real project.
API read models preserve `kind='prime'` and omit `projectId` for projectless
Prime.

Alternative considered: insert a hidden project row for Prime. That would reuse
existing spawn code, but it reintroduces the abstraction bug this change is
removing: project pause, archive, config, and registry semantics would again
look authoritative for Prime.

### Move Prime prompt ownership to fleet settings

Prime harness/model/effort, instructions/rules, rules file, display name, and
wake policy come from `PrimeSettings`. Worker, orchestrator, and reviewer
settings remain project-scoped. Existing project Prime fields are retained as
ignored compatibility data until a later cleanup, but new UI and CLI surfaces no
longer edit them.

Alternative considered: copy project Prime fields into daemon settings during
startup when `AO_PRIME_PROJECT_ID` is present. That silently changes permanent
state based on an old environment variable, which violates the migration
requirement and could re-enable Prime after an operator disabled it.

### Treat legacy environment activation as migration state only

`AO_PRIME_PROJECT_ID` stops enabling Prime. If present, the daemon exposes a
legacy-activation warning containing the configured project id and whether that
project still exists. Operators must explicitly save persisted Prime settings
and remove the drop-in. `AO_PRIME_DISPLAY_NAME` may be surfaced as a suggested
display name during migration but does not become canonical until saved.

Alternative considered: one-time auto-migration from env to persisted settings.
That would preserve old behavior, but it can silently enable a fleet singleton
on upgrade and conflicts with the fresh-install default of disabled.

## Risks / Trade-offs

- Existing session code assumes `ProjectID` is present for workspace, prompts,
  CDC, and PR attribution -> Add narrow projectless Prime branches and tests
  around every persistence/query path touched; keep workers/orchestrators on the
  existing required-project path.
- Nullable `sessions.project_id` can weaken data integrity -> Add a database
  CHECK so only `kind='prime'` may have an empty/null project id.
- API clients may assume every session has a project -> Preserve the top-level
  session shape and make the schema change explicit; sidebar tests cover global
  Prime rendering.
- Legacy drop-ins may remain installed after upgrade -> Surface migration state
  in API/CLI/UI and docs; do not let the env var override a persisted disabled
  toggle.
- A projectless workspace may lack git context expected by harnesses -> Create
  an AO-managed workspace contract with explicit instructions and no hidden
  project config; verify production boot with an empty project registry.

## Migration Plan

1. Add the storage migration and sqlc accessors for persisted Prime settings and
   projectless Prime sessions.
2. Deploy with Prime disabled by default, even when `AO_PRIME_PROJECT_ID` is
   present.
3. Surface a legacy activation warning with the old project id and suggested
   settings values.
4. Operators explicitly enable fleet Prime through the API/CLI/UI, then remove
   the old drop-in.
5. Rollback is disabling persisted Prime and restarting the previous daemon
   version with the old drop-in restored if needed.

## Open Questions

None. The issue decision defaults settle the product behavior: global Settings
owns Prime, the daemon database is canonical, upgraded installations do not
infer enablement from the old environment variable, and the implementation uses
the smallest projectless workspace contract rather than a synthetic project.
