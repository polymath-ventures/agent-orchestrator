## ADDED Requirements

### Requirement: Doctor warns when a live session has gone silent

`ao doctor` SHALL report a check that warns when a live, non-terminated session
has recorded no activity for longer than a fixed silence threshold. The warning
SHALL name each silent session and how long it has been silent, so the operator
can act without further inspection.

Sessions within the threshold, and terminated sessions at any age, SHALL NOT
warn. A session whose activity keeps being recorded — an agent running a
long-lived background server, for example — is by definition never silent and
therefore never warns.

#### Scenario: A session silent past the threshold warns

- **WHEN** a live session's last recorded activity is older than the silence threshold
- **THEN** the check warns, naming that session and how long it has been silent

#### Scenario: A recently active session does not warn

- **WHEN** every live session has recorded activity within the silence threshold
- **THEN** the check passes and names no session

#### Scenario: A terminated session never warns

- **WHEN** a session is terminated and its last recorded activity is older than the threshold
- **THEN** the check does not warn about it

#### Scenario: Several silent sessions are all reported

- **WHEN** more than one live session is silent past the threshold
- **THEN** every one of them is named in the warning

### Requirement: The signal comes from AO's own activity records

The check SHALL derive silence from the activity the daemon already records for
each session, read over the existing loopback session listing. It SHALL NOT
inspect processes: no process-tree walk, no `ps`, no `tmux`, and no other
external process-inspection tool. It SHALL NOT introduce any platform-specific
tool invocation, so it behaves identically on macOS and Linux by construction.

#### Scenario: No process inspection is performed

- **WHEN** the check runs
- **THEN** it invokes no external command and inspects no process tree

#### Scenario: A session with no recorded activity at all is not guessed about

- **WHEN** a live session has no recorded activity timestamp
- **THEN** the check does not warn about it, because silence cannot be measured without a starting point

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

#### Scenario: The supervisor socket is never contacted

- **WHEN** the check runs
- **THEN** no request is made to the supervisor socket

#### Scenario: No session or daemon state is changed

- **WHEN** the check runs
- **THEN** only read requests are issued and no session or daemon state is modified
