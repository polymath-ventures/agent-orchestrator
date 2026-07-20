## ADDED Requirements

### Requirement: Model is an explicit spawn input

A spawn request SHALL accept an optional model alongside the optional harness, through both the daemon API and the command-line client. When a model is supplied it SHALL be used for the launched session, taking precedence over any model derived from project or role configuration. When it is omitted, model resolution SHALL follow the existing configuration precedence unchanged.

#### Scenario: Explicit model is honored

- **WHEN** a spawn request supplies a model
- **THEN** the launched session SHALL use that model
- **AND** SHALL NOT use the model from role or project configuration

#### Scenario: Omitted model falls back to configuration

- **WHEN** a spawn request supplies no model
- **THEN** the model SHALL be resolved from role and project configuration as before

#### Scenario: Model is settable from the CLI

- **WHEN** a user passes an explicit model flag to the spawn command
- **THEN** that model SHALL be transmitted to the daemon

### Requirement: Sessions record their model

A session record SHALL persist the model it was launched with, so that the live per-bucket census can be computed from stored session state. Sessions created before this capability existed, and sessions launched with no resolved model, SHALL carry an empty model and SHALL remain readable.

#### Scenario: Launched model is persisted

- **WHEN** a session is spawned with a resolved model
- **THEN** that model SHALL be stored on the session record
- **AND** SHALL be returned when the session is read

#### Scenario: Pre-existing sessions remain readable

- **WHEN** a session created before this capability is read
- **THEN** it SHALL be returned successfully with an empty model

#### Scenario: Census counts by harness and model

- **WHEN** the live worker census is computed for a project
- **THEN** each live worker SHALL be attributed to the bucket matching its harness and model

### Requirement: Harness resolution is authoritative at the daemon

The daemon SHALL be the authority for resolving an unpinned spawn's harness and model. A client SHALL be able to submit a spawn request naming neither, and the daemon SHALL resolve them from the worker mix or, absent a mix, from project and role configuration. A client SHALL NOT reject an unpinned spawn on the grounds that a default agent is unconfigured, because a worker mix is itself a valid source of that resolution.

#### Scenario: Unpinned request reaches the daemon

- **WHEN** a spawn is requested with no harness and no model
- **THEN** the request SHALL be transmitted with both absent
- **AND** the daemon SHALL perform the resolution

#### Scenario: Client does not pre-reject a mix-only project

- **WHEN** a spawn is requested for a project that configures a worker mix but no default worker agent
- **THEN** the client SHALL NOT reject the request
- **AND** the spawn SHALL succeed

#### Scenario: Unresolvable spawn fails at the daemon

- **WHEN** a spawn is requested for a project with neither a worker mix nor a configured worker agent
- **THEN** the daemon SHALL reject it with an unresolvable-agent error

### Requirement: Agent readiness checks follow the resolved candidate

Any pre-launch agent readiness or authentication check SHALL be performed against the harness actually resolved for the spawn. When resolution is performed by the daemon, the check SHALL NOT be performed by the client against a harness the client guessed.

#### Scenario: Readiness is checked against the selected bucket

- **WHEN** an unpinned spawn resolves to a mix bucket
- **THEN** the readiness check SHALL be performed for that bucket's harness

#### Scenario: Readiness failure marks the candidate down

- **WHEN** the readiness check fails for a mix-selected bucket
- **THEN** that bucket's candidate SHALL be marked down
