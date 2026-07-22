# slack-notifications Specification

## Purpose

One-way mirroring of AO's existing notifications to a Slack channel via a read-only CLI sidecar
(`ao notify slack`). The sidecar consumes the daemon's existing notification stream and posts each
notification the desktop bell shows to a Slack incoming webhook. Delivery is strictly outbound: no
inbound Slack surface, no filtering beyond on/off, and no daemon changes.
## Requirements
### Requirement: Slack delivery is opt-in via configuration alone

The system SHALL deliver notifications to Slack only when a Slack incoming webhook URL is
configured. When no webhook URL is configured, the Slack command SHALL fail as CLI misuse and no
other AO behavior SHALL change.

#### Scenario: Webhook URL configured

- **WHEN** the operator runs the Slack notify command with a webhook URL supplied
- **THEN** the command starts, connects to the daemon, and begins delivering notifications

#### Scenario: Webhook URL missing

- **WHEN** the operator runs the Slack notify command with no webhook URL supplied
- **THEN** the command exits with the CLI usage exit code and a message naming the missing
  configuration
- **AND** no request is made to the daemon and no message is sent to Slack

#### Scenario: Slack not configured at all

- **WHEN** the Slack notify command is never run
- **THEN** the daemon, the database, the HTTP API, and the desktop UI behave exactly as they do
  without this change

### Requirement: Every notification the UI bell shows is mirrored to Slack

The system SHALL deliver to Slack every notification the daemon publishes on its notification
stream, for every notification type the desktop UI bell displays, with no type filtering,
suppression, or routing policy beyond first-run backlog seeding and delivered-ID deduplication.

#### Scenario: A notification is published while connected

- **WHEN** the daemon publishes a notification of any type on the notification stream
- **THEN** the command posts exactly one corresponding message to the configured Slack webhook

#### Scenario: All current bell notification types are supported

- **WHEN** the daemon publishes a notification of type `needs_input`, `ready_to_merge`,
  `pr_merged`, `pr_closed_unmerged`, `low_quota`, `model_unreachable`, `model_recovered`, or
  `prime_restart_capped`
- **THEN** each one is rendered and delivered, and no type is silently discarded

#### Scenario: An unrecognized notification type arrives

- **WHEN** the daemon publishes a notification whose type the renderer does not specifically know
- **THEN** the command still delivers a message using the notification's own title and body rather
  than dropping it

### Requirement: Notifications render as a single readable Slack line

The system SHALL render each notification as one concise Slack message carrying the same
information the UI bell presents, including the notification's title and body, and the pull request
URL when the notification carries one. Each current notification type SHALL have an intentional,
distinct type rendering; only unknown future types MAY use the generic fallback icon.

Reference behavior: informational icons in `ao-slack-notifier.mjs:166-172,379-388`; attention icons
in `attention-core.mjs:32-45`.

#### Scenario: Notification carrying a pull request URL

- **WHEN** a notification with a pull request URL is delivered
- **THEN** the Slack message contains the notification title, its body, and a link to that pull
  request

#### Scenario: Notification without a pull request URL

- **WHEN** a notification with no pull request URL is delivered
- **THEN** the Slack message contains the notification title and body and omits any link, without
  error

#### Scenario: Each current notification type renders intentionally

- **WHEN** each of the eight current `domain.NotificationType` values is rendered
- **THEN** none of them uses the generic unknown-type fallback

#### Scenario: Unknown future type

- **WHEN** an unrecognized future notification type is rendered
- **THEN** it uses the generic icon while preserving the notification's escaped title and body

### Requirement: Missed notifications are reconciled on connect

The system SHALL reconcile against the daemon's complete current unread notifications each time it
establishes or re-establishes a stream connection, SHALL additionally reconcile periodically while
running, and SHALL deliver each notification at most once across live delivery, reconciliation, and
process restarts, identified by persisted notification ID. This is required because the daemon's
notification stream is in-process and best-effort with no replay, so a notification whose live
delivery failed on a stream that never drops would otherwise never be retried.

#### Scenario: Notifications published while disconnected

- **WHEN** the command reconnects after a disconnection during which notifications were published
- **THEN** it lists all of the daemon's current unread notifications and delivers those absent from
  the persisted ledger

#### Scenario: A live delivery failed but the stream stays connected

- **WHEN** a notification's live delivery fails and the stream connection does not drop
- **THEN** a later periodic reconciliation re-lists it and delivers it, without requiring a
  reconnection

#### Scenario: A notification appears in both reconciliation and the live stream

- **WHEN** the same notification ID is seen both in the reconciliation listing and on the stream
- **THEN** it is delivered to Slack exactly once

#### Scenario: Nothing was missed

- **WHEN** the command reconnects and every unread notification is already in the persisted ledger
- **THEN** no Slack notification message is sent

#### Scenario: Stream has no replay cursor

- **WHEN** the in-process notification stream reconnects without `Last-Event-ID` support
- **THEN** complete unread reconciliation plus the persisted delivered-ID ledger provides recovery
  without replaying already-delivered notifications

### Requirement: Delivery failures do not stop the channel

The system SHALL keep running across transient daemon and Slack failures. A failure to reach the
daemon at startup SHALL be reported as a runtime failure; failures encountered while running SHALL
be retried rather than terminating delivery.

#### Scenario: Daemon unreachable at startup

- **WHEN** the command starts and cannot reach the daemon
- **THEN** it exits with the runtime failure exit code and an error identifying the daemon as
  unreachable

#### Scenario: Stream drops while running

- **WHEN** an established notification stream connection drops
- **THEN** the command reconnects and reconciles missed notifications rather than exiting

#### Scenario: Slack rejects a message

- **WHEN** a Slack webhook post fails
- **THEN** the command reports the failure, continues consuming the stream, and does not exit

#### Scenario: Operator stops the command

- **WHEN** the command receives an interrupt or termination signal
- **THEN** it shuts down cleanly without a non-zero runtime failure

### Requirement: The Slack surface is strictly one-way

The system SHALL NOT accept, listen for, or act on any input originating from Slack.

#### Scenario: No inbound listener exists

- **WHEN** the Slack notify command is running
- **THEN** it exposes no network listener, no Slack event subscription, no slash-command handler,
  and no interactivity endpoint

#### Scenario: Removing the configuration removes the surface

- **WHEN** the operator stops the command and removes the webhook configuration
- **THEN** no Slack-related state, listener, scheduled work, or stored data remains anywhere in AO

### Requirement: Slack delivery state survives restarts without flooding

The system SHALL persist the Slack channel's delivered-notification ledger under AO's configured
data directory using atomic replacement, SHALL create the parent directory when needed, and SHALL
advance the durable ledger only after Slack accepts the notification and the persist write succeeds.
On the first run with no initialized ledger, the system SHALL seed the current unread snapshot as
delivered without posting those historical notifications.

Reference behavior: `ao-slack-notifier.mjs:74,610-646,717,725-745,789-790,808`.

#### Scenario: First run against an existing unread backlog

- **WHEN** the Slack command starts with no initialized delivery ledger and unread notifications
  already exist
- **THEN** it records the unread snapshot as delivered without sending those historical
  notifications to Slack
- **AND** it persists the initialized ledger atomically

#### Scenario: Restart after successful deliveries

- **WHEN** the Slack command restarts after previously delivering notifications
- **THEN** it restores the delivered IDs from the durable ledger
- **AND** it does not re-post those notifications even if they remain unread in AO

#### Scenario: Slack accepts a notification but ledger persistence fails

- **WHEN** Slack accepts a notification and the subsequent ledger write fails
- **THEN** the command reports the persistence failure
- **AND** it does not mark the notification delivered in memory
- **AND** a later reconciliation may safely re-offer the notification rather than silently losing it

#### Scenario: AO data directory is overridden

- **WHEN** the operator configures AO's data directory override
- **THEN** the default Slack ledger path is derived under that configured directory rather than
  Electron app-data or a hard-coded deployment path

### Requirement: Reconciliation drains the full unread backlog

The system SHALL page through all current unread notifications using a stable `(createdAt, id)`
keyset cursor until the unread listing is exhausted, even when the backlog exceeds the daemon's
single-page limit. The daemon list endpoint SHALL accept optional `before` and `beforeId` cursor
parameters while preserving the existing response for callers that omit them.

Reference behavior: `ao-slack-notifier.mjs:746-780`.

#### Scenario: More than 100 unread notifications

- **WHEN** reconciliation begins with more than 100 unread notifications
- **THEN** the command requests successive cursor pages until all unread rows have been considered
- **AND** no unread row is silently unrecoverable because it fell outside the first page

#### Scenario: Notifications share a creation timestamp

- **WHEN** multiple unread notifications have the same `createdAt` value
- **THEN** the cursor uses notification ID as the stable tiebreak
- **AND** no tied row is skipped or returned twice between adjacent pages

#### Scenario: Existing API caller omits the cursor

- **WHEN** a caller requests unread notifications with only `status` and `limit`
- **THEN** the endpoint returns the newest unread notifications exactly as before

### Requirement: Attention-class notifications mention the configured Slack member

The system SHALL prepend a Slack member mention to notifications of type `needs_input`,
`prime_restart_capped`, and `model_unreachable` when a member ID is configured. It SHALL NOT mention
the member for routine notification types, and a missing member ID SHALL disable mentions without
disabling delivery. Mentioning is outbound presentation only and SHALL NOT create an inbound Slack
surface.

Reference behavior: `ao-slack-notifier.mjs:154-165,891-915` and
`attention-core.mjs:133-143`. The reference used projection-kind names; this requirement maps that
behavior to this fork's current `domain.NotificationType` values.

#### Scenario: Attention-class notification with member configured

- **WHEN** a `needs_input`, `prime_restart_capped`, or `model_unreachable` notification is delivered
  and a Slack member ID is configured
- **THEN** its one-line message begins with the configured `<@member>` mention

#### Scenario: Routine notification with member configured

- **WHEN** a routine notification such as `pr_merged` is delivered and a Slack member ID is
  configured
- **THEN** the message is delivered without mentioning that member

#### Scenario: No member configured

- **WHEN** an attention-class notification is delivered without a configured Slack member ID
- **THEN** the notification is still delivered normally without a mention

### Requirement: Established daemon outages produce one latched Slack alert

After at least one successful stream connection, the system SHALL count consecutive daemon stream
connection failures. On the third consecutive failure it SHALL post one operator-visible
daemon-unreachable alert, SHALL suppress repeats while the outage remains latched, and SHALL reset
the counter and latch on the next successful connection. A daemon that is unreachable on the first
startup attempt SHALL continue to fail the command as a runtime error instead.

Reference behavior: `ao-slack-notifier.mjs:1017-1045,1179-1180,1211`.

#### Scenario: Daemon dies after a successful connection

- **WHEN** the notification stream has connected successfully and then three consecutive reconnect
  attempts fail
- **THEN** the command posts one daemon-unreachable Slack alert

#### Scenario: Outage continues

- **WHEN** reconnect attempts continue failing after the outage alert was posted
- **THEN** no duplicate outage alert is posted for that same outage

#### Scenario: Daemon recovers then fails again

- **WHEN** the daemon reconnects successfully after an outage and later reaches three consecutive
  failures again
- **THEN** the latch has reset and a new outage alert is posted

#### Scenario: Daemon unreachable on first startup

- **WHEN** the first stream connection cannot reach the daemon
- **THEN** the command exits with the runtime failure exit code instead of pretending a long-lived
  channel is running

### Requirement: Idle notification streams remain alive and cancellation-aware

The daemon notification SSE endpoint SHALL emit periodic comment heartbeats so idle browser or
EventSource clients are not severed by common five-minute body timeouts. The Slack consumer SHALL
ignore SSE comment frames, and all reconnect waits, fetches, Slack posts, pagination loops, and
shutdown paths SHALL remain responsive to context cancellation.

Reference incident: the ~five-minute undici `bodyTimeout=300000` failure and server-side keepalive
fix (#86); current consumer comment handling is `backend/internal/cli/slack.go:509-510`.

#### Scenario: Notification stream is idle

- **WHEN** no notification is published for the heartbeat interval
- **THEN** the daemon emits an SSE comment and flushes it without creating a notification

#### Scenario: Slack consumer receives a heartbeat

- **WHEN** the Slack consumer receives an SSE comment frame
- **THEN** it ignores the frame and keeps consuming the same stream

#### Scenario: Process terminates during network or backoff work

- **WHEN** the command receives SIGTERM while sleeping, fetching, posting, or paging
- **THEN** the in-flight operation is cancelled and the command exits cleanly

