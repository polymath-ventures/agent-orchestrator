## MODIFIED Requirements

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
