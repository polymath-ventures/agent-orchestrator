## ADDED Requirements

### Requirement: Worker external namespace labels are immutable and readable

For every newly created worker session, the daemon SHALL compute and persist one
immutable external namespace key before creating a workspace, runtime, or
default branch. The key SHALL contain a safe human-readable work label and SHALL
retain the complete non-recycling session identity as its collision component.

The daemon SHALL compute the readable label once from creation-time work context.
A later display-name change SHALL NOT change the stored namespace key or rename
an existing workspace, tmux session, or branch.

#### Scenario: New worker receives one shared readable key

- **WHEN** the daemon creates a worker for a work item
- **THEN** it persists one external namespace key before creating external resources
- **AND** the key contains a readable work label
- **AND** the key retains the complete non-recycling session identity
- **AND** the workspace, tmux runtime, and generated root branch consume that same key

#### Scenario: Similar labels remain collision-safe

- **WHEN** two new workers resolve to identical or similar readable labels
- **THEN** their external namespace keys remain distinct because their complete session identities differ
- **AND** no namespace canonicalization removes the collision-safe identity or digest

#### Scenario: Rename changes presentation only

- **WHEN** an operator renames a live session after its resources exist
- **THEN** the mutable display name changes according to `session-naming`
- **AND** the stored external namespace key remains unchanged
- **AND** the workspace path, tmux session, and branch remain unchanged

#### Scenario: Key persistence fails before resource creation

- **WHEN** the daemon cannot persist a newly created worker's namespace key
- **THEN** spawn fails before creating a workspace, runtime, or default branch
- **AND** no partially named external resources are left behind

## MODIFIED Requirements

### Requirement: External namespaces key on the non-recycling identity

The daemon SHALL key every newly created worker session's managed filesystem
workspace path, Claude project-directory slug, tmux runtime session, and default
VCS branch on one stored external namespace key. That key SHALL combine a
human-readable creation-time label with the complete non-recycling session
identity. No one of those namespaces SHALL replace, truncate without a
collision-safe digest, or re-derive the identity from presentation.

Namespace-specific canonicalization SHALL be deterministic. tmux create and
restart SHALL derive the same handle from the stored key; lookup, attach, and
destroy SHALL consume that persisted opaque handle without re-deriving it.
Explicit caller-supplied branches SHALL remain caller-owned and unchanged.
Project Orc and fleet Prime canonical singleton namespaces SHALL remain explicit
exceptions unless a separate requirement changes them.

Harness-native session identity SHALL be persisted independently in
`agent_session_id` when the harness supplies one; it SHALL NOT be re-derived from
the AO session identity except as the documented legacy compatibility fallback
for pre-existing rows.

#### Scenario: Readable ID-derived namespaces differ after rebuild

- **WHEN** two database generations each create a worker with the same readable work label and the same `{project}` session `num=1`
- **THEN** the two stored external namespace keys differ because their generation-qualified session identities differ
- **AND** their managed workspace paths differ
- **AND** their tmux session names differ
- **AND** their generated default branches differ
- **AND** each surface retains a recognizable readable label or readable canonical prefix
- **AND** their Claude project-directory slugs differ transitively through the workspace path

#### Scenario: Legacy live session keeps existing resources

- **WHEN** a session created before namespace labels is restored
- **THEN** its persisted workspace path, runtime handle, and branch remain authoritative
- **AND** the daemon does not synthesize a new readable key that renames or moves those resources
- **AND** any unavoidable recreation uses the documented legacy identity fallback

#### Scenario: Explicit branch bypasses generated naming

- **WHEN** a caller supplies an explicit branch for a worker spawn
- **THEN** AO uses that branch unchanged
- **AND** readable namespace-key generation does not rewrite the caller-owned branch

#### Scenario: Claude native identity remains independently persisted

- **WHEN** a new Claude Code session is launched
- **THEN** its native UUID is minted and persisted through `agent_session_id`
- **AND** restore reuses that persisted UUID
- **AND** the readable external namespace key does not replace the harness-native UUID
