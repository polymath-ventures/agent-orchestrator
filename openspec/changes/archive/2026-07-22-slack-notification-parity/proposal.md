## Why

The first Slack-channel implementation shipped in PR #20, but verified comparison against the
accumulated reference implementation found restart flooding, missing attention mentions, a
single-page recovery ceiling, weak outage signaling, and incomplete per-type rendering. Issue #9
was reopened on July 21, 2026 so those production-learned behaviors can be ported without reviving
the old fork's two-way attention system.

## What Changes

- Persist Slack delivery state under AO's configured data directory, write it atomically, and seed
  the initial unread snapshot without posting historical notifications.
- Page through the full unread backlog and advance durable state only after Slack accepts a
  notification, so reconnect/restart recovery is complete without silent loss.
- Optionally mention a configured Slack member for the current notification types that require
  operator attention, while keeping routine notifications unmentioned and delivery one-way.
- Give every current `domain.NotificationType` a distinct one-line Slack rendering.
- Latch a single operator-visible daemon-unreachable alert after three consecutive post-connect
  failures, reset the latch on recovery, and preserve the current implementation's superior
  cancellation-aware 2s-to-30s exponential reconnect backoff.
- Verify the issue's history-mined failure checklist, including SSE heartbeats, resumable
  reconnect semantics where the current endpoint supports them, lifecycle-owned duplicate
  prevention, persisted-state write ordering, shutdown responsiveness, and observability during
  pause.
- Explicitly omit Slack resolution edits: they would require bot-token delivery and persisted
  message timestamps, which is disproportionate to the ticket's thin one-way webhook channel.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `slack-notifications`: Strengthen restart/reconnect recovery, attention rendering, outage
  signaling, and per-type presentation for the existing one-way Slack notification channel.

## Impact

Primary impact is the existing CLI sidecar in `backend/internal/cli/slack.go`, its tests, and the
Slack CLI documentation. The notification-list API may need cursor pagination support only if the
existing controller/storage path cannot already express a stable `(created_at, id)` cursor. No
inbound Slack handling, reply routing, attention projection, capacity dashboard, usage scheduling,
or generic in-daemon delivery-target abstraction is introduced.
