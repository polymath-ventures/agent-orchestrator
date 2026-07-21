# model-management Specification

## Purpose
TBD - created by archiving change add-model-management. Update Purpose after archive.
## Requirements
### Requirement: Model pins are compatible with their harness

The system SHALL reject a known-provider model pin when it is configured or
requested for a known-provider harness from a different provider. Unknown
models or harnesses SHALL remain permissive so novel providers can still be
launched before AO learns their catalog.

#### Scenario: Cross-provider spawn override is rejected before worktree creation

- **WHEN** a spawn request selects harness `codex` with model `claude-opus-4-5`
- **THEN** the daemon returns a 400 error naming the harness, model, and provider mismatch
- **AND** no session row, runtime, or worktree is created

#### Scenario: Compatible spawn override is accepted

- **WHEN** a spawn request selects harness `codex` with model `gpt-5-codex`
- **THEN** the session launches with that model and records it on the session row

#### Scenario: Unknown model stays permissive

- **WHEN** a spawn request selects a known harness with an unclassified model name
- **THEN** the session launch is not rejected by the provider-compatibility guard

#### Scenario: Invalid project config is rejected

- **WHEN** project config pins an Anthropic model under a Codex harness-specific entry
- **THEN** saving that config fails with a validation error that names the invalid entry

### Requirement: Model resolution is harness-aware

The system SHALL resolve a session's launch model from a harness-aware cascade:
explicit spawn model, role-level per-harness model, project-level per-harness
model, compatible role scalar model, compatible project scalar model, then the
harness default model. A scalar model from another known provider SHALL be
ignored for that harness rather than leaked onto the launch.

#### Scenario: Per-harness role model wins

- **WHEN** project config has a project scalar model and a worker role
  `modelByHarness` entry for the resolved worker harness
- **THEN** an unpinned worker spawn launches with the role's per-harness model

#### Scenario: Scalar cross-provider model does not leak

- **WHEN** project config has scalar model `claude-opus-4-5` and a worker spawn
  resolves to harness `codex`
- **THEN** the Codex launch does not receive the Anthropic scalar model

#### Scenario: Unpinned claude-code gets the configured default

- **WHEN** a `claude-code` spawn resolves no explicit, per-harness, role, or
  project model
- **THEN** the launch receives AO's configured `claude-code` default model
  instead of relying on the account default

#### Scenario: Explicit expensive model is honored

- **WHEN** a spawn request explicitly pins an expensive but compatible model
- **THEN** AO launches that exact model and does not replace it with the default

### Requirement: Session model pins are durable

The system SHALL persist the resolved launch model on the session and SHALL use
that persisted value when restoring the session, so later project config edits
do not move an existing session to a different model.

#### Scenario: Spawn override is scoped to one session

- **WHEN** the operator spawns one session with `--model gpt-5-codex`
- **THEN** that session records `gpt-5-codex`
- **AND** the project's model defaults are unchanged

#### Scenario: Restore uses the persisted model

- **WHEN** a terminated restorable session recorded model `gpt-5-codex`
- **THEN** restore launches the adapter with `gpt-5-codex` even if the project
  config now resolves a different model

#### Scenario: Empty persisted model remains a harness default

- **WHEN** a session was launched with no resolved model
- **THEN** restore does not invent a model pin except for the harness default
  required by the model-resolution cascade

### Requirement: Model availability state is honest and cached

The system SHALL expose model availability for configured model pins with
reason codes that distinguish not-yet-probed, unavailable probe capability,
known unreachable, recovered, and no-capability states. Spawning SHALL consult
only cached verdicts and SHALL do no model probe network I/O on the spawn path.

#### Scenario: Availability has not been probed

- **WHEN** a configured model pin has no cached probe result
- **THEN** model availability reports reason code `not-probed`

#### Scenario: Harness cannot probe models

- **WHEN** the selected harness exposes no model validation capability
- **THEN** model availability reports reason code `no-capability`

#### Scenario: Probe transport is unavailable

- **WHEN** a model probe cannot complete because of auth, rate limit, timeout,
  or transient provider failure
- **THEN** model availability reports reason code `probe-unavailable`
- **AND** the spawn gate remains fail-open for that model pin

#### Scenario: Cached unreachable model warns without probing on spawn

- **WHEN** the cached verdict says a configured model is unreachable
- **THEN** spawning that pinned model does not perform a live probe
- **AND** AO surfaces the cached unreachable state to clients

#### Scenario: Background revalidation emits transitions

- **WHEN** background revalidation changes a model from reachable to unreachable
  or from unreachable to reachable
- **THEN** AO emits the corresponding `model_unreachable` or `model_recovered`
  notification intent once for that transition

### Requirement: Reviewer launches support model pins

The system SHALL allow reviewer configuration to pin a model for each reviewer
harness, resolved and validated by the same harness-aware model rules as worker
and orchestrator sessions.

#### Scenario: Reviewer model pin reaches the adapter

- **WHEN** project config selects reviewer harness `codex` with model
  `gpt-5-codex`
- **THEN** the review launcher starts the Codex reviewer with `gpt-5-codex`

#### Scenario: Reviewer cross-provider model is rejected

- **WHEN** project config selects reviewer harness `codex` with model
  `claude-opus-4-5`
- **THEN** saving the config fails before a review run can be created

### Requirement: Model management is surfaced consistently

The system SHALL expose model overrides and availability state through the
daemon API, `ao spawn`, project settings, and generated client schema without
requiring clients to duplicate provider-compatibility rules.

#### Scenario: CLI spawn accepts a model pin

- **WHEN** the operator runs `ao spawn --model gpt-5-codex`
- **THEN** the spawn request sends the model pin to the daemon and the created
  session read model includes the recorded model

#### Scenario: Settings preserves hidden config

- **WHEN** the project settings UI edits model-management fields
- **THEN** it saves those fields without dropping unrelated hidden project
  config such as env, symlinks, post-create commands, or intake settings

#### Scenario: API schema includes model-management fields

- **WHEN** the OpenAPI and TypeScript schemas are generated
- **THEN** project config, spawn requests, session read models, reviewer config,
  and model availability responses include the model-management fields clients
  need

