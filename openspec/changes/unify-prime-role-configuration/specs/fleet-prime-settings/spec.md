## MODIFIED Requirements

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

### Requirement: Prime control is exposed through API, CLI, and global Settings

The daemon SHALL expose headless API and CLI controls for reading and updating
fleet Prime settings. The global Settings UI SHALL expose an Enable fleet Prime
toggle and editable Prime configuration backed by the same API. Prime settings
UI SHALL label runtime selection as Harness, SHALL use the shared
harness-aware model and effort picker behavior, and SHALL present wake interval
as numeric minutes while saving the existing duration-string contract.

#### Scenario: API and CLI share persisted settings

- **WHEN** an operator enables Prime through the CLI
- **THEN** the global Settings UI reads Prime as enabled from the daemon API
- **AND** disabling Prime through the UI makes the CLI report disabled

#### Scenario: Global Settings controls Prime

- **WHEN** an operator toggles Enable fleet Prime in global Settings
- **THEN** the UI saves the persisted daemon setting
- **AND** the daemon reconciles Prime lifecycle from that setting

#### Scenario: Prime settings use shared model controls

- **WHEN** the operator edits Prime harness, model, or effort in global Settings
- **THEN** the UI uses the same known-model dropdown, effort options, custom model entry path, and custom model warning used by other role model selectors

#### Scenario: Prime wake interval is edited in minutes

- **WHEN** the operator edits Prime wake interval in global Settings
- **THEN** the UI presents a numeric minutes value
- **AND** saving converts that value to the existing `wakeInterval` duration field

### Requirement: Legacy environment activation requires explicit migration

`AO_PRIME_PROJECT_ID` SHALL NOT silently enable Prime. Persisted fleet Prime
settings SHALL remain authoritative, and the daemon SHALL NOT expose legacy
Prime environment or project migration warning state through Prime settings
API, CLI, or UI surfaces.

#### Scenario: Legacy env does not enable Prime

- **WHEN** `AO_PRIME_PROJECT_ID` is set and persisted Prime settings are disabled
- **THEN** the daemon does not spawn Prime from the environment variable
- **AND** Prime settings API, CLI, and UI responses do not report a legacy project warning
