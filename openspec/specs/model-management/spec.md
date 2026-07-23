# model-management Specification

## Purpose

Define harness-native model and effort selection, validation, availability,
health, persistence, and launch-safety behavior across AO's daemon, CLI, and UI.

## Requirements

### Requirement: Model pins are compatible with their harness

The system SHALL reject a known-provider model pin when it is configured or
requested for a known-provider harness from a different provider. Fugu model
identifiers SHALL classify as the Fugu provider before any generic Codex/OpenAI
fragment match. Unknown models or harnesses SHALL remain permissive so novel
providers can still be launched before AO learns their catalog, and the
installed harness SHALL remain the final authority at execution time.

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

#### Scenario: Fugu classification wins over generic Codex fragments

- **WHEN** a model identifier belongs to the installed Codex-Fugu catalog
- **THEN** AO classifies it as Fugu before applying any generic `codex` fragment rule
- **AND** the compatibility guard rejects it for a non-Fugu provider harness

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

The system SHALL expose candidate and configured models through one authoritative
model service with reason codes that distinguish not-yet-probed, unavailable
probe capability, known unreachable, recovered, and no-capability states. The
service SHALL bound request caching, per-pin verdict lifetime, concurrency, and
memory growth. Spawning SHALL consult only fresh cached verdicts and SHALL do no
model discovery or probe network I/O on the spawn path.

#### Scenario: Availability has not been probed

- **WHEN** a configured model pin has no cached probe result
- **THEN** model availability reports reason code `not-probed`

#### Scenario: Harness cannot probe models

- **WHEN** the selected harness exposes no model validation capability
- **THEN** model availability reports reason code `no-capability`
- **AND** save and spawn remain fail-open

#### Scenario: Probe transport is unavailable

- **WHEN** a model probe cannot complete because of auth, rate limit, timeout, provider failure, signal termination, or missing verdict metadata
- **THEN** model availability reports reason code `probe-unavailable`
- **AND** config save and the spawn gate remain fail-open for that model pin

#### Scenario: Cached unreachable model blocks without probing on spawn

- **WHEN** a fresh cached verdict definitively says a configured model is unreachable
- **THEN** spawning that pinned model performs no live probe
- **AND** AO rejects the spawn before creating session or worktree state

#### Scenario: Missing or stale verdict fails open

- **WHEN** the spawn gate has no verdict or its verdict is older than the configured TTL
- **THEN** the spawn proceeds with a loud advisory warning
- **AND** no provider call is made on the spawn path

#### Scenario: Background revalidation preserves real state

- **WHEN** revalidation returns an unknown verdict after a prior reachable or unreachable verdict
- **THEN** the unknown result does not overwrite the prior real state
- **AND** no false transition notification is emitted

#### Scenario: Background revalidation emits first and later transitions

- **WHEN** a configured pin is first observed unreachable, later changes from reachable to unreachable, or recovers from unreachable to reachable
- **THEN** AO emits one typed transition intent carrying project, harness, model, and scope

#### Scenario: Cache remains bounded under config churn

- **WHEN** model pins are repeatedly added and removed
- **THEN** expired and unconfigured verdicts are evicted before current definitive rejections
- **AND** the cache does not exceed its configured entry cap

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

### Requirement: Harness discovery and invocation remain native

The system SHALL store and invoke harness-native model and effort identifiers.
Discovery policy SHALL be harness-specific, and AO SHALL not invent a universal
effort vocabulary or issue a paid model prompt solely to validate saved
configuration.

#### Scenario: Claude uses maintained aliases without a paid save probe

- **WHEN** a user saves a Claude Code default using `fable`, `opus`, `sonnet`, or `haiku`
- **THEN** AO validates it against maintained alias and installed-capability metadata
- **AND** AO does not issue a Claude prompt merely to validate the save

#### Scenario: Claude effort is passed per process

- **WHEN** a Claude Code launch has an explicit supported effort
- **THEN** AO passes it through `CLAUDE_CODE_EFFORT_LEVEL`
- **AND** uses `--effort` only when the installed CLI reports that flag as supported

#### Scenario: OpenCode catalog and variants are queried

- **WHEN** OpenCode model selection is refreshed
- **THEN** AO queries `opencode models --refresh --verbose`
- **AND** stores provider/model IDs and declared variants without inventing an effort

#### Scenario: Codex-Fugu catalog is installed data

- **WHEN** `~/.codex/fugu.json` is readable
- **THEN** AO derives model slugs and supported reasoning levels from that file
- **AND** normalizes `max` to `xhigh`

#### Scenario: Long-running Fugu execution is not a failure

- **WHEN** a Codex-Fugu request continues producing or awaiting valid work beyond an ordinary Codex duration
- **THEN** AO keeps it alive until explicit cancellation, process error, or the configured stream timeout

### Requirement: Model probes are hermetic and provider-aware

The system SHALL isolate validation probes from operator hooks, MCP servers,
session persistence, writable sandboxes, and leaked descendant processes. A
probe SHALL classify a model unreachable only from an explicit provider
rejection status.

#### Scenario: Codex probe can run outside a repository

- **WHEN** AO validates a Codex or Fugu model from a non-repository working directory
- **THEN** it invokes `codex exec` with skip-repo-check, read-only sandbox, and ephemeral-session flags
- **AND** it does not pass TUI-only approval flags

#### Scenario: Claude probe is hermetic

- **WHEN** AO runs a Claude validation probe
- **THEN** the prompt is delivered on stdin and the JSON result envelope determines the verdict
- **AND** setting sources, MCP loading, and session persistence are disabled

#### Scenario: Only provider model rejection is definitive

- **WHEN** a probe reports HTTP 400, 404, or 422 for the selected model
- **THEN** AO records `unreachable`
- **AND** 401, 403, 408, 429, 5xx, signal termination, OOM, timeout, and missing status remain `probe-unavailable`

#### Scenario: Probe timeout kills the process tree

- **WHEN** a probe exceeds its independent 45-second deadline
- **THEN** AO terminates the probe process group and waits for descendant pipes to close
- **AND** the refresh loop proceeds without leaked processes

### Requirement: Agent installation and authentication health is observable

The system SHALL expose cached installed/authenticated health for configured
harnesses and SHALL monitor transitions at an environment-configurable cadence.
Unknown probe results SHALL remain advisory.

#### Scenario: Health endpoint distinguishes actionable states

- **WHEN** a configured harness binary is missing or its authentication expires
- **THEN** `/agents/health` reports `missing` or `unauthorized` with a remedy
- **AND** AO records the transition once through its read-side callback and log surface
- **AND** no session-scoped notification is persisted

#### Scenario: Unknown health does not page

- **WHEN** install or authentication health cannot be determined
- **THEN** the endpoint reports `unknown`
- **AND** AO records no actionable unhealthy transition

#### Scenario: Health cadence can be disabled

- **WHEN** `AO_AGENT_HEALTH_INTERVAL` is set to zero
- **THEN** scheduled health checks are disabled without affecting explicit reads or spawns

### Requirement: Model management preserves session launch liveness

The system SHALL determine spawned-agent liveness from the process tree before
delivering title or prompt data and SHALL not infer death from the tmux pane's
launcher-shell command name.

#### Scenario: Launcher shell remains while agent is healthy

- **WHEN** tmux reports `sh` as the pane command while the agent descendant is alive
- **THEN** AO treats the session as alive and does not kill or respawn it

#### Scenario: Immediate exit is detected before prompt delivery

- **WHEN** the agent process exits immediately after launch
- **THEN** AO detects the dead process tree before typing title or prompt text into the keep-alive shell
