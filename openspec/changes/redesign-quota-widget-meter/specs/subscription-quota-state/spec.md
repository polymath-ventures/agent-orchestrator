## MODIFIED Requirements

### Requirement: Quota state surfaces

AO SHALL surface quota-window state in the daemon API, `ao status`, and the
supervisor UI.

#### Scenario: UI renders quota windows as accessible meters

- **WHEN** the supervisor renders a quota window with a used percent
- **THEN** the quota panel shows the window name, used percent, a progressbar
  meter, and the full dated reset stamp
- **AND** the progressbar exposes accessible progressbar semantics with the
  harness and window in its accessible name
- **AND** warning and critical quota windows carry non-color text signals for
  severity, including remaining-percent text for critical windows
