## ADDED Requirements

### Requirement: Pause state is stored outside project config

The daemon SHALL persist pause state in daemon-owned storage separate from the
user-authored project configuration. A pause followed by a resume SHALL leave
every affected project's configuration file byte-for-byte identical to its state
before the pause. Writing or clearing a pause bit SHALL NOT rewrite, reformat, or
otherwise mutate project config.

#### Scenario: Pause/resume cycle leaves config unchanged

- **WHEN** a project's configuration file is recorded, the project is paused, then resumed
- **THEN** the project's configuration file is byte-for-byte identical to the recorded copy

#### Scenario: Config save does not alter the pause bit

- **WHEN** a project is paused and its configuration is subsequently saved through the normal config-write path
- **THEN** the project remains paused (the config-write path does not clear or set the pause bit)

### Requirement: Two independent pause scopes

The daemon SHALL maintain two independent pause flags: a single global fleet flag
and a per-project flag. A project SHALL be treated as paused when its own flag is
set OR when the global fleet flag is set. The fleet flag SHALL be stored
independently of project rows so that it applies regardless of which projects
exist.

#### Scenario: Project-scoped pause affects only that project

- **WHEN** one project is paused and the fleet flag is clear
- **THEN** that project is gated and all other projects continue normal operation

#### Scenario: Fleet pause affects all projects

- **WHEN** the fleet flag is set
- **THEN** every project is gated regardless of its own per-project flag

#### Scenario: Project registered during a fleet pause is gated from its first moment

- **WHEN** the fleet flag is set and a new project is then registered
- **THEN** the new project is gated immediately, with no window in which it can intake or spawn work

### Requirement: Soft pause gates new work and drains at idle

A soft pause SHALL stop the creation of new work while preserving work already in
flight. While a scope is soft-paused, the daemon SHALL NOT intake new tracker
work for gated projects and SHALL NOT spawn new worker sessions for gated
projects. Workers already running SHALL be allowed to reach an idle or terminal
state, at which point a drain sweeper SHALL terminate them. Mid-flight worker
work SHALL NOT be interrupted by a soft pause.

#### Scenario: No new intake while paused

- **WHEN** a scope is soft-paused and the tracker-intake observer runs
- **THEN** no new work is intaken for any gated project

#### Scenario: No new spawns while paused

- **WHEN** a scope is soft-paused and a spawn is requested for a gated project through the normal (non-forced, worker-tier) path
- **THEN** the spawn is refused

#### Scenario: Running worker is preserved until idle

- **WHEN** a scope is soft-paused and a gated project has a worker in an actively-working state
- **THEN** the worker is not terminated and continues until it reaches an idle or terminal state

#### Scenario: Drain terminates the worker once idle

- **WHEN** a soft-paused project's worker reaches a drainable (idle/terminal) state
- **THEN** the drain sweeper terminates that worker through the clean session-teardown path

### Requirement: Hard pause terminates live workers immediately

A hard pause SHALL immediately terminate the live workers of the gated scope
rather than waiting for them to drain. A hard fleet pause SHALL fan out
termination across all projects. Termination SHALL use the clean session-teardown
path. A hard pause SHALL be confirmed in the UI before it is issued.

#### Scenario: Hard project pause kills live workers now

- **WHEN** a project is hard-paused
- **THEN** its live (non-terminated) workers are terminated immediately rather than left to drain

#### Scenario: Hard fleet pause fans out across all projects

- **WHEN** the fleet is hard-paused
- **THEN** live workers across every project are terminated immediately

#### Scenario: UI confirms before a hard pause

- **WHEN** an operator selects "pause now (hard)" in the UI
- **THEN** the UI requires an explicit confirmation before the hard pause is issued

### Requirement: Resume restores normal operation

Clearing a scope's pause flag SHALL restore normal intake and spawn behavior for
the affected projects. Resume SHALL NOT accept a hard mode (there is nothing to
terminate on resume).

#### Scenario: Resume re-enables intake and spawns

- **WHEN** a paused scope is resumed and no other scope still gates the project
- **THEN** tracker intake and worker spawns resume for that project

### Requirement: Orchestrators and privileged spawns are exempt from pause

Pause SHALL gate only ordinary worker intake and spawns. Orchestrator (and
equivalently privileged prime-tier) sessions SHALL remain alive and continue to
be spawnable while a scope is paused, so alerting and supervision keep running. A
forced spawn SHALL override the pause guard.

#### Scenario: Orchestrator stays alive under pause

- **WHEN** a scope is paused in any mode (soft, per-project hard, or fleet hard)
- **THEN** orchestrator sessions are not terminated by the pause and continue running

#### Scenario: Forced spawn overrides the guard

- **WHEN** a spawn is requested with the force flag for a gated project
- **THEN** the spawn is allowed despite the pause

### Requirement: Pause state is observable as running, draining, or paused

The daemon SHALL expose a derived pause state for each project computed from the
persisted flags and the count of live workers: `running` when neither the project
nor the fleet flag is set; `draining` (with a live-worker count) when the project
is gated and one or more workers are still finishing; and `paused` when the
project is gated and no live workers remain. This derived state SHALL NOT be
persisted; it SHALL be computed at read time. The project/workspace summary
surfaced to clients SHALL carry the project's pause flag, its derived pause state,
and its draining-worker count.

#### Scenario: Draining state reports the live count

- **WHEN** a gated project still has two live workers finishing
- **THEN** its derived pause state is `draining` and its draining-worker count is 2

#### Scenario: Paused state once drained

- **WHEN** a gated project has no remaining live workers
- **THEN** its derived pause state is `paused` and its draining-worker count is 0

#### Scenario: Running state when unpaused

- **WHEN** a project's flag is clear and the fleet flag is clear
- **THEN** its derived pause state is `running`

### Requirement: Pause is controllable through the API and CLI

The daemon SHALL expose HTTP routes to read fleet pause status and to set/clear
fleet and per-project pause, accepting a hard-mode parameter on pause. The CLI
SHALL provide `pause` and `resume` commands scoped to a project argument or to the
whole fleet, with a hard-mode flag on pause, and SHALL surface pause state in
status output.

#### Scenario: CLI pauses the fleet

- **WHEN** the operator runs the fleet-scoped pause command
- **THEN** the fleet flag is set and the command reports the fleet as paused

#### Scenario: CLI pauses a single project hard

- **WHEN** the operator runs the project-scoped pause command with the hard flag
- **THEN** that project is paused, its live workers are terminated immediately, and the command reports the resulting pause state

#### Scenario: Status surfaces pause state

- **WHEN** a scope is paused and the operator queries status
- **THEN** the reported status reflects the paused (or draining) state
