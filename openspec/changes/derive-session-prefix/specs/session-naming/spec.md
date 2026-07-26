## ADDED Requirements

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
