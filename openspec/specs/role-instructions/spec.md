# role-instructions Specification

## Purpose

TBD - created by archiving change role-prompt-transparency. Update Purpose after archive.

## Requirements

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

- **WHEN** an operator runs `ao role prompt <project> <role>` for a valid project and role
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

### Requirement: No un-inspectable prompt content

Every constituent piece of any AO-assembled role prompt SHALL be reachable through the effective
prompt visibility surface. The system SHALL keep only a slim identity/mechanics scaffold in product
code and SHALL place substantive role doctrine in operator-controlled instruction files.

#### Scenario: Slim scaffold plus policy files are visible

- **WHEN** the effective prompt for any AO-assembled role is rendered
- **THEN** the output includes the slim scaffold and every configured operator policy segment in
  assembly order
- **AND** no hardcoded doctrine segment reaches the agent without being inspectable

### Requirement: Reviewer defaults are cross-family

The system SHALL represent agent family separately from individual harness names and SHALL resolve an
unconfigured project reviewer default to a reviewer from a different family than the worker whenever
a classified different-family reviewer is available. Unknown or unclassified harness families SHALL
remain advisory and SHALL NOT hard-block review status evaluation by themselves.

#### Scenario: First-run setup can keep the automatic reviewer

- **WHEN** the operator creates a project without selecting an explicit reviewer
  harness
- **THEN** project creation omits reviewer configuration
- **AND** the UI labels the reviewer choice as an automatic independent reviewer

#### Scenario: First-run setup can store an explicit reviewer

- **WHEN** the operator selects a concrete reviewer harness during first-run
  setup
- **THEN** project creation persists that harness as the first reviewer
  configuration

#### Scenario: Unconfigured reviewer default avoids the worker family

- **WHEN** a project has no explicit reviewer harness configured and the worker harness belongs to a
  known family
- **THEN** reviewer resolution chooses an available reviewer harness from a different known family
- **AND** the UI labels the default with the resolved cross-family behavior rather than a dangling
  "Project default" sentinel

#### Scenario: Unknown family is advisory

- **WHEN** a review status or harness has an unknown/unclassified family
- **THEN** AO does not certify same-family independence from that unknown value
- **AND** AO does not fail the merge gate solely because the family is unknown

### Requirement: Reviewer launch configuration fails loud

The system SHALL propagate errors while resolving a configured reviewer harness, model, or effort.
It SHALL NOT silently drop configured reviewer pins to zero-value launch configuration.

#### Scenario: Invalid configured reviewer pin blocks reviewer launch

- **WHEN** a project config contains a reviewer model or effort that is invalid for the resolved
  reviewer harness
- **THEN** reviewer launch fails with a clear error naming the invalid configured value
- **AND** AO does not run the reviewer with account defaults in place of the configured pin

#### Scenario: Unknown reviewer harness falls back only when unconfigured

- **WHEN** a project explicitly configures an unknown reviewer harness
- **THEN** saving or resolving that config fails loudly
- **AND** AO does not silently replace it with a worker-family reviewer

### Requirement: Reviewer health and model controls are first-class

The system SHALL include reviewer harnesses in install/auth health monitoring and SHALL expose
reviewer model and effort pins through the same dynamic harness/model picker behavior used by other
agent roles.

#### Scenario: Reviewer harness health is monitored

- **WHEN** a configured reviewer harness binary is missing or unauthenticated
- **THEN** the agent health surface reports an actionable reviewer health state with a remedy
- **AND** unknown health remains advisory

#### Scenario: Reviewer model picker is harness-local

- **WHEN** an operator switches the reviewer harness in project settings
- **THEN** the reviewer model and effort picker shows the selected harness's dynamic catalog
- **AND** stale model or effort values from the previous harness are cleared or restored only from
  that harness's saved pair

### Requirement: Review gate status is unambiguous

The system SHALL use one shared review-status contract for final-review writers and merge-gate
readers. Clean reviews that still require human merge authorization SHALL be represented separately
from failed reviews.

#### Scenario: Shared status context prevents literal drift

- **WHEN** final review records a clean verdict for a pull request head
- **THEN** the merge gate reads the same shared status context and recognizes the clean verdict for
  that exact head

#### Scenario: Merge park is not review failure

- **WHEN** a pull request has a clean review verdict but cannot be autonomously merged because human
  authorization or a sensitive-path rule is required
- **THEN** AO records a merge-park state
- **AND** the review state remains clean rather than failed

#### Scenario: Stale or malformed review evidence does not certify current head

- **WHEN** review evidence belongs to a stale pull request, a previous head SHA, a non-array review
  response, or a malformed submitted timestamp
- **THEN** the merge gate does not treat that evidence as a current clean review

### Requirement: Reviewer execution produces durable verdicts

The system SHALL run reviewer adapters in a mode that waits for a foreground verdict and records the
result before review is considered complete.

#### Scenario: Fire-and-forget reviewer dispatch is not accepted

- **WHEN** a reviewer adapter launches successfully but exits before producing a verdict
- **THEN** final review does not mark the pass clean
- **AND** the failure is visible in the review progress/status output
