# worker-mix Specification

## Purpose

TBD - created by syncing change weighted-worker-mix. Update Purpose after archive.

## Requirements

### Requirement: Worker mix configuration

A project SHALL support an optional worker mix: an ordered list of buckets, each naming an agent harness, an optional model, and an integer weight. A bucket is identified by the `(harness, model)` pair. When no worker mix is configured, spawn behavior SHALL be unchanged from the existing harness-resolution rules.

#### Scenario: Absent mix leaves behavior unchanged

- **WHEN** a project has no worker mix configured and an unpinned worker spawn is requested
- **THEN** the harness SHALL be resolved by the existing role/project configuration rules
- **AND** no mix selection SHALL occur

#### Scenario: Empty mix is valid and inert

- **WHEN** a worker mix is configured with zero buckets
- **THEN** validation SHALL succeed
- **AND** the mix SHALL be treated as absent for selection purposes

### Requirement: Worker mix validation

The system SHALL reject an invalid worker mix at configuration time, reporting the offending bucket by its index. A non-empty mix SHALL be rejected when any bucket names an unknown harness, when any weight falls outside 1..100, when any model has leading or trailing whitespace, when any explicit model or effort selection is definitively unreachable for the bucket's harness, when two resolved buckets share the same `(harness, model, effort)` identity, or when the weights do not sum to exactly 100. If model availability cannot provide a definitive verdict, the system SHALL accept the bucket and emit an operator-visible warning rather than making configuration read-only.

#### Scenario: Weights must sum to 100

- **WHEN** a mix is saved whose bucket weights sum to a value other than 100
- **THEN** validation SHALL fail
- **AND** the error SHALL report the observed sum

#### Scenario: Unknown harness is rejected

- **WHEN** a mix bucket names a harness the system does not recognize
- **THEN** validation SHALL fail identifying that bucket's index

#### Scenario: Duplicate resolved bucket is rejected

- **WHEN** two buckets in a mix resolve to the same harness, model, and effort
- **THEN** validation SHALL fail identifying the later bucket's index

#### Scenario: Definitively unreachable model selection is rejected

- **WHEN** a bucket pins a model or effort selection that the current model availability engine definitively rejects for the bucket's harness
- **THEN** validation SHALL fail
- **AND** the error SHALL name the bucket index and rejected selection

#### Scenario: Unknown model verdict is permitted

- **WHEN** a bucket pins a model or effort selection whose availability cannot be verified
- **THEN** validation SHALL succeed for that bucket
- **AND** an operator-visible warning SHALL be emitted

#### Scenario: Weight out of range is rejected

- **WHEN** a bucket carries a weight below 1 or above 100
- **THEN** validation SHALL fail identifying that bucket's index and the observed weight

### Requirement: Deterministic weighted selection

Given a validated non-empty mix and a census of currently-live workers per bucket, selection SHALL choose the bucket maximizing `weight / (live + 1)` — D'Hondt highest-averages apportionment. The comparison SHALL be performed in integer arithmetic so that no floating-point rounding can affect the outcome. Selection SHALL be a pure function of the mix and the census: it SHALL NOT consult a clock, a random source, or any retained state, and SHALL therefore return the same bucket for the same inputs. A bucket absent from the census SHALL count as zero live workers.

#### Scenario: Distribution converges on configured ratio

- **WHEN** a mix of 60/30/10 selects repeatedly, each selection incrementing the chosen bucket's live count
- **THEN** after 10 selections the buckets SHALL hold exactly 6, 3, and 1 workers respectively
- **AND** the sequence SHALL be reproducible across runs

#### Scenario: Ties resolve to the earliest bucket

- **WHEN** two buckets yield an equal `weight / (live + 1)` value
- **THEN** the bucket appearing earlier in the configured mix SHALL be selected

#### Scenario: Selection is stateless across calls

- **WHEN** selection is invoked twice with an identical mix and an identical census
- **THEN** both invocations SHALL return the same bucket

#### Scenario: Empty mix selects nothing

- **WHEN** selection is invoked with a mix containing no buckets
- **THEN** it SHALL report that no bucket was selected rather than returning an arbitrary one

### Requirement: Mix applies only to unpinned worker spawns

Weighted selection SHALL apply when a worker spawn request names no explicit harness. A spawn that pins a harness SHALL bypass selection entirely and launch that harness. A worker spawn that supplies a model but no harness SHALL still use the worker mix, when configured, to select the harness; the explicit model SHALL override the selected bucket's model for launch and candidate identity. When a spawn bypasses the mix by explicit harness, it SHALL launch exactly what was pinned.

#### Scenario: Explicit harness bypasses the mix

- **WHEN** a spawn request names an explicit harness
- **THEN** the mix SHALL NOT be consulted
- **AND** the pinned harness SHALL be launched

#### Scenario: Model-only spawn uses mix-selected harness

- **WHEN** a worker spawn request supplies a model but no harness
- **AND** the project configures a worker mix
- **THEN** the daemon SHALL select a harness from the worker mix
- **AND** the supplied model SHALL be used for the launched session

#### Scenario: Mix-only project is spawnable

- **WHEN** a project configures a worker mix but no default worker agent
- **AND** an unpinned worker spawn is requested
- **THEN** the spawn SHALL succeed using a mix-selected bucket
- **AND** SHALL NOT be rejected for an unresolvable agent

### Requirement: Down buckets redistribute rather than substitute

When a bucket is marked down by candidate health, the system SHALL preserve that bucket's share as skip debit in the selection census rather than silently reallocating it to healthy buckets. Selection SHALL continue to evaluate the configured mix against actual live worker counts plus skip debit. If the selected bucket is down, the spawn SHALL fail with a diagnosable worker-mix bucket-down error and debit that skip. When every bucket in the mix is down or skipped enough to be unselectable, the spawn SHALL fail with a diagnosable exhausted-mix error rather than selecting a substitute outside the mix.

#### Scenario: Down bucket share is debit-preserved

- **WHEN** the 30% bucket of a 60/30/10 mix is marked down
- **THEN** subsequent selection SHALL account for skipped attempts against that bucket in the census
- **AND** SHALL NOT silently redistribute that bucket's share as though it were absent

#### Scenario: Selected down bucket fails loudly

- **WHEN** D'Hondt selection chooses a bucket currently marked down
- **THEN** the spawn SHALL fail with a worker-mix bucket-down error
- **AND** the candidate skip count SHALL increase
- **AND** no substitute harness or model SHALL be launched

#### Scenario: All buckets down fails loudly

- **WHEN** every bucket in a configured mix is down and an unpinned spawn is requested
- **THEN** the spawn SHALL fail with an error naming the exhausted mix
- **AND** SHALL NOT fall back to a harness outside the mix

### Requirement: Worker mix census uses actual live bucket occupancy

The worker-mix census SHALL count every non-terminated worker session for the project by the persisted launch tuple `(harness, model, effort)`, regardless of whether the session was mix-selected or explicitly pinned. The census SHALL be computed from stored session state, not re-derived from current project configuration.

#### Scenario: Pinned worker in a configured bucket consumes share

- **WHEN** a live pinned worker has the same persisted harness, model, and effort as a configured mix bucket
- **THEN** the next unpinned worker selection SHALL count that worker in the bucket's live occupancy

#### Scenario: Config changes do not rewrite census identity

- **WHEN** a worker was launched before the project worker mix changed
- **THEN** the census SHALL use the worker's persisted launch tuple
- **AND** SHALL NOT re-derive the bucket from the current config

### Requirement: Worker mix editor blocks row-level invalid saves

The settings UI SHALL block saving a non-empty worker mix when any row has an empty agent, a non-integer weight, or a weight outside 1..100. The UI SHALL continue to block bad totals and SHALL continue to derive agent and model controls from the live agent catalog and model availability data.

#### Scenario: Empty bucket agent blocks save

- **WHEN** the worker mix editor contains a bucket with no selected agent
- **THEN** Save changes SHALL be blocked
- **AND** the UI SHALL show a worker-mix validation message

#### Scenario: Invalid bucket weight blocks save

- **WHEN** the worker mix editor contains a bucket with a weight below 1, above 100, or non-integer text
- **THEN** Save changes SHALL be blocked
- **AND** the invalid config SHALL NOT be submitted

#### Scenario: Dynamic catalog remains authoritative

- **WHEN** agent catalog or model availability data includes a registered harness and its models
- **THEN** the worker mix editor SHALL offer those options without hardcoded harness/model literals
