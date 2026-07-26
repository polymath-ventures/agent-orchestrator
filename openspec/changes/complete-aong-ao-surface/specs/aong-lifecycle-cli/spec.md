## MODIFIED Requirements

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

#### Scenario: Gating verbs name the way back

- **WHEN** `aong drain` or `aong stop-work` completes successfully
- **THEN** the output states that the fleet stays gated until `aong resume` is run

#### Scenario: pause redirects rather than aliasing

- **WHEN** `aong pause` runs
- **THEN** no `ao pause` command is invoked, the output points at `aong drain` and `aong stop-work`, and the process exits as command-line misuse

### Requirement: aong follows AO's CLI exit-code convention

`aong` SHALL exit 2 for command-line misuse, 1 for runtime failures in `aong`'s
own override/composition layer, and 0 on success, matching the convention `ao`
is scripted against. Passthrough commands SHALL preserve the underlying `ao`
exit code exactly.

#### Scenario: Misuse exits 2

- **WHEN** `aong` is invoked with an unknown flag before the verb, an unexpected argument to an overridden verb, or help for an overridden verb that does not exist
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

## ADDED Requirements

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

- **WHEN** `aong doctor --verbose` runs and `doctor` is not parsed as an `aong` override flag position
- **THEN** `aong` invokes `ao doctor --verbose`

### Requirement: aong help describes the complete surface

`aong` help SHALL list the explicitly overridden verbs and SHALL state that every
other `ao` command is available through `aong` by passthrough. Help for a
passthrough verb SHALL forward to `ao <verb> --help`; help for an overridden
verb SHALL show `aong`'s own help for that verb.

#### Scenario: Root help names overrides and passthrough

- **WHEN** `aong --help` runs
- **THEN** the help lists the overridden verbs and states that non-overridden `ao` commands pass through

#### Scenario: Passthrough verb help delegates to ao

- **WHEN** `aong doctor --help` runs for a passthrough command
- **THEN** `aong` invokes `ao doctor --help`

#### Scenario: Override verb help stays local

- **WHEN** `aong drain --help` runs
- **THEN** `aong` prints its own `drain` help and does not invoke `ao drain --help`

### Requirement: aong doctor includes fork service health

`aong doctor` SHALL run `ao doctor` and SHALL additionally check the fork-owned
user service units `ao-web.service` and `ao-tmux.service` when they are loaded
in the user service manager. The added checks SHALL report each loaded unit's
active state. A loaded unit that is not active SHALL make `aong doctor` fail
after preserving `ao doctor` output. Missing units on hosts where they are not
loaded SHALL be reported as not present and SHALL NOT fail the command.

#### Scenario: doctor preserves ao doctor output

- **WHEN** `aong doctor` runs
- **THEN** `ao doctor` is invoked and its stdout and stderr are preserved before any `aong` unit-health summary

#### Scenario: doctor json includes fork unit checks

- **WHEN** `aong doctor --json` runs
- **THEN** `aong` invokes `ao doctor --json`, preserves the doctor report JSON shape, and includes fork service checks in that report

#### Scenario: Loaded healthy fork units pass

- **WHEN** `ao-web.service` and `ao-tmux.service` are loaded and active
- **THEN** `aong doctor` reports both units as active and exits successfully if `ao doctor` also succeeds

#### Scenario: Loaded unhealthy fork unit fails

- **WHEN** a fork service unit is loaded but inactive or failed
- **THEN** `aong doctor` reports that unit state and exits with a runtime failure

#### Scenario: Missing fork units do not fail plain hosts

- **WHEN** the user service manager has no loaded `ao-web.service` or `ao-tmux.service`
- **THEN** `aong doctor` reports that no fork service units were found and does not fail for that reason
