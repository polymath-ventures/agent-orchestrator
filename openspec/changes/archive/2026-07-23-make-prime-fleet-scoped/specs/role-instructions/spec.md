## MODIFIED Requirements

### Requirement: Per-role operator instruction override

The system SHALL allow an operator to configure, per project and per
project-owned role (worker, orchestrator, reviewer), a pointer to an
operator-owned instructions file whose contents are injected into that role's
assembled prompt on the next spawn. Repo-relative files SHALL remain confined to
the project root. Intentionally absolute/shared instruction paths SHALL be
supported for operator policy files, loaded fail-closed, and represented in
prompt-policy provenance. The fleet Prime role SHALL use the fleet Prime
settings instructions/rules file instead of project configuration.

#### Scenario: Absolute shared override content is injected unchanged on next spawn

- **WHEN** an operator configures a role instructions-file override with an absolute path to a valid
  shared policy file
- **THEN** the next spawn for that role includes the file contents unchanged aside from
  surrounding-whitespace normalization
- **AND** the session records prompt-policy provenance for that injected file

#### Scenario: Repo-relative override remains root confined

- **WHEN** an operator configures a repo-relative role instructions-file override
- **THEN** the loader reads it through the project-root-confined path handling
- **AND** traversal outside the project root fails loudly before spawn

#### Scenario: Prime instructions are fleet-scoped

- **WHEN** an operator configures Prime instructions through fleet Prime settings
- **THEN** the next Prime spawn receives those instructions from fleet settings
- **AND** no project Prime rules field is required or consulted

### Requirement: Effective-prompt visibility surface

The system SHALL expose a read-only surface that renders the exact,
fully-assembled prompt a given role receives at request time. Project-owned
roles (worker, orchestrator, reviewer) SHALL be inspectable for a given project.
The fleet Prime role SHALL be inspectable through a fleet-scoped prompt surface
that does not require a project id.

#### Scenario: Prime prompt is inspectable

- **WHEN** an operator requests the effective prompt for the fleet prime role
- **THEN** the system returns the complete assembled prime prompt text
- **AND** no role prompt segment assembled by AO is omitted from the visibility output

### Requirement: Effective-prompt visibility available from CLI and UI

The effective-prompt visibility surface SHALL be reachable from the `ao` CLI and
from the supervisor UI. The CLI command SHALL obtain the assembled prompt from
the daemon rather than assembling prompts itself, and SHALL return usage errors
for invalid project or role arguments. Project-owned roles SHALL remain
available as `ao role prompt <project> <role>`; fleet Prime SHALL be available
through a fleet-scoped CLI path that does not require a project argument.

#### Scenario: CLI prints the assembled prompt

- **WHEN** an operator runs `ao role prompt <project> <role>` for a valid project-owned role
- **THEN** the CLI prints the exact assembled prompt obtained from the daemon

#### Scenario: CLI rejects an unknown role

- **WHEN** an operator runs `ao role prompt <project> <role>` with a role that is not one of the
  supported project-owned roles
- **THEN** the CLI exits with a usage error (exit code 2) and does not print a prompt

#### Scenario: UI renders the assembled prompt read-only

- **WHEN** an operator opens the role-prompt inspector in the supervisor UI for a role
- **THEN** the UI renders the same assembled prompt the daemon returns, presented read-only

#### Scenario: CLI prints the fleet Prime prompt

- **WHEN** an operator runs the fleet-scoped Prime prompt command
- **THEN** the CLI prints the exact assembled Prime prompt obtained from the daemon
- **AND** the command does not require a project id
