# session-naming Specification

## Purpose

A session's name is owned by the daemon. It is computed once, at the moment the
session's identity is established, from the session's role and work item — and
that same string is rendered on every surface: the AO sidebar, `ao session ls`,
and the harness's own session list, which is what the Claude and Codex desktop
and mobile apps display. Before this capability the sidebar name and the harness
name were invented independently, so a worker AO called `cc #1929 sqlite3`
appeared on the operator's phone as a server-derived summary title.

Naming is cosmetic relative to the session's task, and the requirements below are
shaped by that: a naming failure never costs a working session, the initial
prompt never yields its place to a rename, and a name is never written into a
terminal where it could be interpreted as something other than a name.

## Requirements

### Requirement: The daemon owns a session's display name

The daemon SHALL be the single authority for a session's display name. It SHALL
compute the name at the moment the session's identity is established — on spawn
and on explicit rename — and SHALL persist exactly one name per session.

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

An operator-supplied name SHALL be held to that same limit on every path that
accepts one, including rename, and SHALL be rejected rather than silently
shortened, so no path can persist a name that exceeds the cap the computed names
obey.

#### Scenario: An over-cap operator name is rejected

- **WHEN** a rename supplies a name longer than the display-name limit
- **THEN** the rename is rejected
- **AND** the session keeps its previous name

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

### Requirement: A project's session prefix is derived at creation

The system SHALL supply a project's session prefix at creation rather than
leaving it blank for the naming grammar to fall back on.

When a project is created without an operator-supplied session prefix, the system
SHALL derive one from the project's name and persist it on the project, so the
stored value is the one the operator sees and can edit.

Derivation SHALL be stated in one place and SHALL be deterministic: the same
project name and project id, evaluated against the same set of prefixes already
in use, SHALL yield the same prefix. The id participates because it is what the
rule falls back to when the name yields nothing usable.

A derived prefix SHALL be at most three characters. The prefix identifies the
project at a glance while the work item number carries the identifying detail, so
uniqueness takes priority over readability.

An operator-supplied prefix SHALL always win over derivation, on every path that
accepts one. Derivation fills a blank; it never overrides a choice.

#### Scenario: A created project gets a name-derived prefix

- **WHEN** a project is created with a name and no session prefix
- **THEN** a prefix derived from that name is persisted on the project
- **AND** the prefix is at most three characters
- **AND** the prefix is not blank
- **AND** the prefix is derived by the stated rule rather than by slicing the
  project id to the display cap

#### Scenario: A multi-word name yields its initials

- **WHEN** a project named with several words is created with no session prefix
- **THEN** the derived prefix is built from the leading characters of those words,
  up to the three-character cap

#### Scenario: A single-word name yields its leading characters

- **WHEN** a project whose name is a single word is created with no session prefix
- **THEN** the derived prefix is the leading characters of that word, up to the
  three-character cap

#### Scenario: An operator-supplied prefix is never overwritten

- **WHEN** a project is created with an explicit session prefix
- **THEN** that prefix is persisted unchanged
- **AND** no derivation is applied

#### Scenario: The same name derives the same prefix

- **WHEN** derivation runs twice for the same project name and project id against
  the same set of prefixes already in use
- **THEN** both runs yield the same prefix

### Requirement: A derived session prefix is unique among projects

A derived prefix SHALL be checked against the prefixes already in use by other
projects, and a collision SHALL yield a distinct prefix rather than a duplicate
whenever the derivation's own candidate space still holds a free value.

Collision resolution SHALL first lengthen the candidate using further characters
from the project's own name, and SHALL fall back to the smallest unused numeric
suffix that keeps the prefix within the three-character cap only when the name
offers no distinguishing characters.

That candidate space is finite, which bounds the guarantee: the search covers a
fixed alphabet at every width the cap allows, and uniqueness SHALL hold while any
of those values is free. It is not a claim about every prefix the cap could
represent — a name-drawn prefix may carry characters the search does not
enumerate. Once the search's values are all taken, the system SHALL still create
the project, accepting a duplicate prefix, rather than fail it: a project that
cannot be registered is a worse outcome than a prefix an operator can retype.

When a project's name yields no usable characters, the system SHALL derive a
distinct token from the project's id instead. This path SHALL NOT emit a value
shared by every such project — a prefix shared across unrelated projects is the
condition this requirement exists to prevent — and SHALL NOT fail project
creation.

Existing projects SHALL keep whatever prefix they resolve to today. This
requirement governs creation only; it is not a migration, and no existing project
is renamed by it.

#### Scenario: A colliding derivation lengthens from the name

- **WHEN** a project is created whose name derives a prefix another project
  already uses
- **AND** the name offers further distinguishing characters
- **THEN** the persisted prefix is a longer candidate drawn from that name
- **AND** it differs from every prefix already in use, the candidate space not
  being exhausted

#### Scenario: An exhausted name falls back to a numeric suffix

- **WHEN** a project is created whose name derives a prefix another project
  already uses
- **AND** the name offers no further distinguishing characters within the cap
- **THEN** the persisted prefix carries the smallest unused numeric suffix that
  fits the three-character cap
- **AND** it differs from every prefix already in use, the candidate space not
  being exhausted

#### Scenario: An unusable name still yields a distinct prefix

- **WHEN** a project is created whose name yields no usable characters
- **THEN** project creation succeeds
- **AND** the persisted prefix is derived from the project's id
- **AND** two such projects do not receive the same prefix, the candidate space
  not being exhausted

#### Scenario: An exhausted prefix space still creates the project

- **WHEN** a project is created and every prefix the cap can express is already
  in use
- **THEN** project creation succeeds
- **AND** the persisted prefix is non-blank and within the cap
- **AND** it may duplicate an existing prefix, which the operator can retype

#### Scenario: Existing projects are not renamed

- **WHEN** a project that predates this requirement resolves its session prefix
- **THEN** it resolves to the same prefix it resolved to before
- **AND** no derived prefix is written to it
