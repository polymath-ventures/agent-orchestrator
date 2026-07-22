## MODIFIED Requirements

### Requirement: Export project config as canonical JSON

The system SHALL provide `ao project config export <project>` that writes the
named project's full stored configuration (the config overrides the daemon
persists and serves) to stdout as JSON. The output SHALL be canonical —
deterministic key ordering and stable formatting — so that two exports of
unchanged config are byte-identical and the output is diff-friendly under
version control. Export SHALL capture every field the daemon serializes for the
project's config, independent of any typed CLI mirror, so no field is silently
dropped. When an exported `env` key is secret-shaped, export SHALL preserve the
value in its lossless stdout document but SHALL warn on stderr before the
document is committed; an exact key MAY be exempted through the documented
narrow override. The command SHALL resolve the project by the same identifier
accepted by the existing `ao project` subcommands. A missing or empty project
argument SHALL exit as a usage error (exit code 2); a project the daemon does not
recognize SHALL surface the daemon error and exit nonzero.

#### Scenario: Export emits canonical JSON for an existing project

- **WHEN** an operator runs `ao project config export <project>` for a project
  that exists
- **THEN** the command prints the project's full stored config as JSON to stdout
  and exits zero

#### Scenario: Two exports of unchanged config are byte-stable

- **WHEN** `ao project config export <project>` is run twice with no config
  change in between
- **THEN** the two outputs are byte-for-byte identical

#### Scenario: Export captures fields absent from the CLI mirror

- **WHEN** the daemon's config for a project includes fields the typed CLI
  config mirror does not carry
- **THEN** those fields still appear in the exported JSON

#### Scenario: Export warns without redacting a secret-shaped env key

- **WHEN** an exported config contains a non-exempt env key whose name resembles
  a credential
- **THEN** stdout still contains the lossless canonical config and stderr warns
  that the key must be reviewed before commit

#### Scenario: Export with no project argument is a usage error

- **WHEN** an operator runs `ao project config export` with no project argument
- **THEN** the command exits with a usage error (exit code 2) and makes no daemon
  call

### Requirement: Surgical apply of a partial config spec

The system SHALL provide `ao project config apply <project> <file>` that reads a
JSON config spec from `<file>` and applies it to the named project's live
config, mutating only the top-level fields named in the spec and leaving every
field not named in the spec unchanged. Apply SHALL read the current config and
its content token, overlay the named fields, and write the merged result through
the existing config write path with a matching precondition. If the config
changes after the read, the write SHALL fail with a stale-config conflict and
SHALL NOT overwrite the newer edit. Applying a spec that equals the current
exported config SHALL change nothing. An explicit zero scalar or empty container
in the spec SHALL compare equal to an absent live field when that value is
omitted by serialization, while a nonzero live field SHALL still be cleared by
applying its zero value.

Apply SHALL support repeatable `--only <field.path>` options. When present, only
the selected safe dotted object paths SHALL be copied from the spec into the
live config; every other value SHALL be retained. Each selected path SHALL
exist in the spec, unsafe path segments SHALL be rejected as usage errors, and
the merged write SHALL use the read-derived precondition.

The command SHALL report which fields or selected paths it changed and SHALL
skip the write entirely when the spec introduces no change. A missing,
unreadable, or non-JSON spec file SHALL exit as a usage error (exit code 2)
without mutating live config; a spec naming a field that is not a known config
key SHALL be rejected by the daemon's strict decoder, leaving live config
unmutated, and the command SHALL surface that error and exit nonzero.

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

#### Scenario: Concurrent edit rejects stale apply

- **WHEN** live config changes after apply reads it but before apply writes the
  merged config
- **THEN** the daemon rejects the write as stale, preserves the newer config, and
  the CLI exits nonzero with an instruction to reload and reapply

#### Scenario: Omitted zero value is already converged

- **WHEN** the spec names a zero scalar or empty container and live config omits
  that field because of `omitempty`
- **THEN** apply reports no change for that field and does not write solely to
  reintroduce the omitted zero

#### Scenario: Nested only restore changes one selected path

- **WHEN** an operator runs apply with `--only worker.agentConfig.model` and the
  path exists in the spec
- **THEN** only that nested value is restored from the spec and all other live
  values remain unchanged

#### Scenario: Unsafe or missing only path is rejected

- **WHEN** an operator selects an unsafe dotted path or a path absent from the
  spec
- **THEN** apply exits with a usage error and performs no config write

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
When every top-level field named in the spec matches live config, including
absent fields that are equivalent to an explicitly named `omitempty` zero or
empty value, the command SHALL print no drift and exit zero. When any named
field disagrees, the command SHALL print each drifted field with its spec value
and live value and exit nonzero, so the command can gate a CI job or a scheduled
drift check. Fields not named in the spec SHALL be ignored by default,
consistent with surgical apply.

When the operator enables unexpected-field reporting, meaningful live fields
absent from the spec SHALL also be reported as drift; omitted-equivalent
zero/empty live values SHALL not be reported. Diff output SHALL redact an entire
`env` value and every secret-shaped leaf value while still naming the drifted
field. Diff SHALL never mutate live config.

#### Scenario: Diff of matching config exits zero

- **WHEN** an operator runs `ao project config diff <project> <file>` and every
  top-level field named in the spec matches live config
- **THEN** the command reports no drift and exits zero

#### Scenario: Diff names each drifted field and exits nonzero

- **WHEN** an operator runs `ao project config diff <project> <file>` and one or
  more fields named in the spec disagree with live config
- **THEN** the command prints each drifted field with its spec value and live
  value and exits nonzero

#### Scenario: Diff ignores fields not named in the spec by default

- **WHEN** a spec names a subset of config fields and only unnamed fields differ
  from live config
- **THEN** the command reports no drift and exits zero

#### Scenario: Unexpected mode reports live-only fields

- **WHEN** unexpected-field reporting is enabled and live config contains a
  meaningful field absent from the spec
- **THEN** the command names that field as unexpected drift and exits nonzero

#### Scenario: Diff treats omitted zero as converged

- **WHEN** the spec names a zero scalar or empty container and live config omits
  it through `omitempty`
- **THEN** the command does not report that field as drift

#### Scenario: Diff redacts secret-shaped leaves

- **WHEN** a drifted value contains `env` data or a leaf key whose name resembles
  a credential
- **THEN** the command names the drift but does not print either secret value

## ADDED Requirements

### Requirement: Project config writes support read-derived preconditions

Project reads SHALL expose a stable token identifying the exact stored config
content. Project config writes SHALL accept an optional `If-Match` precondition;
when supplied, the daemon SHALL atomically compare it to the current token before
persisting. A stale token SHALL return a conflict with the current token and
SHALL perform no write. A successful write SHALL return the new token.

#### Scenario: Matching token authorizes a write

- **WHEN** a client writes config with the token returned by its current project
  read
- **THEN** the write succeeds and the response exposes the resulting token

#### Scenario: Stale token preserves the newer config

- **WHEN** a client writes config with a token for older content
- **THEN** the daemon returns `409 PROJECT_CONFIG_STALE`, includes the current
  token, and leaves the stored config unchanged

#### Scenario: Existing client without a token remains compatible

- **WHEN** an existing client writes config without `If-Match`
- **THEN** the daemon processes the write using the pre-existing compatibility
  behavior

### Requirement: Project metadata updates do not rewrite config

A component updating one project metadata field SHALL persist only the field it
owns and SHALL NOT rewrite a previously-read copy of project config.

#### Scenario: SCM origin backfill preserves a concurrent config edit

- **WHEN** project config changes after SCM origin backfill reads the project but
  before it stores the discovered origin URL
- **THEN** the origin URL is updated and the newer config remains unchanged
