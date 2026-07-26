## ADDED Requirements

### Requirement: aong is a porcelain over ao's public CLI

`aong` SHALL be a separate binary that drives AO lifecycle exclusively through
the public `ao` executable and, for service units `ao` does not manage,
`systemctl --user`. It SHALL NOT import AO daemon or `ao` CLI internal packages,
SHALL NOT open the run file, the shutdown token, the daemon HTTP API, or AO
storage directly, and SHALL NOT re-implement behavior `ao` already provides.

`aong` SHALL locate the `ao` executable next to its own binary first and fall
back to `PATH`, so a co-installed pair is never split by a stale `ao` earlier on
`PATH`. A sibling SHALL only be preferred when it is an executable file, so a
file that merely shares the name cannot displace a working `ao` on `PATH`. When
no `ao` executable can be found, `aong` SHALL fail with a message naming both
locations it searched.

#### Scenario: Sibling ao is preferred over PATH

- **WHEN** an `ao` executable exists in the same directory as the running `aong` binary and a different `ao` also exists on `PATH`
- **THEN** `aong` invokes the sibling executable

#### Scenario: A non-executable sibling does not displace PATH

- **WHEN** a file named `ao` exists beside the `aong` binary but is not executable, and an executable `ao` is on `PATH`
- **THEN** `aong` invokes the `PATH` executable

#### Scenario: Falls back to PATH when no sibling exists

- **WHEN** no `ao` executable exists beside the `aong` binary but one is on `PATH`
- **THEN** `aong` invokes the `PATH` executable

#### Scenario: Missing ao is reported, not worked around

- **WHEN** no `ao` executable exists beside the `aong` binary or on `PATH`
- **THEN** the command fails with an error naming the sibling directory and `PATH` as the locations searched, and performs no lifecycle action

#### Scenario: Failure from ao is surfaced verbatim

- **WHEN** an invoked `ao` command exits non-zero
- **THEN** `aong` fails, reports the `ao` command it ran and that command's output, and does not retry or substitute its own implementation

### Requirement: aong detects its runtime environment

`aong` SHALL determine, at run time, whether AO is managed by systemd user
services or is a plain local daemon, rather than assuming any particular
deployment layout. The environment SHALL be classified as `systemd` when
`systemctl` is available AND at least one AO user unit is loaded in the user
manager; otherwise it SHALL be classified as `plain`. Commands whose behavior
depends on the environment SHALL report which environment was detected.

A failed probe SHALL NOT be reported as `plain`. Only an answer from the service
manager that a unit is absent counts as absent; a probe that could not be
answered SHALL be surfaced as a probe failure, so "there is nothing to manage
here" is never confused with "I could not ask".

#### Scenario: systemd environment detected from loaded units

- **WHEN** `systemctl` is available and at least one AO user unit is loaded
- **THEN** the detected environment is `systemd`

#### Scenario: No systemctl means plain environment

- **WHEN** `systemctl` is not available on the host
- **THEN** the detected environment is `plain` and no `systemctl` invocation is attempted

#### Scenario: systemctl present but no AO units means plain environment

- **WHEN** `systemctl` is available but the service manager reports every AO user unit as not-found
- **THEN** the detected environment is `plain`

#### Scenario: A failed probe is reported, not classified as plain

- **WHEN** `systemctl` is available but the unit probe fails to return an answer
- **THEN** the environment is not reported as `plain`, and the probe failure is surfaced

### Requirement: aong start starts the local AO services

`aong start` SHALL start AO's local services in a `systemd` environment,
starting only the AO user units that are actually loaded, and SHALL report each
unit it started. In a `plain` environment `aong start` SHALL NOT invent a
process-supervision path of its own: it SHALL fail with a message stating that
no AO service units were found and naming how a daemon is started on that
environment.

#### Scenario: Loaded units are started

- **WHEN** `aong start` runs in a `systemd` environment where AO user units are loaded
- **THEN** those units are started via `systemctl --user start` and each started unit is reported

#### Scenario: Unloaded units are skipped, not failed on

- **WHEN** `aong start` runs in a `systemd` environment where only some AO user units are loaded
- **THEN** the loaded units are started and the absent units are neither started nor treated as an error

#### Scenario: Plain environment reports instead of pretending

- **WHEN** `aong start` runs in a `plain` environment
- **THEN** the command fails, states that no AO service units were found, and names the command that starts a daemon on that environment

### Requirement: aong status reports the daemon, its services, and fleet state

`aong status` SHALL report daemon state and fleet pause state by delegating to
`ao status`, and SHALL additionally report the active state of each loaded AO
user unit when the environment is `systemd`. It SHALL NOT duplicate any status
fact `ao status` already reports. `aong status` SHALL be read-only.

#### Scenario: Daemon and fleet state come from ao

- **WHEN** `aong status` runs
- **THEN** the output includes `ao status`'s report of daemon state and fleet pause state

#### Scenario: Unit states are added under systemd

- **WHEN** `aong status` runs in a `systemd` environment with loaded AO user units
- **THEN** the output additionally names each loaded unit and its active state

#### Scenario: No unit section without systemd

- **WHEN** `aong status` runs in a `plain` environment
- **THEN** the output reports the detected environment and contains no service-unit section

#### Scenario: A failed unit probe is reported without failing the command

- **WHEN** `aong status` runs and the service-manager probe fails
- **THEN** the daemon state is still reported, the environment is reported as unknown with the probe failure, and the command does not fail

#### Scenario: Status still reports when the daemon is down

- **WHEN** `aong status` runs and the daemon is not running
- **THEN** the command reports the stopped daemon state and any unit states, and does not fail

### Requirement: Fleet work verbs are named for what they do

`aong` SHALL expose the fleet work-control verbs under names that describe their
actual effect:

- `aong drain` SHALL run `ao pause --all`: gate new intake and spawns, let live
  workers finish their current work, and terminate each worker as it reaches
  idle.
- `aong stop-work` SHALL run `ao pause --all --hard`: terminate all live work
  immediately, including orchestrator and Prime sessions.
- `aong resume` SHALL run `ao resume --all`, restoring normal intake and spawns.

`aong drain` and `aong stop-work` SHALL state in their output that the fleet
remains gated until `aong resume` is run. `aong` SHALL NOT expose a `pause` verb
distinct from `drain`, because no non-draining pause capability exists to
compose.

#### Scenario: drain composes the soft fleet pause

- **WHEN** `aong drain` runs
- **THEN** `ao pause --all` is invoked without the hard flag

#### Scenario: stop-work composes the hard fleet pause

- **WHEN** `aong stop-work` runs
- **THEN** `ao pause --all --hard` is invoked

#### Scenario: resume composes the fleet resume

- **WHEN** `aong resume` runs
- **THEN** `ao resume --all` is invoked

#### Scenario: Gating verbs name the way back

- **WHEN** `aong drain` or `aong stop-work` completes successfully
- **THEN** the output states that the fleet stays gated until `aong resume` is run

### Requirement: aong stop stops the daemon and says what survives

`aong stop` SHALL run `ao stop` and SHALL state plainly in its output that agent
sessions keep running after the daemon stops. It SHALL NOT terminate agent
sessions.

#### Scenario: Stop delegates to ao stop

- **WHEN** `aong stop` runs
- **THEN** `ao stop` is invoked and no session-termination command is invoked

#### Scenario: Stop discloses that work survives

- **WHEN** `aong stop` completes successfully
- **THEN** the output states that agent sessions keep running and names the verb that also stops work

### Requirement: aong shutdown stops work and then the daemon

`aong shutdown` SHALL be the single verb that stops everything: it SHALL first
perform `stop-work`, then `stop`. If stopping work fails, `aong shutdown` SHALL
NOT proceed to stop the daemon, so the operator is never left with live work and
no supervisor.

`aong shutdown` SHALL skip the stop-work step ONLY when the reported daemon
state proves there is no live daemon to gate. Any state that leaves a live
daemon possible — including a daemon whose health or readiness probe is
currently failing — SHALL be treated as work-bearing, so stop-work is attempted
and a failure aborts the shutdown.

#### Scenario: Work is stopped before the daemon

- **WHEN** `aong shutdown` runs against a ready daemon
- **THEN** `ao pause --all --hard` is invoked before `ao stop`

#### Scenario: Failed stop-work aborts the shutdown

- **WHEN** `aong shutdown` runs and the stop-work step fails
- **THEN** the daemon is not stopped and the command fails reporting the stop-work failure

#### Scenario: A daemon proven absent skips stop-work

- **WHEN** `aong shutdown` runs and the reported daemon state proves no live daemon exists
- **THEN** no fleet pause command is invoked and `ao stop` is invoked

#### Scenario: An unhealthy or not-ready daemon still has its work stopped

- **WHEN** `aong shutdown` runs and the daemon is reported as unhealthy or not ready
- **THEN** `ao pause --all --hard` is invoked before `ao stop`

### Requirement: aong follows AO's CLI exit-code convention

`aong` SHALL exit 2 for command-line misuse, 1 for runtime failures, and 0 on
success, matching the convention `ao` is scripted against.

#### Scenario: Misuse exits 2

- **WHEN** `aong` is invoked with an unknown flag or an unexpected argument
- **THEN** the process exits with code 2

#### Scenario: Runtime failure exits 1

- **WHEN** an invoked `ao` or `systemctl` command fails
- **THEN** the process exits with code 1
