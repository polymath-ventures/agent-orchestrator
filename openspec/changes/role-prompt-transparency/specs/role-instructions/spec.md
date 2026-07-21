## ADDED Requirements

### Requirement: Per-role operator instruction override

The system SHALL allow an operator to configure, per project and per role (worker, orchestrator,
reviewer), a pointer to an operator-owned instructions file whose contents are injected into that
role's assembled prompt on the next spawn. The override SHALL extend the role's existing instruction
sources rather than replace the base scaffold, and its content SHALL NOT be summarized, reordered, or
otherwise modified before injection (only surrounding whitespace is normalized for clean assembly).

#### Scenario: Configured override content is injected unchanged on next spawn

- **WHEN** an operator sets a role's instructions-file override for a project and a new session for
  that role is spawned
- **THEN** the referenced file's content appears unchanged (not summarized, reordered, or edited;
  aside from surrounding-whitespace normalization) in that role's assembled prompt, and no separate
  parallel prompt-assembly path is used

#### Scenario: No override configured leaves the role prompt unchanged

- **WHEN** a role has no instructions-file override configured for a project and a session is spawned
- **THEN** the role's prompt is assembled from its existing sources with no override segment, and the
  spawn succeeds

#### Scenario: Override change takes effect on next spawn, not retroactively

- **WHEN** an operator changes a role's instructions-file override while a session is already running
- **THEN** the running session is unaffected and the new contents are injected into the next spawn of
  that role

### Requirement: Fail-closed on misconfigured override

The system SHALL fail a spawn loudly when a role's configured instructions-file override cannot be
loaded as valid, non-empty content within limits, raising a clear error that identifies the project,
role, and file, and SHALL NOT silently fall back to a default or override-less prompt.

#### Scenario: Missing file fails the spawn

- **WHEN** a role's override points to a file that does not exist and a spawn is attempted
- **THEN** the spawn fails with an error naming the project, role, and missing path, and no session
  is started with a fallback prompt

#### Scenario: Empty file fails the spawn

- **WHEN** a role's override points to a file that exists but is empty (or whitespace-only)
- **THEN** the spawn fails with a clear error and no session is started

#### Scenario: Oversized file fails the spawn

- **WHEN** a role's override points to a file whose size exceeds the configured maximum
- **THEN** the spawn fails with an error stating the limit and the actual size, and no session is
  started

### Requirement: Effective-prompt visibility surface

The system SHALL expose a read-only surface that renders the exact, fully-assembled prompt a given
role receives for a given project — the base scaffold plus every injected instruction source in the
order they are assembled. The rendered prompt SHALL match what a spawn of that role would actually
receive at the time of the request.

#### Scenario: Assembled prompt is retrievable per project and role

- **WHEN** an operator requests the effective prompt for a `(project, role)` pair
- **THEN** the system returns the complete assembled prompt text, including the base scaffold and any
  operator override segment, without omitting or masking any constituent piece

#### Scenario: Visibility reflects a configured override

- **WHEN** a role has an instructions-file override configured and its effective prompt is requested
- **THEN** the returned prompt contains the verbatim override contents in the position they would be
  injected at spawn

#### Scenario: Visibility surfaces a misconfiguration instead of hiding it

- **WHEN** a role's override is configured but currently unloadable (missing, empty, or oversized)
  and its effective prompt is requested
- **THEN** the surface reports the same fail-closed error the spawn would raise, rather than showing a
  prompt that omits the override

### Requirement: Effective-prompt visibility available from CLI and UI

The effective-prompt visibility surface SHALL be reachable from the `ao` CLI (`ao role prompt
<project> <role>`) and from the supervisor UI. The CLI command SHALL obtain the assembled prompt from
the daemon rather than assembling prompts itself, and SHALL return usage errors for invalid project
or role arguments.

#### Scenario: CLI prints the assembled prompt

- **WHEN** an operator runs `ao role prompt <project> <role>` for a valid project and role
- **THEN** the CLI prints the exact assembled prompt obtained from the daemon

#### Scenario: CLI rejects an unknown role

- **WHEN** an operator runs `ao role prompt <project> <role>` with a role that is not one of the
  supported roles
- **THEN** the CLI exits with a usage error (exit code 2) and does not print a prompt

#### Scenario: UI renders the assembled prompt read-only

- **WHEN** an operator opens the role-prompt inspector in the supervisor UI for a project and role
- **THEN** the UI renders the same assembled prompt the daemon returns, presented read-only

### Requirement: No un-inspectable prompt content

Every constituent piece of any role's assembled prompt SHALL be reachable through the
effective-prompt visibility surface. The system SHALL NOT inject into any role's prompt content that
the visibility surface cannot render.

#### Scenario: Every injected source is represented in the visibility output

- **WHEN** the effective prompt for any role is rendered
- **THEN** it includes the base scaffold and every instruction source that a spawn of that role would
  receive, such that no prompt segment reaches an agent without also being visible to the operator
