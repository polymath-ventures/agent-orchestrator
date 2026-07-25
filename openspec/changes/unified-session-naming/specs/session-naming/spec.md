## ADDED Requirements

### Requirement: The daemon owns a session's display name

The daemon SHALL be the single authority for a session's display name. It SHALL
compute the name at the moment the session's identity is established — on spawn,
on rebind to a different work item, and on explicit rename — and SHALL persist
exactly one name per session.

An omitted display name on a spawn request SHALL be the signal that asks the
daemon to compute the name, and SHALL NOT result in an unnamed session. A
display name supplied explicitly on a spawn request SHALL be honored as a
deliberate override.

The daemon SHALL NOT accept a name derived from the spawn prompt, because a
prompt-derived name is indistinguishable from an operator-supplied one and would
silently outrank the computed name.

#### Scenario: Spawn without a name gets the computed name

- **WHEN** a worker is spawned for a work item with no display name supplied
- **THEN** the session's persisted display name is the daemon-computed name for
  that role and work item
- **AND** the name is not empty

#### Scenario: Explicit name overrides computation

- **WHEN** a session is spawned with an explicit display name
- **THEN** that name is persisted as the session's display name
- **AND** the daemon does not overwrite it with a computed name

#### Scenario: A prompt is never a name

- **WHEN** a worker is spawned with a prompt and no display name
- **THEN** the persisted display name is the computed name
- **AND** the display name does not contain the prompt text

### Requirement: Session names follow a role-based grammar

The daemon SHALL compute display names from the session's role and work item:

- A worker SHALL be named from the project's configured session prefix, the work
  item number, and a slug of the work item title.
- A project orchestrator SHALL be named from the project's configured session
  prefix and a role suffix identifying it as the orchestrator.
- Prime SHALL be named from fleet Prime settings, which already own its display
  name.

A worker whose work item cannot be resolved SHALL still receive a non-empty name
built from the parts that are available. Names SHALL be capped at the system's
existing display-name length limit, and truncation SHALL preserve the leading
identifying portion (prefix, work item number, role suffix) and clip the trailing
title slug.

#### Scenario: Worker named from prefix, item, and title

- **WHEN** a worker is spawned for a work item whose title is known
- **THEN** the display name begins with the project's session prefix followed by
  the work item number
- **AND** the remainder is a slug derived from the work item title

#### Scenario: Orchestrator named from the project prefix

- **WHEN** a project orchestrator session is spawned
- **THEN** its display name is the project's session prefix followed by the
  orchestrator role suffix

#### Scenario: Missing work item title degrades, never fails

- **WHEN** a worker is spawned and the work item title cannot be resolved
- **THEN** the spawn succeeds
- **AND** the display name still contains the session prefix and the work item
  number
- **AND** the reason the title was unavailable is logged

#### Scenario: Over-long name keeps its identifying head

- **WHEN** a computed name exceeds the display-name length limit
- **THEN** the persisted name is within the limit
- **AND** the session prefix, work item number, and any role suffix are intact
- **AND** only the trailing title slug is shortened

### Requirement: A session's name is delivered to its harness

The system SHALL deliver a session's display name into the harness the session
runs, so that the harness's own session list shows the same name AO shows. The
delivered name SHALL be byte-identical to the name AO persists and displays.

Delivery SHALL be expressed as an optional agent-adapter capability with two
forms:

- An **in-harness rename command**, which the system SHALL use for any name
  change applied to an already-running session.
- An **optional launch argument**, which the system SHALL prefer at spawn time
  when the harness offers one, so the name is applied atomically with process
  start and cannot race the harness's own naming.

A harness that offers neither form SHALL simply keep AO's own display name, and
the system SHALL NOT attempt to name it by writing blindly into its input.

#### Scenario: Harness with a launch argument is named at start

- **WHEN** a session spawns on a harness that offers a launch naming argument
- **THEN** the launch command carries the computed name
- **AND** no post-start rename is issued for that spawn

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

### Requirement: Renames and rebinds reach the running session

A rename of a live session SHALL update both the persisted display name and the
name inside the running harness. Rebinding a live worker to a different work item
SHALL recompute the display name from the new work item and SHALL deliver the
recomputed name to the harness through the same path a rename uses.

A rename or rebind targeting a session that is terminated, or that has no running
runtime, SHALL update persisted state without attempting harness delivery.

#### Scenario: Sidebar rename updates the harness

- **WHEN** an operator renames a live session
- **THEN** the persisted display name is updated
- **AND** the new name is delivered to the running harness

#### Scenario: Rebinding a worker recomputes and delivers

- **WHEN** a live worker is rebound to a different work item
- **THEN** the display name is recomputed from the new work item
- **AND** the recomputed name is delivered to the running harness

#### Scenario: Renaming a terminated session skips delivery

- **WHEN** a rename targets a terminated session
- **THEN** the persisted display name is updated
- **AND** no naming input is written to any runtime

### Requirement: The startup prompt slot always belongs to the prompt

Where a harness accepts a single positional startup argument, the system SHALL
use that argument for the session's initial prompt and SHALL NOT use it to
deliver a name. Naming SHALL never displace, delay, or concatenate with the
initial prompt.

#### Scenario: Positional argument carries the prompt

- **WHEN** a launch command is built for a session that has both a prompt and a
  computed name
- **THEN** the harness's positional startup argument contains the prompt
- **AND** it does not contain a rename command

#### Scenario: A named session still runs its task

- **WHEN** a session is spawned with both a name and a prompt
- **THEN** the initial prompt reaches the agent
- **AND** the session does not come up waiting for input

### Requirement: Post-start writes wait for the harness to be ready

Before writing any content into a newly created session's input, the system SHALL
wait for evidence that the harness is ready to receive input, bounded by a
deadline. Exceeding the deadline SHALL NOT fail the spawn; the system SHALL
proceed with the write and SHALL record that it did so without confirmation.

#### Scenario: Write waits for readiness

- **WHEN** a session is created on a harness that requires a post-start write
- **THEN** the system waits for harness readiness before issuing that write

#### Scenario: A silent harness does not block a spawn

- **WHEN** a harness produces no readiness evidence before the deadline
- **THEN** the write is issued anyway
- **AND** the spawn is not failed for the missing confirmation
- **AND** the unconfirmed readiness is logged

### Requirement: A failed name never destroys a working session

A failure to deliver a name SHALL NOT fail a spawn or tear down a session whose
harness is alive, because a name is cosmetic relative to the session's task and
AO retains its own display name regardless.

This forgiveness SHALL apply only when the runtime is confirmed alive. When a
name delivery fails and the runtime cannot be confirmed alive, the system SHALL
treat the spawn as failed and roll it back, so a harness that died before doing
any work is never reported as a live idle session.

#### Scenario: Naming failure against a live session is tolerated

- **WHEN** name delivery fails during spawn and the runtime is confirmed alive
- **THEN** the session is kept
- **AND** the session retains AO's computed display name
- **AND** the failure is logged

#### Scenario: Naming failure against a dead session fails the spawn

- **WHEN** name delivery fails during spawn and the runtime cannot be confirmed
  alive
- **THEN** the spawn is failed and rolled back
- **AND** the session is not left listed as live and idle

### Requirement: Shipped guidance does not teach agents to name sessions

Shipped agent-facing guidance SHALL NOT instruct an agent to supply a session
name when spawning, and SHALL NOT describe the name flag as required. This
covers every artifact the system ships to agents, including the daemon-embedded
usage skill, role instructions, and orchestrator policy.

Automated verification SHALL fail if shipped guidance pairs a spawn instruction
with a name flag, and SHALL fail if a file it claims to cover is absent, so the
check cannot pass vacuously.

#### Scenario: Shipped guidance omits the name flag

- **WHEN** shipped agent guidance describes spawning a session
- **THEN** it does not pass a display name
- **AND** it does not describe the name flag as required

#### Scenario: The guard fails on a missing covered file

- **WHEN** a file the guard claims to cover does not exist
- **THEN** the guard fails rather than skipping that file
