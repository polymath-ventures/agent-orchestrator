## ADDED Requirements

### Requirement: Doctor warns when a session is stuck in the active state

`ao doctor` SHALL report a check that warns when a session the daemon records as
**active** has been in that state, with no transition to any other state, for longer than a fixed threshold.
The warning SHALL name each such session, how long it has been in that state, and a
command the operator can run next, so it is actionable rather than merely
alarming.

Only the active state can be wedged, so only it SHALL warn. A session recorded
as idle has stopped working; sessions recorded as waiting on input or blocked
are paused on the user and are routinely left that way for long periods. Warning
on those would produce recurring noise an operator learns to ignore, which would
defeat the check. Terminated sessions SHALL NOT warn at any age.

#### Scenario: A session active past the threshold warns

- **WHEN** a session the daemon records as active entered that state longer ago than the threshold, with no transition since
- **THEN** the check warns, naming that session, how long it has been active, and a command to inspect it

#### Scenario: A recently active session does not warn

- **WHEN** every active session entered that state within the threshold
- **THEN** the check passes and names no session

#### Scenario: A session that is not active never warns

- **WHEN** a session recorded as idle, waiting on input, blocked, or exited has been in that state far past the threshold
- **THEN** the check does not warn about it

#### Scenario: A terminated session never warns

- **WHEN** a session is terminated and its last recorded activity is older than the threshold
- **THEN** the check does not warn about it

#### Scenario: Every stuck session is reported

- **WHEN** more than one active session is past the threshold
- **THEN** every one of them is named in the warning

### Requirement: The signal comes from AO's own activity records

The check SHALL derive the duration from the activity record the daemon already
keeps for each session, read over the existing loopback session listing. It SHALL NOT
inspect processes: no process-tree walk, no `ps`, no `tmux`, and no other
external process-inspection tool. It SHALL NOT introduce any platform-specific
tool invocation, so it behaves identically on macOS and Linux by construction.

#### Scenario: No process inspection is performed

- **WHEN** the check runs
- **THEN** it invokes no external command and inspects no process tree

#### Scenario: A session with no recorded activity at all is not guessed about

- **WHEN** an active session has no recorded activity timestamp
- **THEN** the check does not warn about it, because a duration cannot be measured without a starting point

### Requirement: The check is read-only and cannot fail doctor

The check SHALL be read-only: it SHALL issue only reads against the daemon and
SHALL NOT contact `supervise.sock`, whose traffic perturbs daemon lifecycle. It
SHALL NOT mutate any session or daemon state.

When the daemon cannot be reached, or the session listing cannot be read, the
check SHALL report the signal as unavailable and SHALL NOT fail. A missing
signal is not evidence of an unhealthy machine.

#### Scenario: An unreachable daemon degrades to unavailable

- **WHEN** the check runs and the daemon is not reachable
- **THEN** the check reports that the signal is unavailable and does not fail

#### Scenario: A slow daemon does not stall the report

- **WHEN** the check runs and the daemon accepts the request but does not answer
- **THEN** the check gives up within the probe bound doctor uses for its other daemon reads, reports the signal as unavailable, and lets the rest of the report proceed

#### Scenario: The supervisor socket is never contacted

- **WHEN** the check runs
- **THEN** no request is made to the supervisor socket

#### Scenario: No session or daemon state is changed

- **WHEN** the check runs
- **THEN** only read requests are issued and no session or daemon state is modified
