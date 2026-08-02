## ADDED Requirements

### Requirement: Identity and presentation are separate schemas

The daemon SHALL treat a session's identity and its display name as separate,
one-directional schemas. Identity SHALL be stable and non-recycling. Presentation
MAY be computed from the session's role and work item and MAY change without
changing identity. Identity SHALL NOT be re-derived from presentation.

#### Scenario: Display-name ownership remains unchanged

- **WHEN** a worker is spawned for an issue after the identity schema is enabled
- **THEN** its display name is still the daemon-computed `sessionPrefix #issue slug` form specified by `session-naming`
- **AND** the display name shown by AO and delivered to the harness is byte-identical
- **AND** the identity suffix does not appear in the display name

#### Scenario: Rename does not move external state

- **WHEN** an existing session's display name is changed
- **THEN** its session identity remains unchanged
- **AND** its existing workspace, runtime handle, branch, and harness session continue to resolve from the stored identity and metadata

### Requirement: A database generation namespaces every new session identity

The daemon SHALL mint and persist one 64-bit lowercase-hex generation token for
each database generation. The token SHALL be stable for the lifetime of that
database and SHALL be regenerated when a new database is initialized.

Every newly-created project session identity SHALL have the form
`{project}-{num}-{generation}`. Every newly-created projectless Prime identity
SHALL have the form `prime-{num}-{generation}`. `{num}` SHALL remain the existing
per-project (or projectless-Prime) monotonic sequence.

The daemon SHALL refuse to mint a new session identity when the persisted
generation token is absent or malformed rather than silently falling back to a
recyclable identity.

#### Scenario: Sessions in one generation keep the existing sequence

- **WHEN** two sessions are created for one project in the same database generation
- **THEN** their identities have distinct monotonic `num` components
- **AND** they share the same generation token

#### Scenario: Different projects remain distinct

- **WHEN** the first session is created for each of two projects in one database generation
- **THEN** both identities have `num=1`
- **AND** their project components keep the identities distinct
- **AND** they share the database generation token

#### Scenario: Rebuilt database cannot recycle an identity

- **WHEN** a database containing `{project}-1-{old-generation}` is replaced by a freshly initialized database
- **AND** the new database's counter begins at 1
- **THEN** the new identity is `{project}-1-{new-generation}`
- **AND** `{new-generation}` differs from `{old-generation}`
- **AND** the new identity cannot equal the surviving old identity

#### Scenario: Projectless Prime is non-recycling

- **WHEN** a projectless Prime is created at `num=1` in a freshly initialized database
- **THEN** its identity includes that database's generation token
- **AND** it cannot equal a `prime-1-{old-generation}` identity from an earlier database generation

#### Scenario: Invalid generation stops creation

- **WHEN** the daemon settings row contains an empty or non-hex generation token
- **THEN** session creation fails before any session row is inserted
- **AND** no tokenless recyclable identity is minted

### Requirement: External namespaces key on the non-recycling identity

The daemon SHALL key every newly created worker session's managed filesystem workspace path, Claude
project-directory slug, tmux runtime session, and default VCS branch on the
non-recycling session identity. No one of those namespaces SHALL replace
the identity with the recyclable `{project}-{num}` head.

Harness-native session identity SHALL be persisted independently in
`agent_session_id` when the harness supplies one; it SHALL NOT be re-derived from
the AO session identity except as the documented legacy compatibility fallback
for pre-existing rows.

#### Scenario: ID-derived namespaces differ after rebuild

- **WHEN** two database generations each create `{project}` session `num=1`
- **THEN** the two session identities differ
- **AND** their managed workspace paths differ
- **AND** their tmux session names differ
- **AND** their default branches differ
- **AND** their Claude project-directory slugs differ transitively through the workspace path

#### Scenario: Claude native identity remains independently persisted

- **WHEN** a new Claude Code session is launched
- **THEN** its native UUID is minted and persisted through `agent_session_id`
- **AND** restore reuses that persisted UUID
- **AND** the legacy AO-ID-derived UUID remains only a fallback for rows that predate persisted native identity

### Requirement: Existing session identities are never rewritten

A migration enabling the identity schema SHALL NOT rewrite any existing session
ID. Existing rows, their foreign-key relationships, workspaces, tmux sessions,
branches, and harness sessions SHALL continue to resolve under their current
identities.

#### Scenario: Live pre-change session survives migration

- **WHEN** a live session with legacy identity `{project}-{num}` exists while the generation-token migration is applied
- **THEN** its stored session ID remains `{project}-{num}`
- **AND** restore uses its existing workspace, runtime handle, branch, and `agent_session_id`
- **AND** only sessions created after the migration receive the generation suffix

### Requirement: The identity schema is documented as one table

The repository SHALL document all nine session-name surfaces in one schema table.
For each surface the table SHALL state whether it is identity, presentation, or a
native harness identity, and SHALL state the value it derives from or persists.
Any external namespace that intentionally does not derive from the non-recycling
session identity SHALL be explicitly justified rather than left implicit.

#### Scenario: Operator can trace every surface

- **WHEN** a maintainer reads the session-identity schema document
- **THEN** the AO session ID, display name, `sessionPrefix`, Claude UUID, workspace path, Claude project-directory slug, tmux session, branch, and `agent_session_id` are all present
- **AND** the document distinguishes current behavior from deferred per-surface readability adoption
