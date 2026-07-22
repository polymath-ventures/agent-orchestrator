# spawn-concurrency-cap Specification

## Purpose

TBD - created by syncing change weighted-worker-mix. Update Purpose after archive.

## Requirements

### Requirement: Per-project live worker cap

A project SHALL support an optional maximum number of concurrently-live worker sessions. A session counts toward the cap while it is not terminated. When no cap is configured, the number of live workers SHALL be unbounded, preserving current behavior.

#### Scenario: Absent cap is unbounded

- **WHEN** a project configures no worker cap
- **THEN** worker spawns SHALL NOT be limited by this feature

#### Scenario: Terminated sessions free capacity

- **WHEN** a project is at its cap and one live worker terminates
- **THEN** the project SHALL be below the cap
- **AND** a subsequent spawn SHALL be admitted

#### Scenario: Cap counts workers only

- **WHEN** a project's live sessions include an orchestrator session
- **THEN** that orchestrator SHALL NOT count toward the worker cap

### Requirement: Spawns at the cap are refused distinguishably

When a spawn would exceed a project's configured cap, the system SHALL refuse it with an error distinguishable from a launch failure, and SHALL make that refusal before committing any durable session state or workspace. Cap admission SHALL be serialized with worker-mix census/selection and session seed-row creation so concurrent worker spawns cannot all observe stale capacity and collectively exceed the cap. A cap refusal SHALL NOT mark any candidate down, because being at capacity is not evidence that a harness or model is broken.

#### Scenario: Spawn at cap is refused

- **WHEN** a project is at its configured cap and a worker spawn is requested
- **THEN** the spawn SHALL be refused with a capacity error
- **AND** no session record or workspace SHALL be created

#### Scenario: Concurrent spawns cannot breach the cap

- **WHEN** multiple worker spawns are requested concurrently for a project with one free worker slot
- **THEN** at most one spawn SHALL create a session record
- **AND** the rest SHALL be refused with the capacity error before creating workspaces

#### Scenario: Cap refusal does not affect candidate health

- **WHEN** a spawn is refused because the project is at its cap
- **THEN** no candidate SHALL be marked down
- **AND** no candidate-down event SHALL be emitted

### Requirement: Tracker intake defers rather than fails at the cap

When tracker intake cannot spawn a worker for an issue because the project is at its cap, it SHALL treat the outcome as a deferral: the issue SHALL be left unclaimed so a subsequent poll retries it, and the project SHALL NOT be placed into the failure backoff that a genuine spawn failure triggers. Once a poll observes that the worker pool is full, later matching issues in the same pass SHALL be short-circuited without repeatedly calling Spawn until the next poll. A capacity condition is a normal steady state, not a project fault, and SHALL NOT be reported as an error.

#### Scenario: Capped intake retries on the next poll

- **WHEN** tracker intake attempts to spawn for an issue and the project is at its cap
- **THEN** the issue SHALL remain eligible for intake
- **AND** the next poll SHALL attempt it again

#### Scenario: Cap deferral does not trigger project backoff

- **WHEN** an intake spawn is deferred because of the cap
- **THEN** the project SHALL NOT enter failure backoff
- **AND** the poll interval for that project SHALL be unchanged

#### Scenario: Full pool short-circuits later issues in the same pass

- **WHEN** tracker intake observes a worker concurrency cap deferral for one issue
- **THEN** later matching issues in the same poll pass SHALL be deferred without calling Spawn

#### Scenario: Genuine spawn failure still backs off

- **WHEN** an intake spawn fails for a reason other than a deferral
- **THEN** the existing failure backoff behavior SHALL apply unchanged

#### Scenario: Deferred issue is claimed once capacity frees

- **WHEN** an issue was deferred at the cap and a live worker later terminates
- **THEN** a subsequent poll SHALL spawn a worker for that issue
