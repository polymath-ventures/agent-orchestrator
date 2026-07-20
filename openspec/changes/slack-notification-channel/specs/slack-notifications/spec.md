## ADDED Requirements

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
suppression, or routing policy.

#### Scenario: A notification is published while connected
- **WHEN** the daemon publishes a notification of any type on the notification stream
- **THEN** the command posts exactly one corresponding message to the configured Slack webhook

#### Scenario: All bell notification types are supported
- **WHEN** the daemon publishes a notification of type `needs_input`, `ready_to_merge`,
  `pr_merged`, or `pr_closed_unmerged`
- **THEN** each one is rendered and delivered, and no type is silently discarded

#### Scenario: An unrecognized notification type arrives
- **WHEN** the daemon publishes a notification whose type the renderer does not specifically know
- **THEN** the command still delivers a message using the notification's own title and body rather
  than dropping it

### Requirement: Notifications render as a single readable Slack line

The system SHALL render each notification as one concise Slack message carrying the same
information the UI bell presents, including the notification's title and body, and the pull request
URL when the notification carries one.

#### Scenario: Notification carrying a pull request URL
- **WHEN** a notification with a pull request URL is delivered
- **THEN** the Slack message contains the notification title, its body, and a link to that pull
  request

#### Scenario: Notification without a pull request URL
- **WHEN** a notification with no pull request URL is delivered
- **THEN** the Slack message contains the notification title and body and omits any link, without
  error

### Requirement: Missed notifications are reconciled on connect

The system SHALL reconcile against the daemon's current unread notifications each time it
establishes or re-establishes a stream connection, and SHALL additionally reconcile periodically
while running, and SHALL deliver each notification at most once, identified by notification ID. This
is required because the daemon's notification stream is in-process and best-effort with no replay,
so a notification whose live delivery failed on a stream that never drops would otherwise never be
retried.

#### Scenario: Notifications published while disconnected
- **WHEN** the command reconnects after a disconnection during which notifications were published
- **THEN** it lists the daemon's current unread notifications and delivers those it has not
  previously delivered

#### Scenario: A live delivery failed but the stream stays connected
- **WHEN** a notification's live delivery fails and the stream connection does not drop
- **THEN** a later periodic reconciliation re-lists it and delivers it, without requiring a
  reconnection

#### Scenario: A notification appears in both reconciliation and the live stream
- **WHEN** the same notification ID is seen both in the reconciliation listing and on the stream
- **THEN** it is delivered to Slack exactly once

#### Scenario: Nothing was missed
- **WHEN** the command reconnects and every unread notification has already been delivered
- **THEN** no Slack message is sent

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
