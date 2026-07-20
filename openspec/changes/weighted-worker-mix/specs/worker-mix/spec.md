## ADDED Requirements

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

The system SHALL reject an invalid worker mix at configuration time, reporting the offending bucket by its index. A non-empty mix SHALL be rejected when any bucket names an unknown harness, when any weight falls outside 1..100, when any model has leading or trailing whitespace, when a model is incompatible with its harness's model provider, when two buckets share the same `(harness, model)` identity, or when the weights do not sum to exactly 100.

#### Scenario: Weights must sum to 100

- **WHEN** a mix is saved whose bucket weights sum to a value other than 100
- **THEN** validation SHALL fail
- **AND** the error SHALL report the observed sum

#### Scenario: Unknown harness is rejected

- **WHEN** a mix bucket names a harness the system does not recognize
- **THEN** validation SHALL fail identifying that bucket's index

#### Scenario: Duplicate bucket is rejected

- **WHEN** two buckets in a mix share the same harness and model
- **THEN** validation SHALL fail identifying the later bucket's index

#### Scenario: Cross-provider model pin is rejected

- **WHEN** a bucket pins a model whose provider is incompatible with the bucket's harness provider
- **AND** neither provider is unknown
- **THEN** validation SHALL fail
- **AND** the error SHALL name the model and the expected provider

#### Scenario: Unknown provider on either side is permitted

- **WHEN** a bucket pins a model whose provider cannot be classified, or whose harness has no known provider
- **THEN** validation SHALL succeed for that bucket

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

Weighted selection SHALL apply only when a worker spawn request names no explicit harness. A spawn that pins a harness, a model, or both SHALL bypass selection entirely, SHALL NOT be counted against any bucket's share at selection time, and SHALL launch exactly what was pinned.

#### Scenario: Explicit pin bypasses the mix

- **WHEN** a spawn request names an explicit harness
- **THEN** the mix SHALL NOT be consulted
- **AND** the pinned harness SHALL be launched

#### Scenario: Pinned spawns do not consume mix share

- **WHEN** a pinned spawn and an unpinned spawn are both requested for a project with a configured mix
- **THEN** the unpinned spawn's selection SHALL be computed as though the pinned spawn had not occurred

#### Scenario: Mix-only project is spawnable

- **WHEN** a project configures a worker mix but no default worker agent
- **AND** an unpinned worker spawn is requested
- **THEN** the spawn SHALL succeed using a mix-selected bucket
- **AND** SHALL NOT be rejected for an unresolvable agent

### Requirement: Down buckets redistribute rather than substitute

When a bucket is marked down by candidate health, selection SHALL exclude it and redistribute its share across the remaining healthy buckets according to their relative weights. The system SHALL NOT silently launch a different harness or model in a down bucket's place. When every bucket in the mix is down, the spawn SHALL fail with a diagnosable error rather than selecting a down bucket.

#### Scenario: Down bucket's share redistributes

- **WHEN** the 30% bucket of a 60/30/10 mix is marked down
- **THEN** subsequent selections SHALL choose only among the 60 and 10 buckets
- **AND** SHALL converge on their 6:1 relative ratio

#### Scenario: All buckets down fails loudly

- **WHEN** every bucket in a configured mix is marked down and an unpinned spawn is requested
- **THEN** the spawn SHALL fail with an error naming the exhausted mix
- **AND** SHALL NOT fall back to a harness outside the mix
