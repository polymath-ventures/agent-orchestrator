## MODIFIED Requirements

### Requirement: Per-role operator instruction override

The system SHALL allow an operator to configure, per project and per
project-owned role (worker, orchestrator, reviewer), a pointer to an
operator-owned instructions file whose contents are injected into that role's
assembled prompt on the next spawn. Repo-relative files SHALL remain confined to
the project root. Intentionally absolute/shared instruction paths SHALL be
supported for operator policy files, loaded fail-closed, and represented in
prompt-policy provenance. Project-scoped role settings SHALL label this value
as a repo-relative or absolute path. The fleet Prime role SHALL use the fleet
Prime settings instructions/rules file instead of project configuration, and
fleet Prime settings SHALL label that value as an absolute instructions file
path.

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

#### Scenario: Instructions assembly order is visible

- **WHEN** the operator edits inline instructions and an instructions file path for a role or Prime
- **THEN** the UI explains that inline content is loaded first and file content is appended after it
- **AND** the file is not described as overriding the inline field
