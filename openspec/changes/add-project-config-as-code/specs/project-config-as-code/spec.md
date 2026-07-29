## ADDED Requirements

### Requirement: Export project config as canonical JSON

The system SHALL provide `ao project config export <project>` that writes the
named project's full effective configuration to stdout as JSON. The output
SHALL be canonical — deterministic key ordering and stable formatting — so that
two exports of unchanged config are byte-identical and the output is
diff-friendly under version control. The command SHALL resolve the project by
the same identifier accepted by the existing `ao project config` subcommands and
SHALL exit nonzero with a usage error when the project does not exist.

#### Scenario: Export emits canonical JSON for an existing project

- **WHEN** an operator runs `ao project config export <project>` for a project
  that exists
- **THEN** the command prints the project's full effective config as JSON to
  stdout and exits zero

#### Scenario: Two exports of unchanged config are byte-stable

- **WHEN** `ao project config export <project>` is run twice with no config
  change in between
- **THEN** the two outputs are byte-for-byte identical

#### Scenario: Export of an unknown project fails loudly

- **WHEN** an operator runs `ao project config export <project>` for a project
  that does not exist
- **THEN** the command prints an error naming the project and exits with a
  usage error (exit code 2)

### Requirement: Surgical apply of a partial config spec

The system SHALL provide `ao project config apply <file>` that reads a JSON
config spec from `<file>` and applies it to the named project's live config,
mutating **only** the fields named in the spec and leaving every field not named
in the spec unchanged. Applying a spec that equals the current exported config
SHALL change nothing (round-trip stability). The command SHALL exit nonzero with
a usage error when the spec file is missing, unreadable, or not valid JSON, and
SHALL report which fields it changed.

#### Scenario: Export then apply round-trips with no change

- **WHEN** an operator exports a project's config to a file and immediately runs
  `ao project config apply <file>` for the same project
- **THEN** no field of the live config changes and the command reports zero
  changed fields

#### Scenario: A two-field spec changes exactly those two fields

- **WHEN** an operator applies a spec file that names exactly two config fields
  with new values
- **THEN** those two fields take the new values, every other field retains its
  prior value, and the command reports exactly those two fields as changed

#### Scenario: Applying an invalid spec file fails loudly

- **WHEN** an operator runs `ao project config apply <file>` where `<file>` is
  missing, unreadable, or not valid JSON
- **THEN** the command prints an error describing the problem, exits with a
  usage error (exit code 2), and does not mutate live config

### Requirement: Diff a config spec against live config with a drift exit code

The system SHALL provide `ao project config diff <file>` that compares a JSON
config spec against the named project's live config and reports drift. When
every field named in the spec matches live config, the command SHALL print no
drift and exit zero. When any named field disagrees, the command SHALL print
each drifted field with its spec value and live value and exit nonzero, so the
command can gate a CI job or a scheduled drift check. Fields not named in the
spec SHALL be ignored, consistent with surgical apply. Diff SHALL never mutate
live config.

#### Scenario: Diff of matching config exits zero

- **WHEN** an operator runs `ao project config diff <file>` and every field
  named in the spec matches live config
- **THEN** the command reports no drift and exits zero

#### Scenario: Diff names each drifted field and exits nonzero

- **WHEN** an operator runs `ao project config diff <file>` and one or more
  fields named in the spec disagree with live config
- **THEN** the command prints each drifted field with its spec value and live
  value and exits nonzero

#### Scenario: Diff ignores fields not named in the spec

- **WHEN** a spec names a subset of config fields and only unnamed fields differ
  from live config
- **THEN** the command reports no drift and exits zero
