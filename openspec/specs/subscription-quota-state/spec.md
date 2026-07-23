# subscription-quota-state Specification

## Purpose

TBD - created by archiving change add-usage-telemetry-quota. Update Purpose after archive.

## Requirements

### Requirement: Quota signal discovery and snapshots

AO SHALL discover and store quota-window snapshots per subscription
harness/account when a harness exposes quota state, and MUST record signal
quality for every snapshot. When a harness exposes more than one simultaneous
quota window for the same harness/account/model, AO SHALL preserve the window
identity separately from the model identity so each window can be displayed and
alerted independently.

#### Scenario: Exact quota signal is available

- **WHEN** a harness exposes a current subscription window with used and
  remaining quota
- **THEN** AO stores the account, harness, window name when known, window start,
  window end, used, remaining, limit, observed time, and signal quality `exact`

#### Scenario: Only estimated quota is available

- **WHEN** AO can derive quota state from known window boundaries and observed
  token usage but the harness does not expose an exact remaining value
- **THEN** AO stores the snapshot with signal quality `estimated` and includes
  the basis for the estimate

#### Scenario: Codex rollout rate limits become exact quota windows

- **WHEN** a Codex stop hook finds a matching rollout whose newest `token_count`
  event contains `rate_limits.primary` or `rate_limits.secondary`
- **THEN** AO stores each valid Codex rate-limit window as an exact quota
  snapshot with `used` set to the reported `used_percent`, `limit` set to 100,
  `remaining` set to `100 - used_percent`, and `windowEnd` set to the reported
  reset time
- **AND** AO records the window as `primary` or `secondary` without using the
  model field for that window identity

#### Scenario: No quota signal is available

- **WHEN** a harness has no discoverable quota endpoint, response header, CLI
  output, or local state file
- **THEN** AO reports quota signal quality `none` for that harness/account
  instead of fabricating remaining usage

#### Scenario: Claude Code no-signal is scoped

- **WHEN** Claude Code has no local machine-readable quota source
- **THEN** AO reports no-signal for Claude Code with a Claude-specific basis
  describing the local paths and CLI surfaces checked, and does not reuse that
  basis for Codex when Codex rollout rate limits are present

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

#### Scenario: Codex exact signal replaces Codex no-signal

- **WHEN** an exact or estimated Codex quota snapshot exists
- **THEN** metrics and status surfaces do not also report the old static Codex
  no-signal row for the same harness

#### Scenario: UI shows quota state

- **WHEN** the supervisor renders the quota panel
- **THEN** it shows remaining quota, used percent when available, window timing,
  and signal quality for each known subscription harness/account/window

#### Scenario: UI renders quota windows as accessible meters

- **WHEN** the supervisor renders a quota window with a used percent
- **THEN** the quota panel shows the window name, used percent, a progressbar
  meter, and the full dated reset stamp
- **AND** the progressbar exposes accessible progressbar semantics with the
  harness and window in its accessible name
- **AND** warning and critical quota windows carry non-color text signals for
  severity, including remaining-percent text for critical windows

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

#### Scenario: Codex low quota uses reported used percent

- **WHEN** a Codex quota snapshot reports `used_percent` at or above
  `100 - threshold`
- **THEN** AO fires the low-quota alert for that Codex window even though Codex
  reported the source value as used percent rather than remaining tokens

#### Scenario: No-signal quota does not alert as low

- **WHEN** a harness/account has quota signal quality `none`
- **THEN** AO does not emit a low-quota notification for that missing signal
