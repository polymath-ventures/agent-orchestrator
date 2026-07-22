## MODIFIED Requirements

### Requirement: Hard pause terminates live workers immediately

A hard pause SHALL immediately terminate the live workers of the gated scope
rather than waiting for them to drain. A hard fleet pause SHALL fan out
termination across all projects and, because it is the fleet-wide emergency
stop, SHALL ALSO terminate orchestrator sessions. A hard **per-project** pause
SHALL terminate that project's live workers but SHALL spare its orchestrator so
supervision of the remaining fleet continues. Termination SHALL use the clean
session-teardown path and SHALL be best-effort: a failure terminating one
session SHALL NOT prevent termination of the others. A hard pause SHALL be
confirmed in the UI before it is issued.

#### Scenario: Hard project pause kills live workers now

- **WHEN** a project is hard-paused
- **THEN** its live (non-terminated) workers are terminated immediately rather than left to drain

#### Scenario: Hard project pause spares the orchestrator

- **WHEN** a single project is hard-paused and it has a live orchestrator
- **THEN** the orchestrator is not terminated and continues supervising

#### Scenario: Hard fleet pause fans out across all projects

- **WHEN** the fleet is hard-paused
- **THEN** live workers across every project are terminated immediately

#### Scenario: Hard fleet pause is an emergency stop that also kills orchestrators

- **WHEN** the fleet is hard-paused and one or more projects have live orchestrators
- **THEN** those orchestrators are also terminated, so no session keeps running after the fleet-wide emergency stop

#### Scenario: Hard drain is best-effort across sessions

- **WHEN** a hard pause terminates multiple sessions and one termination fails
- **THEN** the remaining sessions are still terminated and the failure is reported in aggregate

### Requirement: Orchestrators and privileged spawns are exempt from pause

Pause SHALL gate only ordinary worker intake and spawns. Orchestrator (and
equivalently privileged prime-tier) sessions SHALL remain alive and continue to
be spawnable while a scope is paused in **soft** mode or under a **per-project
hard** pause, so alerting and supervision keep running. The sole exception is a
**fleet-wide hard pause**, the deliberate emergency stop, which terminates
orchestrators as well (see "Hard pause terminates live workers immediately").
A forced spawn SHALL override the pause guard.

#### Scenario: Orchestrator stays alive under soft or per-project-hard pause

- **WHEN** a scope is paused in soft mode or a single project is hard-paused
- **THEN** orchestrator sessions are not terminated by the pause and continue running

#### Scenario: Fleet hard pause is the exception that stops orchestrators

- **WHEN** the fleet is hard-paused
- **THEN** orchestrator sessions are terminated as part of the emergency stop

#### Scenario: Forced spawn overrides the guard

- **WHEN** a spawn is requested with the force flag for a gated project
- **THEN** the spawn is allowed despite the pause

### Requirement: Pause is controllable through the API and CLI

The daemon SHALL expose HTTP routes to read fleet pause status and to set/clear
fleet and per-project pause, accepting a hard-mode parameter on pause. The CLI
SHALL provide `pause` and `resume` commands scoped to a project argument or to the
whole fleet, with a hard-mode flag on pause, and SHALL surface pause state in
status output. The `pause` command SHALL document its drain semantics in its help
text and SHALL reject an empty/whitespace project id as a usage error before
contacting the daemon. An operator SHALL be able to escalate an already-active
(soft) pause to a hard pause without first resuming, and the hard-pause
confirmation SHALL state the true blast radius of the scope being hard-paused
(for a fleet hard pause, that orchestrators are terminated too).

#### Scenario: CLI pauses the fleet

- **WHEN** the operator runs the fleet-scoped pause command
- **THEN** the fleet flag is set and the command reports the fleet as paused

#### Scenario: CLI pauses a single project hard

- **WHEN** the operator runs the project-scoped pause command with the hard flag
- **THEN** that project is paused, its live workers are terminated immediately, and the command reports the resulting pause state

#### Scenario: CLI rejects a blank project id

- **WHEN** the operator runs the pause command with an empty or whitespace-only project id
- **THEN** the command exits with a usage error and does not contact the daemon

#### Scenario: Operator escalates a running drain to a hard stop

- **WHEN** a scope is already soft-paused and draining and the operator issues a hard pause for the same scope
- **THEN** the hard pause is accepted (no resume required) and live workers are terminated immediately

#### Scenario: Status surfaces pause state

- **WHEN** a scope is paused and the operator queries status
- **THEN** the reported status reflects the paused (or draining) state

## ADDED Requirements

### Requirement: Pause enforcement fails closed

The daemon SHALL fail closed at its authoritative pause enforcement points —
the worker spawn guard and the tracker-intake observer — when it cannot read the
fleet pause flag. If reading the persisted fleet flag returns an error, the spawn
guard SHALL refuse the spawn (surfacing the error) and the intake observer SHALL
abort the current tick, rather than proceeding as though the fleet were running.
Read-only display paths that merely report derived pause state MAY continue to
fail open to "not paused" so that a transient storage error does not wedge the
UI; only the enforcement paths are required to fail closed.

#### Scenario: Spawn guard refuses when the pause flag cannot be read

- **WHEN** a worker spawn is requested and reading the fleet pause flag fails
- **THEN** the spawn is refused rather than allowed

#### Scenario: Intake tick aborts when the pause flag cannot be read

- **WHEN** the tracker-intake observer runs and reading the fleet pause flag fails
- **THEN** the tick aborts without intaking new work rather than dispatching it

#### Scenario: Display path still tolerates a read blip

- **WHEN** a read-only pause-state display computation cannot read the flag
- **THEN** it MAY report "not paused" so the UI is not wedged, without affecting the enforcement paths
