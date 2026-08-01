## MODIFIED Requirements

### Requirement: A session's name is delivered to its harness

The system SHALL deliver a session's display name into the harness the session
runs, so that the harness's own session list shows the same name AO shows. The
delivered name SHALL be byte-identical to the name AO persists and displays.

Delivery SHALL be expressed as an optional agent-adapter capability with two
forms:

- An **in-harness rename command**, which the system SHALL use for every
  supported spawn after harness readiness and for any name change applied to an
  already-running session.
- An **optional launch argument**, which the system MAY use at spawn time as an
  accelerator when the harness offers one, so early harness surfaces can show
  the name before the in-harness rename is accepted.

A launch argument SHALL NOT suppress the in-harness rename path for that spawn.
A harness that offers neither form SHALL simply keep AO's own display name, and
the system SHALL NOT attempt to name it by writing blindly into its input.

#### Scenario: Harness with a launch argument is named at start and after readiness

- **WHEN** a session spawns on a harness that offers a launch naming argument and
  an in-harness rename
- **THEN** the launch command carries the computed name
- **AND** the post-readiness rename command is also issued for that spawn
- **AND** both delivery forms carry the same computed name

#### Scenario: Harness without a launch argument is named after start

- **WHEN** a session spawns on a harness that offers only an in-harness rename
- **THEN** the rename command is issued after the harness is ready to accept it
- **AND** the command carries the computed name

#### Scenario: Harness with no naming capability is left alone

- **WHEN** a session spawns on a harness that offers neither naming form
- **THEN** AO persists and displays its own computed name
- **AND** no naming input is written into that session's harness

#### Scenario: Delivered name matches the displayed name

- **WHEN** a session has been named on any harness
- **THEN** the name delivered to the harness is byte-identical to the display
  name AO persists

### Requirement: A rename reaches the running session

A rename of a live session SHALL update both the persisted display name and the
name inside the running harness, through the same delivery path spawn uses, so
that no surface can be updated without the others.

A rename targeting a session that is terminated, or that has no running runtime,
SHALL update persisted state without attempting harness delivery.

Because a name is cosmetic relative to the session's task, an operator rename or
restore redelivery SHALL be delivered only while the session is idle, re-checked
at the moment of the write, and SHALL be skipped when the session is mid-turn,
blocked, or sitting at a prompt that would treat the write as the next user
message. A spawn's own naming write MAY proceed while the session is mid-turn,
because a session whose prompt was delivered at launch is routinely working by
the time its harness reports ready. It MAY also proceed while the session is
back at its idle prompt only when the harness can report pending decisions as a
distinct blocked state; otherwise a waiting-input state is ambiguous and SHALL
be skipped. It SHALL always be skipped while the session is blocked or awaiting
a pending decision, where the keystrokes that deliver a name would answer that
decision.

A runtime may keep a session's terminal alive after its agent exits, so a naming
write can reach whatever replaced the agent. The system SHALL constrain a
session name to characters that cannot cause a command to run: no control
characters and none of the shell grammar that starts, substitutes, quotes,
escapes, or redirects a command. That way, a misdirected naming write cannot be
executed. It SHALL reject rather than truncate a supplied name that falls
outside that set, on every path that accepts one, so no path can persist a name
that delivery would refuse. The system SHALL additionally require positive
confirmation that a session's runtime is still running its agent before writing
a name into it, and SHALL skip the write when it cannot confirm this.

#### Scenario: A rename is not written into a busy session

- **WHEN** a rename targets a session that is mid-turn, blocked, or sitting at a
  prompt that would accept the write as user input
- **THEN** the persisted display name is updated
- **AND** no naming input is written to the runtime

#### Scenario: A spawn names a session that is already working

- **WHEN** a session's harness begins its task before it reports readiness
- **THEN** the spawn's naming write is still delivered

#### Scenario: A spawn names a distinguishable idle prompt

- **WHEN** a session's harness can report pending decisions as blocked
- **AND** the session reaches its idle prompt before the spawn naming write
  would be issued
- **THEN** the spawn's naming write is still delivered

#### Scenario: A spawn skips an ambiguous waiting-input state

- **WHEN** a session's harness cannot report pending decisions as blocked
- **AND** the session is in a waiting-input state when its spawn naming write
  would be issued
- **THEN** no naming input is written to that runtime

#### Scenario: A spawn does not name a session awaiting a pending decision

- **WHEN** a session is blocked or awaiting a pending decision when its naming
  write would be issued
- **THEN** no naming input is written to that runtime

#### Scenario: A name is not written into a terminal the agent has left

- **WHEN** a session's runtime is no longer running its agent
- **THEN** no naming input is written to that runtime

#### Scenario: A restored session keeps the name AO owns

- **WHEN** a session that was renamed is torn down and later restored
- **THEN** the restored harness carries the persisted display name

#### Scenario: A name cannot carry executable syntax

- **WHEN** a work item title contains shell syntax
- **THEN** the computed name contains none of it
- **AND** a supplied name containing such syntax is rejected rather than altered

Harness delivery SHALL NOT decide whether the rename succeeded: the persisted
name is the rename's outcome, and a failed delivery SHALL be recorded rather
than reported as a failed rename.

#### Scenario: Sidebar rename updates the harness

- **WHEN** an operator renames a live session
- **THEN** the persisted display name is updated
- **AND** the new name is delivered to the running harness

#### Scenario: Renaming a terminated session skips delivery

- **WHEN** a rename targets a terminated session
- **THEN** the persisted display name is updated
- **AND** no naming input is written to any runtime

#### Scenario: A failed harness write still renames the session

- **WHEN** a rename persists but the harness write fails
- **THEN** the rename succeeds
- **AND** the failure is recorded
