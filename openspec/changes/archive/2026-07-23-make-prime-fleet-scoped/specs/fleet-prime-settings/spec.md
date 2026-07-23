## ADDED Requirements

### Requirement: Fleet Prime settings are persisted daemon state

The daemon SHALL persist one fleet-level Prime settings contract outside project
configuration. The contract SHALL include enablement, display name,
harness/model/effort, Prime instructions/rules, Prime rules file, and wake
policy. Fresh installations SHALL default Prime to disabled.

#### Scenario: Fresh install starts disabled

- **WHEN** the daemon database is initialized with no prior Prime settings
- **THEN** the persisted Prime settings report Prime as disabled
- **AND** the Prime supervisor does not spawn a Prime session

#### Scenario: Settings round trip through storage

- **WHEN** an operator saves fleet Prime settings
- **THEN** a daemon restart reads the same settings from daemon storage
- **AND** no project configuration file is read or rewritten to recover those settings

### Requirement: Fleet Prime is live-controllable

The daemon SHALL use persisted Prime settings as the single source of truth for
Prime lifecycle. Enabling Prime SHALL immediately ensure exactly one active
Prime. Disabling Prime SHALL retire any active Prime and stop future ensure,
replacement, and wake attempts without requiring a daemon restart.

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
fleet Prime settings. The global Settings UI SHALL expose an Enable fleet Prime
toggle and editable Prime configuration backed by the same API.

#### Scenario: API and CLI share persisted settings

- **WHEN** an operator enables Prime through the CLI
- **THEN** the global Settings UI reads Prime as enabled from the daemon API
- **AND** disabling Prime through the UI makes the CLI report disabled

#### Scenario: Global Settings controls Prime

- **WHEN** an operator toggles Enable fleet Prime in global Settings
- **THEN** the UI saves the persisted daemon setting
- **AND** the daemon reconciles Prime lifecycle from that setting

### Requirement: Legacy environment activation requires explicit migration

`AO_PRIME_PROJECT_ID` SHALL NOT silently enable Prime. When legacy Prime
environment activation is present, the daemon SHALL expose auditable migration
state so an operator can explicitly save fleet Prime settings and remove the old
drop-in. A persisted disabled toggle SHALL remain authoritative even when the
legacy environment variable is still present.

#### Scenario: Legacy env does not enable Prime

- **WHEN** `AO_PRIME_PROJECT_ID` is set and persisted Prime settings are disabled
- **THEN** the daemon does not spawn Prime from the environment variable
- **AND** the legacy project id is reported as migration state

#### Scenario: Persisted disable wins after migration

- **WHEN** Prime was previously enabled through persisted settings and an operator later saves `enabled=false`
- **THEN** `AO_PRIME_PROJECT_ID` cannot re-enable Prime on the next daemon restart

#### Scenario: Operator can audit migration

- **WHEN** the daemon detects legacy Prime environment activation
- **THEN** the API, CLI, or global Settings surface reports the legacy project id and that explicit migration is required

### Requirement: Prime remains a global sidebar session

The supervisor UI SHALL continue to show the active Prime singleton as one
global top-level session, independent of the project list.

#### Scenario: Projectless Prime appears globally

- **WHEN** a projectless active Prime session exists
- **THEN** the sidebar renders it as the global Prime entry
- **AND** it is not nested under any project
