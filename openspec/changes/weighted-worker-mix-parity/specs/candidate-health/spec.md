## ADDED Requirements

### Requirement: Worker-mix spawn failures feed candidate health

For a worker-mix-selected spawn, every candidate-attributable launch failure SHALL be reported to candidate health for the exact selected candidate before retry/rollback work can obscure the caller context. Candidate-attributable failures include workspace preparation, missing agent binary, launch-command resolution for a missing binary, runtime creation, launch process liveness, and prompt delivery after start. Failures that are not evidence of a broken candidate, such as caller cancellation, project capacity, project pause, or prompt/config construction errors, SHALL NOT mark the candidate down.

#### Scenario: Workspace preparation failure marks selected candidate down

- **WHEN** workspace preparation fails for a worker-mix-selected spawn because the selected candidate cannot prepare its workspace
- **THEN** candidate health SHALL mark down that exact candidate

#### Scenario: Launch command failure marks missing binary down

- **WHEN** a worker-mix-selected spawn fails because the selected adapter cannot resolve its launch binary
- **THEN** candidate health SHALL mark down that exact candidate

#### Scenario: Runtime launch failure marks selected candidate down

- **WHEN** runtime creation or launch process liveness fails for a worker-mix-selected spawn
- **THEN** candidate health SHALL mark down that exact candidate

#### Scenario: After-start prompt delivery failure marks selected candidate down

- **WHEN** a worker-mix-selected spawn uses after-start prompt delivery and the delivery fails
- **THEN** candidate health SHALL mark down that exact candidate

#### Scenario: Non-candidate refusal does not mark down

- **WHEN** a worker spawn is refused because the project is paused, at capacity, or the caller context is canceled
- **THEN** candidate health SHALL NOT mark down any candidate
