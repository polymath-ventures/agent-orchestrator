## ADDED Requirements

### Requirement: Export project config as canonical JSON

The system SHALL provide `ao project config export <project>` that writes the
named project's full effective configuration to stdout as JSON. The output
SHALL be canonical — deterministic key ordering and stable formatting — so that
two exports of unchanged config are byte-identical and the output is
diff-friendly under version control. Export SHALL capture every field the daemon
serializes for the project's config, independent of any typed CLI mirror, so no
field is silently dropped. The command SHALL resolve the project by the same
identifier accepted by the existing `ao project` subcommands. A missing or empty
project argument SHALL exit as a usage error (exit code 2); a project the daemon
does not recognize SHALL surface the daemon error and exit nonzero.

#### Scenario: Export emits canonical JSON for an existing project

- **WHEN** an operator runs `ao project config export <project>` for a project
  that exists
- **THEN** the command prints the project's full effective config as JSON to
  stdout and exits zero

#### Scenario: Two exports of unchanged config are byte-stable

- **WHEN** `ao project config export <project>` is run twice with no config
  change in between
- **THEN** the two outputs are byte-for-byte identical

#### Scenario: Export captures fields absent from the CLI mirror

- **WHEN** the daemon's config for a project includes fields the typed CLI
  config mirror does not carry
- **THEN** those fields still appear in the exported JSON

#### Scenario: Export with no project argument is a usage error

- **WHEN** an operator runs `ao project config export` with no project argument
- **THEN** the command exits with a usage error (exit code 2) and makes no
  daemon call

### Requirement: Surgical apply of a partial config spec

The system SHALL provide `ao project config apply <project> <file>` that reads a
JSON config spec from `<file>` and applies it to the named project's live
config, mutating **only** the top-level fields named in the spec and leaving
every field not named in the spec unchanged. Apply SHALL do this by reading the
project's current config, overlaying the named fields, and writing the merged
result back through the existing config write path. Applying a spec that equals
the current exported config SHALL change nothing (round-trip stability). The
command SHALL report which fields it changed and SHALL skip the write entirely
when the spec introduces no change. A missing, unreadable, or non-JSON spec file
SHALL exit as a usage error (exit code 2) without mutating live config; a spec
naming a field that is not a known config key SHALL be rejected by the daemon's
strict decoder, leaving live config unmutated, and the command SHALL surface
that error and exit nonzero.

#### Scenario: Export then apply round-trips with no change

- **WHEN** an operator exports a project's config to a file and immediately runs
  `ao project config apply <project> <file>` for the same project
- **THEN** no field of the live config changes and the command reports zero
  changed fields

#### Scenario: A two-field spec changes exactly those two fields

- **WHEN** an operator applies a spec file that names exactly two top-level
  config fields with new values
- **THEN** the config write carries those two fields at their new values plus
  every other live field unchanged, and the command reports exactly those two
  fields as changed

#### Scenario: Applying an invalid spec file is a usage error

- **WHEN** an operator runs `ao project config apply <project> <file>` where
  `<file>` is missing, unreadable, or not valid JSON
- **THEN** the command exits with a usage error (exit code 2), prints an error
  describing the problem, and makes no config write call

#### Scenario: A spec naming an unknown field is rejected by the daemon

- **WHEN** an operator applies a spec whose top-level keys include a name that is
  not a known config field
- **THEN** the daemon's strict decoder rejects the write, live config is not
  mutated, and the command surfaces the error and exits nonzero

### Requirement: Diff a config spec against live config with a drift exit code

The system SHALL provide `ao project config diff <project> <file>` that compares
a JSON config spec against the named project's live config and reports drift.
When every top-level field named in the spec matches live config, the command
SHALL print no drift and exit zero. When any named field disagrees, the command
SHALL print each drifted field with its spec value and live value and exit
nonzero, so the command can gate a CI job or a scheduled drift check. Fields not
named in the spec SHALL be ignored, consistent with surgical apply. Diff SHALL
never mutate live config.

#### Scenario: Diff of matching config exits zero

- **WHEN** an operator runs `ao project config diff <project> <file>` and every
  top-level field named in the spec matches live config
- **THEN** the command reports no drift and exits zero

#### Scenario: Diff names each drifted field and exits nonzero

- **WHEN** an operator runs `ao project config diff <project> <file>` and one or
  more fields named in the spec disagree with live config
- **THEN** the command prints each drifted field with its spec value and live
  value and exits nonzero

#### Scenario: Diff ignores fields not named in the spec

- **WHEN** a spec names a subset of config fields and only unnamed fields differ
  from live config
- **THEN** the command reports no drift and exits zero
