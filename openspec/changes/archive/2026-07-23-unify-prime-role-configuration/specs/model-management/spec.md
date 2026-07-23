## MODIFIED Requirements

### Requirement: Model management is surfaced consistently

The system SHALL expose model overrides, harness catalogs, supported effort
values, catalog provenance, and availability state through the daemon API,
`ao spawn`, project creation, project settings, role/reviewer configuration,
worker-mix configuration, and fleet Prime settings without requiring clients to
duplicate provider or effort compatibility rules.

#### Scenario: Project creation shows defaults for selected harnesses

- **WHEN** the operator selects worker, orchestrator, or reviewer harnesses in
  first-run project setup
- **THEN** the setup dialog shows one default model and effort row for each
  selected concrete harness
- **AND** duplicate selected harnesses appear once

#### Scenario: Project creation stores selected harness defaults

- **WHEN** the operator creates a project with default model or effort values
  for selected harnesses
- **THEN** project creation persists those values in `agentConfig.modelByHarness`
- **AND** unselected harnesses and blank harness defaults are omitted

#### Scenario: Manual first-run model entry remains possible

- **WHEN** the operator types a model id that is not present in the current
  model catalog during first-run setup
- **THEN** the value remains editable and submittable
- **AND** the dialog shows an inline notice that launch may fail if the harness
  rejects the model

#### Scenario: First-run refresh controls distinguish scope

- **WHEN** the first-run setup dialog renders model and harness controls
- **THEN** the model catalog refresh affordance is labeled separately from
  harness availability refresh

#### Scenario: CLI spawn accepts a model pin

- **WHEN** the operator runs `ao spawn --model gpt-5-codex`
- **THEN** the spawn request sends the model pin to the daemon and the created
  session read model includes the recorded model

#### Scenario: Model catalog can be refreshed explicitly

- **WHEN** a client requests `GET /agents/models?force=true`
- **THEN** AO bypasses the request cache, runs bounded harness discovery, and returns a timestamped catalog
- **AND** a harness discovery failure is returned as an error or visible fallback rather than an empty successful catalog

#### Scenario: Every picker uses the dynamic harness registry

- **WHEN** the installed registry contains a supported harness
- **THEN** project, role, reviewer, worker-mix, and fleet Prime model selectors render its catalog and effort choices
- **AND** no client hardcoded harness list is required

#### Scenario: Fallback is visible and recorded

- **WHEN** live discovery fails but a known or cached catalog is available
- **THEN** the selector remains usable and identifies the fallback source as unverified
- **AND** AO does not silently change an explicit model and effort pair

#### Scenario: Settings preserves hidden config

- **WHEN** the project settings UI edits model-management fields
- **THEN** it saves those fields without dropping unrelated hidden project
  config such as env, symlinks, post-create commands, or intake settings

#### Scenario: API schema includes model-management fields

- **WHEN** the OpenAPI and TypeScript schemas are generated
- **THEN** project config, spawn requests, session read models, reviewer config,
  model catalog, effort metadata, and availability responses include the fields clients need

#### Scenario: Prime settings use the shared model picker

- **WHEN** the operator edits fleet Prime harness, model, or effort
- **THEN** Prime settings use the same harness-local catalog, effort, custom model, and warning behavior as the shared model picker
