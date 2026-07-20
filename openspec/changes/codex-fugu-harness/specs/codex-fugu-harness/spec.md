## ADDED Requirements

### Requirement: codex-fugu is a first-class agent harness

The system SHALL recognize `codex-fugu` as a known `AgentHarness` everywhere a
harness can be named: the domain harness vocabulary, the adapter registry, the
spawn API, the CLI, and the persisted session record. The harness identifier, the
adapter manifest ID, and the hook token SHALL all be the exact string `codex-fugu`,
because the registry derives the harness from the manifest ID and a mismatch fails
to resolve silently.

`codex-fugu` SHALL NOT be added to the reviewer-harness vocabulary, which is
deliberately narrower than the worker-harness vocabulary.

#### Scenario: Harness resolves to an adapter

- **WHEN** the daemon resolves the harness `codex-fugu`
- **THEN** it returns an adapter whose `Manifest().ID` is `codex-fugu`

#### Scenario: Harness is accepted by the spawn API

- **WHEN** a spawn request names harness `codex-fugu`
- **THEN** the request passes enum validation and the session persists without
  violating the `sessions.harness` CHECK constraint

#### Scenario: Harness is offered to the operator

- **WHEN** the operator lists harnesses via the CLI `--harness` help, the agent
  catalog, or the frontend agent options
- **THEN** `codex-fugu` appears as a selectable worker harness

#### Scenario: Reviewer vocabulary is unchanged

- **WHEN** the reviewer-harness vocabulary is enumerated
- **THEN** `codex-fugu` is absent from it

### Requirement: codex-fugu reuses the Codex adapter by parameterization

The `codex-fugu` harness SHALL be served by the existing Codex adapter type,
parameterized by manifest identity, binary name, and hook token, rather than by a
duplicated adapter implementation. An unparameterized Codex adapter SHALL continue
to behave exactly as before this change, so that the Codex defaults are preserved by
construction rather than by a parallel code path.

#### Scenario: Fugu adapter reports its own identity

- **WHEN** the fugu adapter's manifest is read
- **THEN** its ID is `codex-fugu` and its name is `Codex Fugu`

#### Scenario: Plain Codex identity is unaffected

- **WHEN** the unparameterized Codex adapter's manifest is read
- **THEN** its ID is `codex` and its name is `Codex`

#### Scenario: Fugu launches its own binary

- **WHEN** the fugu adapter builds a launch command
- **THEN** the resolved executable is the `codex-fugu` binary, not `codex`

#### Scenario: Fugu inherits Codex launch behavior

- **WHEN** the fugu adapter builds launch and restore commands
- **THEN** the flags, prompt delivery, and session-resume behavior match the Codex
  adapter's, differing only in the binary, the hook token, and the wrapper flag

### Requirement: The fugu wrapper update prompt is suppressed

The system SHALL pass `--no-update` as the **first** argument, ahead of any
subcommand, on every invocation of the `codex-fugu` binary: interactive launch,
session restore, and model/capability probes. This is required because the binary is
an auto-updating wrapper that otherwise blocks on an interactive update prompt, and
because the wrapper parses the flag only at top level. The plain Codex adapter SHALL
NOT emit this flag.

#### Scenario: Launch suppresses the prompt

- **WHEN** the fugu adapter builds a launch command
- **THEN** the argument immediately following the binary is `--no-update`

#### Scenario: Restore suppresses the prompt before the subcommand

- **WHEN** the fugu adapter builds a restore command
- **THEN** `--no-update` precedes the `resume` subcommand

#### Scenario: Probe suppresses the prompt before the subcommand

- **WHEN** the fugu adapter builds probe arguments
- **THEN** `--no-update` precedes the `exec` subcommand

#### Scenario: Plain Codex is unaffected

- **WHEN** the Codex adapter builds a launch command
- **THEN** no `--no-update` flag is present

### Requirement: Fugu sessions report activity under their own hook token

Session hooks installed for a `codex-fugu` session SHALL invoke
`ao hooks codex-fugu …` rather than `ao hooks codex …`, and the activity-dispatch
map SHALL carry a deriver for `codex-fugu`. Without both, the adapter writes
callbacks that nothing on the receiving side understands and the session's activity
is silently never reported.

#### Scenario: Hook flags carry the fugu token

- **WHEN** the fugu adapter builds a launch command
- **THEN** every managed session hook command it emits invokes
  `ao hooks codex-fugu`

#### Scenario: Activity is derived for fugu sessions

- **WHEN** an activity callback arrives for harness `codex-fugu`
- **THEN** a deriver is registered for that harness and the activity state is
  computed using the Codex derivation

### Requirement: Fugu authorization resolves through the shared Codex login

The system SHALL determine `codex-fugu` authorization by probing the plain `codex`
binary's login status, when and only when the fugu login probe fails with an
`--profile only applies` error. This is required because `codex-fugu` has no
credential of its own — its login is the shared Codex login, and
`codex-fugu login status` therefore always fails with that error.

The fallback SHALL NOT treat a merely successful process exit as authorization. An
ambiguous result — a help dump, an unrecognized response — SHALL remain unknown or
unauthorized, never authorized.

#### Scenario: Profile error falls back to the shared Codex login

- **WHEN** `codex-fugu login status` exits non-zero with output containing
  `--profile only applies` **and** `codex login status` reports a logged-in account
- **THEN** the fugu harness reports authorized

#### Scenario: Shared login is logged out

- **WHEN** `codex-fugu login status` fails with the profile error **and**
  `codex login status` reports not logged in
- **THEN** the fugu harness does not report authorized

#### Scenario: Ambiguous output is not authorization

- **WHEN** the fallback probe exits successfully but its output carries no
  recognizable login state
- **THEN** the fugu harness does not report authorized

#### Scenario: Unrelated failures do not trigger the fallback

- **WHEN** `codex-fugu login status` fails for a reason other than the profile error
- **THEN** the shared Codex login is not consulted

### Requirement: Operators can diagnose a missing fugu binary

The `doctor` command SHALL probe for the `codex-fugu` binary alongside the other
probed harnesses, so an operator on a host without it sees a named failing check
rather than an opaque spawn error.

#### Scenario: Binary present

- **WHEN** `doctor` runs on a host where `codex-fugu` is on PATH
- **THEN** the report includes a passing `codex-fugu` check with the reported version

#### Scenario: Binary absent

- **WHEN** `doctor` runs on a host without `codex-fugu`
- **THEN** the report includes a failing `codex-fugu` check naming the missing binary
