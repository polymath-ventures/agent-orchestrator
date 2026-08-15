## ADDED Requirements

### Requirement: Session initial context is inspectable

AO SHALL expose a read-only session-scoped initial-context inspection surface for every non-terminated session and every persisted session for which a best-effort historical reconstruction is possible. The surface SHALL identify the session, kind, harness, model, effort, mode, project, issue, and whether the response is an exact launch-time snapshot or a reconstructed best-effort view.

#### Scenario: Active session context is returned

- **WHEN** an operator requests the initial context for a live worker session
- **THEN** AO returns that session's launch-time context inspection document
- **AND** the response is scoped to the requested session id rather than the session's project and role defaults

#### Scenario: Historical session is marked reconstructed

- **WHEN** an operator requests the initial context for a persisted session that has no launch-time context snapshot
- **THEN** AO returns a best-effort reconstruction when enough durable state exists
- **AND** the response marks the context as reconstructed rather than exact

#### Scenario: Missing snapshot is marked reconstructed

- **WHEN** a session exists but no launch-time context snapshot was recorded because the session is historical or capture failed during spawn
- **THEN** AO returns a best-effort reconstruction when enough durable state exists
- **AND** the response warning says that no snapshot was recorded rather than attributing the absence only to age

#### Scenario: Unknown session is rejected

- **WHEN** an operator requests initial context for a session id that does not exist
- **THEN** AO returns the existing not-found error shape for sessions

### Requirement: Context segments preserve assembly order and provenance

AO SHALL represent initial context as an ordered list of segments. Each segment SHALL include a stable source identifier, the segment role in the assembled context, an optional absolute file path when a file source exists, a byte count, whether it contributed content, whether it was redacted, whether it was reconstructed, and the segment content when not redacted.

#### Scenario: Concatenating segments reproduces context

- **WHEN** a session has an exact launch-time context snapshot
- **THEN** concatenating the content of all contributing, non-redacted segments in order reproduces the assembled initial context byte-for-byte
- **AND** the response reports the total byte count of that assembled context

#### Scenario: File source includes absolute path

- **WHEN** a segment came from an on-disk file
- **THEN** the segment identifies the source structurally
- **AND** the segment includes the absolute file path that was used at launch time

#### Scenario: Non-file source is structurally named

- **WHEN** a segment came from a computed field, project setting, role prompt scaffold, spawn prompt, harness-native injection, or other non-file source
- **THEN** the segment uses a structural source identifier such as `projectConfig.agentRules`, `ao.role.worker.scaffold`, or `spawn.prompt`
- **AND** the segment is not labelled only as `other`

### Requirement: Consulted empty sources are explicit

AO SHALL include explicit segment entries for sources that were considered during context assembly but contributed no bytes. These entries SHALL report zero bytes, `contributed=false`, and their source identifier.

#### Scenario: Empty project rules are visible

- **WHEN** a worker session is spawned for a project with no configured worker rules
- **THEN** the context inspection response includes a zero-byte segment for the project worker rules source
- **AND** the source is not silently omitted

### Requirement: Secret-bearing values are not exposed

AO SHALL NOT expose raw secret-bearing values through the initial-context inspection API, CLI, or UI. When a secret-bearing source participates in context assembly, AO SHALL include an explicit redacted segment entry that names the source, reports redaction, and omits the raw secret content.

#### Scenario: Secret segment is redacted

- **WHEN** a launch-time context segment is sourced from secret-bearing configuration or environment
- **THEN** the inspection response includes the segment with `redacted=true`
- **AND** the raw secret value is absent from both JSON and human-readable output

#### Scenario: Runtime environment provenance is visible without values

- **WHEN** AO records an exact launch-time context snapshot
- **THEN** the response includes a redacted `ao.runtime.env` launch segment
- **AND** environment values delivered outside prompt text are not exposed as segment content

### Requirement: Context inspection is available through API and CLI

AO SHALL expose session initial-context inspection through a daemon API endpoint and through an `ao session context <id>` CLI command. The CLI SHALL fetch the data from the daemon rather than assembling context locally, SHALL support a `--json` mode that prints the API payload, and SHALL provide a stable human-readable rendering for normal output.

#### Scenario: CLI prints JSON payload

- **WHEN** an operator runs `ao session context <id> --json`
- **THEN** the CLI prints machine-readable JSON matching the daemon response

#### Scenario: CLI prints human-readable segments

- **WHEN** an operator runs `ao session context <id>` without `--json`
- **THEN** the CLI prints the session identity summary followed by ordered segment entries with source identifiers, paths when present, byte counts, and content or redaction notices

#### Scenario: CLI rejects missing id

- **WHEN** an operator runs `ao session context` without a session id
- **THEN** the CLI exits with a usage error and does not contact the daemon

### Requirement: Session context works across roles and modes

AO SHALL capture and inspect initial context for prime, orchestrator, and worker sessions, and for both TUI and Chat-mode launches.

#### Scenario: Prime context is inspectable

- **WHEN** an operator requests context for a Prime session
- **THEN** AO returns the Prime launch context segments
- **AND** project-specific fields that do not apply are represented as absent or explicitly non-contributing rather than fabricated

#### Scenario: Chat context is inspectable

- **WHEN** an operator requests context for a Chat-mode session
- **THEN** AO returns the same launch-time system and task context that was delivered to the Chat controller
- **AND** the response identifies the session mode as `chat`
