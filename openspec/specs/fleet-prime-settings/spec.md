# fleet-prime-settings Specification

## Purpose

Defines the fleet-owned configuration and lifecycle contract for Prime.

## Requirements

### Requirement: Fleet Prime settings are persisted daemon state

The daemon SHALL persist one fleet-level Prime settings contract outside project
configuration. The contract SHALL include enablement, display name,
harness/model/effort, Prime instructions/rules, Prime rules file, and wake
policy. Fresh installations SHALL default Prime to disabled. Prime wake
interval settings SHALL preserve the existing duration-string API/storage shape
and SHALL reject values below 1 minute or above 360 minutes.

#### Scenario: Fresh install starts disabled

- **WHEN** the daemon database is initialized with no prior Prime settings
- **THEN** the persisted Prime settings report Prime as disabled
- **AND** the Prime supervisor does not spawn a Prime session

#### Scenario: Settings round trip through storage

- **WHEN** an operator saves fleet Prime settings
- **THEN** a daemon restart reads the same settings from daemon storage
- **AND** no project configuration file is read or rewritten to recover those settings

#### Scenario: Wake interval bounds are enforced

- **WHEN** an operator saves fleet Prime settings with a wake interval below 1 minute or above 360 minutes
- **THEN** the settings write is rejected with a validation error

### Requirement: Fleet Prime is live-controllable

The daemon SHALL use persisted Prime settings as the single source of truth for
Prime lifecycle. Enabling Prime SHALL immediately ensure exactly one active
Prime. Disabling Prime SHALL retire any active Prime and stop future ensure,
replacement, and wake attempts without requiring a daemon restart. Ensuring Prime
SHALL be performed through the shared role-session reconciliation contract, so
that a terminated Prime row, a stale worktree holding the canonical Prime branch,
or a leaked Prime runtime does not prevent Prime from being ensured.

#### Scenario: Enabling creates Prime without projects

- **WHEN** the project registry is empty and persisted Prime settings are saved with `enabled=true`
- **THEN** the supervisor starts exactly one active Prime session
- **AND** the spawn does not require a registered project id

#### Scenario: Disabling retires active Prime

- **WHEN** an active Prime exists and persisted Prime settings are saved with `enabled=false`
- **THEN** the daemon retires the active Prime through the clean teardown path
- **AND** subsequent supervisor ticks do not spawn, replace, or wake Prime while disabled

#### Scenario: Singleton remains storage-enforced

- **WHEN** two supervisor loops concurrently attempt to ensure Prime
- **THEN** storage permits at most one non-terminated session with `kind='prime'`
- **AND** both callers observe the same active singleton after reconciliation

#### Scenario: Enabling recovers from stale terminated Prime state

- **WHEN** Prime is enabled while the newest Prime row is terminated and its worktree still holds the
  canonical Prime branch
- **THEN** ensuring Prime releases that stale resource and creates an active Prime
- **AND** no manual worktree or runtime cleanup is required first

### Requirement: Prime sessions are projectless

Prime sessions SHALL use an AO-managed fleet workspace and SHALL NOT require,
create, pause, archive, or remove any registered project. Worker and
orchestrator sessions SHALL continue to require a project.

#### Scenario: Prime has no project owner

- **WHEN** the daemon spawns a fleet Prime
- **THEN** the session is persisted with `kind='prime'` and no project owner
- **AND** the session workspace is the AO-managed fleet workspace

#### Scenario: Non-Prime sessions still require projects

- **WHEN** a worker or orchestrator session is spawned
- **THEN** storage and service validation require a registered project id

#### Scenario: Project lifecycle does not control Prime

- **WHEN** a project is added, paused, archived, or removed
- **THEN** that project lifecycle action does not enable, disable, retire, or wake Prime

### Requirement: Prime configuration is fleet-owned

The system SHALL resolve Prime harness, model, effort, instructions/rules, rules
file, display name, and wake policy from fleet Prime settings. Project-level
Prime fields SHALL NOT control newly spawned Prime sessions.

#### Scenario: New Prime uses fleet settings

- **WHEN** fleet Prime settings specify a harness/model/effort and display name
- **THEN** the next Prime spawn uses those fleet-scoped values
- **AND** project config Prime fields are ignored for that spawn

#### Scenario: Wake policy uses fleet settings

- **WHEN** fleet Prime settings specify a wake interval and backoff policy
- **THEN** supervisor wake eligibility and repeat wake spacing use that fleet policy

### Requirement: Prime control is exposed through API, CLI, and global Settings

The daemon SHALL expose headless API and CLI controls for reading and updating
fleet Prime settings, and an API control for relaunching Prime. The global
Settings UI SHALL expose an Enable Prime toggle and editable Prime configuration
backed by the same API. The enablement control SHALL describe Prime as
supervising the fleet globally rather than as being supervised globally. Prime
settings UI SHALL label runtime selection as Harness, SHALL use the shared
harness-aware model and effort picker behavior, and SHALL present wake interval
as numeric minutes while saving the existing duration-string contract.

#### Scenario: API and CLI share persisted settings

- **WHEN** an operator enables Prime through the CLI
- **THEN** the global Settings UI reads Prime as enabled from the daemon API
- **AND** disabling Prime through the UI makes the CLI report disabled

#### Scenario: Global Settings controls Prime

- **WHEN** an operator toggles Enable Prime in global Settings
- **THEN** the UI saves the persisted daemon setting
- **AND** the daemon reconciles Prime lifecycle from that setting

#### Scenario: Prime settings use shared model controls

- **WHEN** the operator edits Prime harness, model, or effort in global Settings
- **THEN** the UI uses the same known-model dropdown, effort options, custom model entry path, and custom model warning used by other role model selectors

#### Scenario: Prime wake interval is edited in minutes

- **WHEN** the operator edits Prime wake interval in global Settings
- **THEN** the UI presents a numeric minutes value
- **AND** saving converts that value to the existing `wakeInterval` duration field

#### Scenario: Enablement copy describes Prime's role correctly

- **WHEN** the operator views the Prime enablement control in global Settings
- **THEN** the toggle reads `Enable Prime`
- **AND** the enabled description states that Prime supervises the fleet globally

### Requirement: Legacy environment activation requires explicit migration

`AO_PRIME_PROJECT_ID` SHALL NOT silently enable Prime. Persisted fleet Prime
settings SHALL remain authoritative, and the daemon SHALL NOT expose legacy
Prime environment or project migration warning state through Prime settings
API, CLI, or UI surfaces.

#### Scenario: Legacy env does not enable Prime

- **WHEN** `AO_PRIME_PROJECT_ID` is set and persisted Prime settings are disabled
- **THEN** the daemon does not spawn Prime from the environment variable
- **AND** Prime settings API, CLI, and UI responses do not report a legacy project warning

### Requirement: Prime remains a global sidebar session

The supervisor UI SHALL show Prime as one global top-level entry, independent of
the project list, whenever Prime is enabled in persisted settings. That entry's
presence SHALL be derived from persisted Prime settings rather than from the
existence of a live Prime session row, and live session rows SHALL be used only
to determine which Prime state the entry and its surface present.

#### Scenario: Projectless Prime appears globally

- **WHEN** a projectless active Prime session exists
- **THEN** the sidebar renders it as the global Prime entry
- **AND** it is not nested under any project

#### Scenario: Enabled Prime stays visible without a live session

- **WHEN** Prime is enabled in persisted settings and no active Prime session exists
- **THEN** the sidebar still renders the global Prime entry
- **AND** selecting it opens the Prime surface rather than removing the entry

#### Scenario: Disabled Prime is not shown

- **WHEN** Prime is disabled in persisted settings
- **THEN** the sidebar does not render a global Prime entry

### Requirement: Prime relaunch is an explicit user-initiated operation

The daemon SHALL expose an explicit Prime relaunch operation that triggers role-session
reconciliation for Prime immediately. Relaunch SHALL be idempotent with respect to the Prime
singleton, SHALL clear restart-budget-paused replacement state, and SHALL NOT wait for a later
supervisor tick or backoff window. Relaunch SHALL remain distinct from generic session restore and
from manual session spawn, both of which SHALL continue to be rejected for Prime.

#### Scenario: Relaunch recovers a terminated Prime immediately

- **WHEN** Prime is enabled, no active Prime exists, and an operator invokes Prime relaunch
- **THEN** the daemon reconciles Prime without waiting for the next supervisor tick
- **AND** an active Prime session exists after the operation completes

#### Scenario: Relaunch clears budget-paused replacement state

- **WHEN** automatic Prime replacement is paused because the restart budget is exhausted and an
  operator invokes Prime relaunch
- **THEN** the paused replacement state is cleared
- **AND** the daemon attempts reconciliation rather than reporting the budget as still exhausted

#### Scenario: Relaunch does not create a second Prime

- **WHEN** an operator invokes Prime relaunch while a healthy active Prime already exists
- **THEN** storage still permits at most one non-terminated session with `kind='prime'`

#### Scenario: Manual spawn and restore remain forbidden for Prime

- **WHEN** a client attempts a manual session spawn with `kind='prime'` or a generic session restore
  of a Prime session
- **THEN** the daemon rejects the request as it does today
- **AND** relaunch remains the only supported user-initiated Prime recovery operation

#### Scenario: Settings save triggers immediate reconciliation

- **WHEN** an operator saves fleet Prime settings with `enabled=true` after previously saving
  `enabled=false`
- **THEN** the daemon reconciles Prime immediately rather than depending on the disabled window
  overlapping a supervisor tick
- **AND** any restart-budget-paused state from before the toggle is cleared

### Requirement: The supervisor UI presents a recoverable Prime not-running state

While Prime is enabled, the supervisor UI SHALL present a Prime surface even when no active Prime
session exists. That surface SHALL explain that Prime is enabled but not currently running and SHALL
offer one primary `Relaunch Prime` action. An ended Prime terminal SHALL offer Prime relaunch rather
than the generic session restore affordance. Notifications about exhausted automatic replacement
SHALL direct the operator to the Prime surface or the relaunch action rather than instructing them to
inspect an active Prime that may not exist.

#### Scenario: Prime board explains the not-running state

- **WHEN** Prime is enabled in persisted settings and no active Prime session exists
- **THEN** the Prime surface reports that Prime is enabled but not running
- **AND** it presents `Relaunch Prime` as the primary action

#### Scenario: Dead Prime terminal offers relaunch

- **WHEN** an operator opens a terminated Prime session's terminal
- **THEN** the UI offers Prime relaunch
- **AND** it does not offer the generic session restore affordance that the daemon rejects for Prime

#### Scenario: Budget-exhausted notification points at recovery

- **WHEN** the daemon notifies that automatic Prime replacement is exhausted
- **THEN** the notification directs the operator to the Prime surface or the relaunch action
- **AND** it does not instruct the operator to inspect an active Prime

### Requirement: A missing project Orchestrator is recoverable from the supervisor UI

When a project has no active Orchestrator, the supervisor UI SHALL present an action that spawns or
restarts the Orchestrator through the Orchestrator spawn/restart path. Ended Orchestrator terminals
and restore-unavailable states SHALL route to that path rather than to generic worker restore
behavior, and SHALL NOT navigate the operator back to a terminated session's terminal as the
resolution.

#### Scenario: Missing Orchestrator offers an action

- **WHEN** a project's Orchestrator is missing or terminated
- **THEN** the project's board presents an action that spawns or restarts the Orchestrator

#### Scenario: Dead Orchestrator terminal routes to the Orchestrator path

- **WHEN** an operator reaches a terminated Orchestrator session's terminal or a restore-unavailable
  state for that session
- **THEN** the offered recovery routes through the Orchestrator spawn/restart path
- **AND** the operator is not returned to the dead session's terminal as the outcome
