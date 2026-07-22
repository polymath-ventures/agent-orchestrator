## MODIFIED Requirements

### Requirement: Harness resolution is authoritative at the daemon

The daemon SHALL be the authority for resolving an unpinned spawn's harness, model, and effort. A client SHALL be able to submit a spawn request naming neither harness nor model, and the daemon SHALL resolve them from the worker mix or, absent a mix, from project and role configuration. A worker spawn request that names a model but no harness SHALL also reach the daemon; when a worker mix is configured, the daemon SHALL select the harness from the mix and apply the explicit model to the resolved launch tuple. A client SHALL NOT reject an unpinned or model-only spawn on the grounds that a default agent is unconfigured, because a worker mix is itself a valid source of harness resolution.

#### Scenario: Unpinned request reaches the daemon

- **WHEN** a spawn is requested with no harness and no model
- **THEN** the request SHALL be transmitted with both absent
- **AND** the daemon SHALL perform the resolution

#### Scenario: Model-only request reaches the daemon

- **WHEN** a worker spawn is requested with a model but no harness
- **THEN** the request SHALL be transmitted with the harness absent
- **AND** the daemon SHALL perform harness resolution

#### Scenario: Client does not pre-reject a mix-only project

- **WHEN** a spawn is requested for a project that configures a worker mix but no default worker agent
- **THEN** the client SHALL NOT reject the request
- **AND** the spawn SHALL succeed

#### Scenario: Unresolvable spawn fails at the daemon

- **WHEN** a spawn is requested for a project with neither a worker mix nor a configured worker agent
- **THEN** the daemon SHALL reject it with an unresolvable-agent error

### Requirement: Agent readiness checks follow the resolved candidate

Any pre-launch agent readiness or authentication check SHALL be performed against the harness actually resolved for the spawn. When resolution is performed by the daemon, the check SHALL NOT be performed by the client against a harness the client guessed. For worker-mix selections, readiness and model-selection failures attributable to the resolved candidate SHALL mark down that exact candidate and SHALL NOT mark any other bucket down.

#### Scenario: Readiness is checked against the selected bucket

- **WHEN** an unpinned spawn resolves to a mix bucket
- **THEN** the readiness check SHALL be performed for that bucket's harness

#### Scenario: Readiness failure marks the candidate down

- **WHEN** the readiness check fails for a mix-selected bucket
- **THEN** that bucket's candidate SHALL be marked down

#### Scenario: Similar buckets are not marked down

- **WHEN** readiness fails for one mix-selected harness, model, and effort tuple
- **THEN** no other configured bucket SHALL be marked down

## ADDED Requirements

### Requirement: Resolved spawn selections are validated before durable state

Before creating a session row or workspace, the daemon SHALL validate the resolved launch tuple `(harness, model, effort)` using cached model availability. A definitive unreachable verdict SHALL reject the spawn with a distinguishable model-unreachable error that names the harness, model, effort, and pin source. Missing, stale, unknown, or probe-unavailable verdicts SHALL fail open with a warning and SHALL NOT perform paid or blocking probes on the spawn path.

#### Scenario: Definitive spawn rejection names source

- **WHEN** the resolved launch tuple is definitively unreachable
- **THEN** the spawn SHALL fail before creating durable session state
- **AND** the error SHALL name whether the selection came from an explicit spawn model, a worker-mix bucket, or project/role config

#### Scenario: Unknown spawn verdict fails open

- **WHEN** cached model availability has no definitive verdict for the resolved tuple
- **THEN** the spawn SHALL continue
- **AND** the daemon SHALL emit a warning naming the unresolved tuple

#### Scenario: Spawn validation is cache-only

- **WHEN** spawn-time validation runs
- **THEN** it SHALL NOT perform a paid model probe or any blocking provider call
