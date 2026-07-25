# session-usage-telemetry Specification

## Purpose

TBD - created by archiving change add-usage-telemetry-quota. Update Purpose after archive.

## Requirements

### Requirement: Harness-local usage extraction

AO SHALL extract real token usage deltas for supported subscription harnesses
from harness-local records at the agent turn boundary, and MUST treat extraction
as best effort so hook failures do not break an agent session.

#### Scenario: Claude Code transcript usage becomes a delta

- **WHEN** a Claude Code stop hook names a transcript containing assistant
  `message.usage` records
- **THEN** AO sums input, cache input, and output token buckets into cumulative
  usage and emits only the growth since the session cursor

#### Scenario: Codex rollout usage becomes a delta

- **WHEN** a Codex stop hook runs from a worktree with a matching
  rollout file containing `token_count` cumulative usage
- **THEN** AO reads the latest cumulative token count and emits only the growth
  since the session cursor

#### Scenario: Missing or unreadable usage source is harmless

- **WHEN** a supported harness has no readable usage source, malformed records,
  or no new cumulative growth
- **THEN** AO emits no usage delta and the hook still reports normal activity

#### Scenario: Cursor advances after successful delivery

- **WHEN** AO extracts a nonzero usage delta
- **THEN** AO advances the per-session cursor only after the activity request
  carrying that delta is accepted

### Requirement: Usage telemetry event production

AO SHALL emit an `ao.session.usage` telemetry event after lifecycle accepts an
activity signal that carries a valid usage delta.

#### Scenario: Accepted usage signal writes telemetry

- **WHEN** lifecycle accepts a stop activity signal with input, output, or total
  token deltas
- **THEN** AO writes an `ao.session.usage` telemetry event with session id,
  project id, harness, and allowlisted token fields

#### Scenario: Dollar cost is not fabricated

- **WHEN** a harness reports token usage but not monetary cost
- **THEN** AO leaves cost fields absent rather than deriving plan-specific
  dollars

### Requirement: Metrics expose usage rollups

AO SHALL expose token usage rollups through `/api/v1/metrics` grouped by project
and harness.

#### Scenario: Metrics include nonzero per-harness usage

- **WHEN** sessions for a project have emitted `ao.session.usage` telemetry
- **THEN** `/api/v1/metrics` includes nonzero input, output, and total token
  rollups for each harness with usage

#### Scenario: Metrics ignore invalid usage payloads

- **WHEN** telemetry payloads omit token fields or contain invalid token values
- **THEN** metrics omit those values from usage rollups instead of poisoning the
  aggregate
