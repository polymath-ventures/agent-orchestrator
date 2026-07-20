# subscription-quota-state Specification

## Purpose
TBD - created by archiving change add-usage-telemetry-quota. Update Purpose after archive.
## Requirements
### Requirement: Quota signal discovery and snapshots
AO SHALL discover and store quota-window snapshots per subscription
harness/account when a harness exposes quota state, and MUST record signal
quality for every snapshot.

#### Scenario: Exact quota signal is available
- **WHEN** a harness exposes a current subscription window with used and
  remaining quota
- **THEN** AO stores the account, harness, window start, window end, used,
  remaining, limit, observed time, and signal quality `exact`

#### Scenario: Only estimated quota is available
- **WHEN** AO can derive quota state from known window boundaries and observed
  token usage but the harness does not expose an exact remaining value
- **THEN** AO stores the snapshot with signal quality `estimated` and includes
  the basis for the estimate

#### Scenario: No quota signal is available
- **WHEN** a harness has no discoverable quota endpoint, response header, CLI
  output, or local state file
- **THEN** AO reports quota signal quality `none` for that harness/account
  instead of fabricating remaining usage

### Requirement: Quota state surfaces
AO SHALL surface quota-window state in the daemon API, `ao status`, and the
supervisor UI.

#### Scenario: API returns quota state
- **WHEN** a client requests metrics or status data
- **THEN** AO returns quota snapshots grouped by harness/account with window
  timing, used, remaining, limit, signal quality, and last observed time

#### Scenario: CLI shows honest quota state
- **WHEN** the operator runs `ao status`
- **THEN** the output names each subscription harness/account and shows exact,
  estimated, or no-signal quota state without implying unavailable precision

#### Scenario: UI shows quota state
- **WHEN** the supervisor renders the quota panel
- **THEN** it shows remaining quota, window timing, and signal quality for each
  known subscription harness/account

### Requirement: Low-quota notifications
AO SHALL emit a user-facing notification intent when a harness/account crosses a
configured low-quota threshold.

#### Scenario: Low quota threshold fires
- **WHEN** a quota snapshot reports remaining usage at or below the configured
  threshold
- **THEN** AO creates one unread low-quota notification naming the harness,
  account when known, window, and threshold

#### Scenario: Low quota notification dedupes per window
- **WHEN** later snapshots remain below the same threshold in the same window
- **THEN** AO does not create duplicate unread low-quota notifications for that
  harness/account/window

#### Scenario: No-signal quota does not alert as low
- **WHEN** a harness/account has quota signal quality `none`
- **THEN** AO does not emit a low-quota notification for that missing signal

