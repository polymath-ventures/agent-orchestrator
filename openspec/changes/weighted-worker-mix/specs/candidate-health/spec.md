## ADDED Requirements

### Requirement: Candidate identity
A candidate SHALL be identified by the combination of its selection surface, harness, model, provider, account, and bot. All axes SHALL be trimmed of surrounding whitespace before comparison. Two candidates differing on any populated axis SHALL be distinct health subjects; health state SHALL NOT be shared between them. This distinctness is the basis of the no-silent-substitution guarantee: marking one candidate down SHALL never imply anything about another.

#### Scenario: Candidates differing by model are distinct
- **WHEN** two candidates share a harness but name different models
- **AND** one is marked down
- **THEN** the other SHALL remain healthy

#### Scenario: Whitespace does not create a distinct candidate
- **WHEN** a candidate is referenced with surrounding whitespace on any axis
- **THEN** it SHALL resolve to the same health subject as its trimmed form

### Requirement: Marking a candidate down
The system SHALL mark a candidate down when a spawn attempt fails in a way attributable to that candidate, recording the failure reason and the time of the transition. A mark-down call carrying no error SHALL be a no-op, so callers may invoke it unconditionally on a failure path. Every mark-down SHALL debit the candidate's skip count.

#### Scenario: Failed launch marks the candidate down
- **WHEN** a spawn attempt for a candidate fails with an error
- **THEN** that candidate SHALL be recorded as down with the error's message as the reason

#### Scenario: Nil error is a no-op
- **WHEN** mark-down is invoked with no error
- **THEN** the candidate's health state SHALL be unchanged

### Requirement: Alert on transition only
The system SHALL emit a candidate-down telemetry event only on the transition from healthy to down. A repeated failure of an already-down candidate SHALL debit the skip count and log, but SHALL NOT emit a further down event. This keeps one alert per outage rather than one per retry.

#### Scenario: First failure alerts
- **WHEN** a healthy candidate is marked down
- **THEN** exactly one candidate-down event SHALL be emitted at warning level

#### Scenario: Repeat failure does not re-alert
- **WHEN** an already-down candidate is marked down again
- **THEN** no additional candidate-down event SHALL be emitted
- **AND** the skip count SHALL still increase

### Requirement: Caller-context cancellation is not a candidate fault
When a spawn attempt is abandoned because the *caller's* context was canceled or exceeded its deadline, the system SHALL NOT mark the candidate down, SHALL NOT debit a skip, and SHALL NOT alert. Attribution SHALL be determined by the state of the caller's context at the time of the failure, and SHALL NOT be inferred from the identity of the returned error. An error that wraps a cancellation or deadline-exceeded value while the caller's context is still active SHALL be treated as a genuine candidate fault, because a candidate's own startup probe may legitimately fail that way. A mark-down invoked without any attempt context SHALL attribute the fault to the candidate.

#### Scenario: Canceled caller context is a no-op
- **WHEN** an attempt fails while the caller's context is already canceled
- **AND** the returned error wraps a cancellation value
- **THEN** the candidate SHALL remain healthy
- **AND** no event SHALL be emitted and no skip SHALL be debited

#### Scenario: Candidate-side deadline with a live caller is a fault
- **WHEN** an attempt fails with an error wrapping a deadline-exceeded value
- **AND** the caller's context is still active
- **THEN** the candidate SHALL be marked down
- **AND** exactly one candidate-down event SHALL be emitted
- **AND** one skip SHALL be debited

#### Scenario: Absent attempt context attributes to the candidate
- **WHEN** a failure is reported with no attempt context supplied
- **THEN** the candidate SHALL be marked down

### Requirement: Skipping a down candidate
When selection encounters a candidate that is currently down, the system SHALL report that it is down so the caller can refuse the selection, SHALL debit the skip count, and SHALL log the skip. Recording a skip SHALL NOT emit a telemetry event, so a persistent outage does not flood the sink. Querying a healthy candidate SHALL report healthy and SHALL NOT mutate any state.

#### Scenario: Down candidate is reported and debited
- **WHEN** selection queries a down candidate
- **THEN** the system SHALL report it as down
- **AND** SHALL increase its skip count
- **AND** SHALL NOT emit a telemetry event

#### Scenario: Healthy candidate query is inert
- **WHEN** selection queries a healthy candidate
- **THEN** the system SHALL report it healthy
- **AND** SHALL NOT change its skip count

### Requirement: Explicit recovery only
A candidate SHALL return to healthy only when explicitly recovered following a successful attempt on that exact candidate. Recovery SHALL clear both the down state and the accumulated skip count, and SHALL emit a candidate-recovered event at informational level. Recovering a candidate that was not down SHALL be silent — no log, no event. The system SHALL NOT implement a time-to-live, a backoff timer, a half-open probe, or any other automatic recovery: the recorded transition time is observational only and SHALL NOT drive any decision. Health state is in-memory and SHALL NOT survive a daemon restart.

#### Scenario: Successful spawn recovers the candidate
- **WHEN** a spawn succeeds for a candidate that was down
- **THEN** that candidate SHALL be marked healthy
- **AND** its skip count SHALL be reset
- **AND** exactly one candidate-recovered event SHALL be emitted

#### Scenario: Down state does not expire
- **WHEN** a candidate has been down for an arbitrary period with no successful attempt
- **THEN** it SHALL remain down
- **AND** SHALL continue to be skipped by selection

#### Scenario: Recovering a healthy candidate is silent
- **WHEN** recovery is invoked for a candidate that is not down
- **THEN** no event SHALL be emitted

#### Scenario: Restart clears health state
- **WHEN** the daemon restarts
- **THEN** all candidates SHALL be healthy

### Requirement: Health telemetry payload
Candidate health events SHALL carry the emitting component, the selection surface, and a rendered candidate identity. The harness, model, provider, account, bot, reason, and skip count SHALL be included only when populated. The surface SHALL be carried in the payload rather than encoded in the event name, so a single subscription observes every selection surface. Event emission SHALL be decoupled from the failing attempt's context so an alert is not lost when that attempt is canceled.

#### Scenario: Down event carries identifying payload
- **WHEN** a candidate-down event is emitted
- **THEN** its payload SHALL include the component, surface, and rendered candidate
- **AND** SHALL include the failure reason

#### Scenario: Unpopulated axes are omitted
- **WHEN** a candidate has no account or bot set
- **THEN** those keys SHALL be absent from the payload rather than present and empty

#### Scenario: Alert survives a canceled attempt
- **WHEN** a candidate-down event is emitted during an attempt whose context is canceled
- **THEN** the event SHALL still reach the telemetry sink

### Requirement: Concurrent health access
Health state SHALL be safe for concurrent use by multiple spawning goroutines. Logging and telemetry emission SHALL NOT occur while the internal lock is held, so a slow sink cannot block selection.

#### Scenario: Concurrent mutation is safe
- **WHEN** many goroutines concurrently mark down, skip, recover, and query candidates
- **THEN** no data race SHALL occur
- **AND** the resulting state SHALL be internally consistent

#### Scenario: Slow sink does not block selection
- **WHEN** the telemetry sink is slow to accept an event
- **THEN** concurrent health queries SHALL NOT be blocked by it
