## MODIFIED Requirements

### Requirement: Hard pause terminates live workers immediately

A hard pause SHALL immediately terminate the live workers of the gated scope
rather than waiting for them to drain. A hard fleet pause SHALL fan out
termination across all projects and, because it is the fleet-wide emergency
stop, SHALL ALSO terminate orchestrator sessions and the fleet Prime singleton.
A hard **per-project** pause SHALL terminate that project's live workers but
SHALL spare its orchestrator and SHALL NOT affect fleet Prime, so supervision of
the remaining fleet continues. Termination SHALL use the clean session-teardown
path and SHALL be best-effort: a failure terminating one session SHALL NOT
prevent termination of the others. A hard pause SHALL be confirmed in the UI
before it is issued.

#### Scenario: Hard project pause kills live workers now

- **WHEN** a project is hard-paused
- **THEN** its live (non-terminated) workers are terminated immediately rather than left to drain

#### Scenario: Hard project pause spares the orchestrator

- **WHEN** a single project is hard-paused and it has a live orchestrator
- **THEN** the orchestrator is not terminated and continues supervising

#### Scenario: Hard project pause does not terminate Prime

- **WHEN** a single project is hard-paused and a fleet Prime is active
- **THEN** the fleet Prime is not terminated by the project pause

#### Scenario: Hard fleet pause fans out across all projects

- **WHEN** the fleet is hard-paused
- **THEN** live workers across every project are terminated immediately

#### Scenario: Hard fleet pause is an emergency stop that also kills orchestrators

- **WHEN** the fleet is hard-paused and one or more projects have live orchestrators
- **THEN** those orchestrators are also terminated, so no project orchestrator keeps running after the fleet-wide emergency stop

#### Scenario: Hard fleet pause terminates Prime

- **WHEN** the fleet is hard-paused and a fleet Prime is active
- **THEN** Prime is terminated as part of the fleet-wide emergency stop

#### Scenario: Hard drain is best-effort across sessions

- **WHEN** a hard pause terminates multiple sessions and one termination fails
- **THEN** the remaining sessions are still terminated and the failure is reported in aggregate

### Requirement: Orchestrators and privileged spawns are exempt from pause

Pause SHALL gate only ordinary worker intake and spawns. Orchestrator and fleet
Prime sessions SHALL remain alive and continue to be spawnable while a scope is
paused in **soft** mode or under a **per-project hard** pause, so alerting and
supervision keep running. The sole exception is a **fleet-wide hard pause**, the
deliberate emergency stop, which terminates orchestrators and Prime as well (see
"Hard pause terminates live workers immediately"). A forced spawn SHALL override
the pause guard.

#### Scenario: Orchestrator stays alive under soft or per-project-hard pause

- **WHEN** a scope is paused in soft mode or a single project is hard-paused
- **THEN** orchestrator sessions are not terminated by the pause and continue running

#### Scenario: Prime stays alive under soft or per-project-hard pause

- **WHEN** a scope is paused in soft mode or a single project is hard-paused
- **THEN** fleet Prime is not terminated by the pause and continues running

#### Scenario: Fleet hard pause is the exception that stops orchestrators

- **WHEN** the fleet is hard-paused
- **THEN** orchestrator sessions are terminated as part of the emergency stop

#### Scenario: Fleet hard pause is the exception that stops Prime

- **WHEN** the fleet is hard-paused
- **THEN** fleet Prime is terminated as part of the emergency stop

#### Scenario: Forced spawn overrides the guard

- **WHEN** a spawn is requested with the force flag for a gated project
- **THEN** the spawn is allowed despite the pause
