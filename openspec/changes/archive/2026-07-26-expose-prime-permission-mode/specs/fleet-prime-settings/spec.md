## MODIFIED Requirements

### Requirement: Prime configuration is fleet-owned

The system SHALL resolve Prime harness, model, effort, permission mode, instructions/rules,
rules file, display name, and wake policy from fleet Prime settings.
Project-level Prime fields SHALL NOT control newly spawned Prime sessions.

#### Scenario: New Prime uses fleet settings

- **WHEN** fleet Prime settings specify a harness/model/effort, permission mode, and display name
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
harness-aware model and effort picker behavior, SHALL expose permission mode,
and SHALL present wake interval as numeric minutes while saving the existing
duration-string contract.

#### Scenario: API and CLI share persisted settings

- **WHEN** an operator enables Prime through the CLI with a permission mode
- **THEN** the global Settings UI reads Prime as enabled with that permission mode from the daemon API
- **AND** disabling Prime through the UI makes the CLI report disabled

#### Scenario: Global Settings controls Prime

- **WHEN** an operator toggles Enable Prime in global Settings
- **THEN** the UI saves the persisted daemon setting
- **AND** the daemon reconciles Prime lifecycle from that setting

#### Scenario: Prime settings use shared model controls

- **WHEN** the operator edits Prime harness, model, or effort in global Settings
- **THEN** the UI uses the same known-model dropdown, effort options, custom model entry path, and custom model warning used by other role model selectors

#### Scenario: Prime permission mode is editable

- **WHEN** the operator edits Prime permission mode in global Settings or with `ao prime enable --permission` / `ao prime set --permission`
- **THEN** the saved Prime settings update `agentConfig.permissions`
- **AND** the setting uses the same permission-mode vocabulary as project role settings

#### Scenario: Prime wake interval is edited in minutes

- **WHEN** the operator edits Prime wake interval in global Settings
- **THEN** the UI presents a numeric minutes value
- **AND** saving converts that value to the existing `wakeInterval` duration field

#### Scenario: Enablement copy describes Prime's role correctly

- **WHEN** the operator views the Prime enablement control in global Settings
- **THEN** the toggle reads `Enable Prime`
- **AND** the enabled description states that Prime supervises the fleet globally
