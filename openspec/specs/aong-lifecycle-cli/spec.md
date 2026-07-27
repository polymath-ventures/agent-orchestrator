# aong-lifecycle-cli Specification

## Purpose

TBD - created by archiving change add-aong-lifecycle-cli. Update Purpose after archive.

## Requirements

### Requirement: aong is a porcelain over ao's public CLI

`aong` SHALL be a separate binary that drives AO operations through the public
`ao` executable by default and, for the service-unit operations `ao` has no
commands for — starting units and reporting unit state — `systemctl --user`.
For any top-level verb that `aong` does not explicitly override, `aong` SHALL
forward the invocation to `ao` by preserving argv, stdin, stdout, stderr, and the
underlying exit code.

`aong` SHALL NOT import AO daemon or `ao` CLI internal packages, SHALL NOT open
the run file, the shutdown token, the daemon HTTP API, or AO storage directly,
and SHALL NOT re-implement behavior `ao` already provides.

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
- **THEN** the command fails with an error naming the sibling directory and `PATH` as the locations searched, and performs no lifecycle action or passthrough

#### Scenario: Failure from ao is surfaced verbatim

- **WHEN** an invoked `ao` command exits non-zero
- **THEN** `aong` fails, reports the `ao` command it ran and that command's output for overridden wrapping commands, and preserves the `ao` command's stdout, stderr, and exit code for passthrough commands

#### Scenario: Non-overridden ao commands pass through

- **WHEN** `aong` is invoked with a top-level verb that `ao` supports and `aong` does not explicitly override
- **THEN** `aong` invokes `ao` with the original verb and arguments

#### Scenario: New ao commands are reachable without an aong registry update

- **WHEN** `ao` gains a new top-level command and `aong` has no explicit override for that command
- **THEN** `aong <new-command>` invokes `ao <new-command>` instead of failing as misuse

#### Scenario: Passthrough stdio and exit code are transparent

- **WHEN** a passthrough `ao` command reads stdin, writes stdout or stderr, or exits non-zero
- **THEN** `aong` connects the same stdin, relays stdout and stderr without merging them, and exits with `ao`'s exit code

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
- `aong pause <project>` SHALL run `ao pause <project>`, preserving AO's
  project-scoped pause.
- `aong pause <project> --hard` SHALL run `ao pause <project> --hard`,
  preserving AO's project-scoped hard pause.
- `aong resume <project>` SHALL run `ao resume <project>`, preserving AO's
  project-scoped resume.

`aong drain` and `aong stop-work` SHALL state in their output that the fleet
remains gated until `aong resume` is run. `aong pause` SHALL be a deliberate
instructional divergence: it SHALL NOT alias `drain`, and it SHALL report that
the available work-control verbs are `aong drain` for gate-and-drain-at-idle and
`aong stop-work` for immediate termination.

#### Scenario: drain composes the soft fleet pause

- **WHEN** `aong drain` runs
- **THEN** `ao pause --all` is invoked without the hard flag

#### Scenario: stop-work composes the hard fleet pause

- **WHEN** `aong stop-work` runs
- **THEN** `ao pause --all --hard` is invoked

#### Scenario: resume composes the fleet resume

- **WHEN** `aong resume` runs
- **THEN** `ao resume --all` is invoked

#### Scenario: project pause is preserved

- **WHEN** `aong pause my-project` runs
- **THEN** `ao pause my-project` is invoked

#### Scenario: project hard pause is preserved

- **WHEN** `aong pause my-project --hard` runs
- **THEN** `ao pause my-project --hard` is invoked

#### Scenario: project resume is preserved

- **WHEN** `aong resume my-project` runs
- **THEN** `ao resume my-project` is invoked

#### Scenario: Gating verbs name the way back

- **WHEN** `aong drain` or `aong stop-work` completes successfully
- **THEN** the output states that the fleet stays gated until `aong resume` is run

#### Scenario: pause redirects rather than aliasing

- **WHEN** `aong pause` runs
- **THEN** no `ao pause` command is invoked, the output points at `aong drain` and `aong stop-work`, and the process exits as command-line misuse

#### Scenario: fleet pause flags redirect rather than aliasing

- **WHEN** `aong pause --all` or `aong pause --all --hard` runs
- **THEN** no `ao pause` command is invoked, the output points at `aong drain` and `aong stop-work`, and the process exits as command-line misuse

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
state proves there is nothing to gate. Any state that leaves a live daemon
possible — including a daemon whose health or readiness probe is currently
failing, an ambiguous state, and any state `aong` does not recognise — SHALL be
treated as work-bearing, so stop-work is attempted and a failure aborts the
shutdown. There SHALL be no state for which a failed stop-work still stops the
daemon, because `aong shutdown` must never exit successfully while a daemon it
could not reach may still be running.

Because refusing must not be a dead end, the failure SHALL name the verb that
reconciles a daemon that is already gone.

#### Scenario: Work is stopped before the daemon

- **WHEN** `aong shutdown` runs against a ready daemon
- **THEN** `ao pause --all --hard` is invoked before `ao stop`

#### Scenario: Failed stop-work aborts the shutdown

- **WHEN** `aong shutdown` runs and the stop-work step fails
- **THEN** the daemon is not stopped and the command fails reporting the stop-work failure

#### Scenario: A daemon proven absent skips stop-work

- **WHEN** `aong shutdown` runs and the reported daemon state proves no live daemon exists
- **THEN** no fleet pause command is invoked and `ao stop` is invoked

#### Scenario: An unhealthy, not-ready, or stale daemon still has its work stopped

- **WHEN** `aong shutdown` runs and the daemon is reported as unhealthy, not ready, or stale
- **THEN** `ao pause --all --hard` is invoked before `ao stop`

#### Scenario: An unrecognised daemon state fails closed

- **WHEN** `aong shutdown` runs and the reported daemon state is empty or not one `aong` recognises
- **THEN** `ao pause --all --hard` is invoked before `ao stop`

#### Scenario: An ambiguous state does not license stopping the daemon anyway

- **WHEN** `aong shutdown` runs against an ambiguous daemon state and the stop-work attempt fails
- **THEN** `ao stop` is not invoked and the command fails

#### Scenario: The failure names the way forward

- **WHEN** `aong shutdown` fails because work could not be stopped
- **THEN** the error reports the underlying failure and names the verb that stops a daemon without stopping work

### Requirement: aong follows AO's CLI exit-code convention

`aong` SHALL exit 2 for command-line misuse, 1 for runtime failures in `aong`'s
own override/composition layer, and 0 on success, matching the convention `ao`
is scripted against. Passthrough commands SHALL preserve the underlying `ao`
exit code exactly.

#### Scenario: Misuse exits 2

- **WHEN** `aong` is invoked with an unknown flag before the verb, an unexpected argument to an overridden verb, or help for a command that does not exist
- **THEN** the process exits with code 2

#### Scenario: Help and version still succeed

- **WHEN** `aong` is invoked with no arguments, `--help`, `--version`, `help`, or `help <overridden verb>`
- **THEN** the process prints the corresponding text, exits 0, and invokes no lifecycle command

#### Scenario: Runtime failure exits 1

- **WHEN** an overridden `aong` command's composed `ao` or `systemctl` command fails
- **THEN** the process exits with code 1

#### Scenario: Passthrough preserves ao exit code

- **WHEN** `aong` is invoked with a non-overridden verb and the underlying `ao` command exits with any code
- **THEN** `aong` exits with the same code instead of remapping it to 1 or 2

### Requirement: aong explains delegated and divergent execution

`aong` SHALL provide a global `--verbose` flag that is parsed only before the
verb. When enabled, `aong` SHALL print the exact underlying `ao` invocation for
passthrough commands and overridden verbs that wrap `ao`, and SHALL plainly say
when an overridden verb diverges from `ao` instead of wrapping an equivalent
command.

#### Scenario: Passthrough verbose prints the ao invocation

- **WHEN** `aong --verbose project list` runs through the passthrough path
- **THEN** the output includes the exact `ao project list` invocation before the passthrough command runs

#### Scenario: Override verbose prints composed ao invocation

- **WHEN** `aong --verbose drain` runs
- **THEN** the output includes the exact `ao pause --all` invocation before the override runs

#### Scenario: Divergence verbose names the divergence

- **WHEN** `aong --verbose pause` runs
- **THEN** the output says that `pause` intentionally diverges and no equivalent `ao pause` invocation is being run

#### Scenario: Flags after passthrough verb are preserved

- **WHEN** `aong project --verbose` runs and `project` is not parsed as an `aong` override flag position
- **THEN** `aong` invokes `ao project --verbose`

### Requirement: aong help describes the complete surface

`aong` help SHALL list the explicitly overridden verbs and SHALL state that every
other `ao` command is available through `aong` by passthrough. Help for a
passthrough verb SHALL forward to `ao <verb> --help`; help for an overridden
verb SHALL show `aong`'s own help for that verb.

#### Scenario: Root help names overrides and passthrough

- **WHEN** `aong --help` runs
- **THEN** the help lists the overridden verbs and states that non-overridden `ao` commands pass through

#### Scenario: Passthrough verb help delegates to ao

- **WHEN** `aong project --help` runs for a passthrough command
- **THEN** `aong` invokes `ao project --help`

#### Scenario: Override verb help stays local

- **WHEN** `aong drain --help` or `aong doctor --help` runs
- **THEN** `aong` prints its own override help and does not invoke `ao <verb> --help`

### Requirement: aong doctor includes fork service health

`aong doctor` SHALL run `ao doctor` and SHALL additionally check the fork-owned
user service units `ao-web.service` and `ao-tmux.service` when they are loaded
in the user service manager. The added checks SHALL report each loaded unit's
active state. A loaded unit that is not active SHALL make `aong doctor` fail
after preserving `ao doctor` output. Missing units on hosts where they are not
loaded SHALL be reported as not present and SHALL NOT fail the command. Plain
text `aong doctor` SHALL still run the fork service checks when `ao doctor`
fails, so the fork-owned service state is visible in the same diagnostic run.

#### Scenario: doctor preserves ao doctor output

- **WHEN** `aong doctor` runs
- **THEN** `ao doctor` is invoked and its stdout and stderr are preserved before any `aong` unit-health summary

#### Scenario: doctor json includes fork unit checks

- **WHEN** `aong doctor --json` or `aong doctor --json=true` runs
- **THEN** `aong` invokes `ao doctor --json`, preserves the doctor report JSON shape, and includes fork service checks in that report

#### Scenario: doctor text keeps fork checks when ao doctor fails

- **WHEN** `aong doctor` runs and `ao doctor` fails before fork service checks
- **THEN** `aong` preserves `ao doctor` output, still reports fork service health, and exits with a failure

#### Scenario: Loaded healthy fork units pass

- **WHEN** `ao-web.service` and `ao-tmux.service` are loaded and active
- **THEN** `aong doctor` reports both units as active and exits successfully if `ao doctor` also succeeds

#### Scenario: Loaded unhealthy fork unit fails

- **WHEN** a fork service unit is loaded but inactive or failed
- **THEN** `aong doctor` reports that unit state and exits with a runtime failure

#### Scenario: Missing fork units do not fail plain hosts

- **WHEN** the user service manager has no loaded `ao-web.service` or `ao-tmux.service`
- **THEN** `aong doctor` reports that no fork service units were found and does not fail for that reason
