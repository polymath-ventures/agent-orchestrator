## Why

AO already produces notifications and shows them in the desktop UI bell, but an operator away from
the desktop app sees nothing. Slack is where the operator already is, so mirroring the existing
notifications there — one-way, no new notification semantics — closes the gap for the smallest
possible amount of code. Tracks GH #9 / bead `ao-289`.

## What Changes

- Add a long-lived `ao` CLI subcommand that subscribes to the daemon's existing
  `GET /api/v1/notifications/stream` SSE endpoint and posts each notification to a Slack incoming
  webhook as a one-line message.
- Render every notification type the UI bell displays (`needs_input`, `ready_to_merge`, `pr_merged`,
  `pr_closed_unmerged`) from the payload the stream already carries. No type filtering, no
  per-type policy, no new notification kinds.
- Reconcile missed notifications on (re)connect by listing current unread notifications via
  `GET /api/v1/notifications?status=unread` and deduping against already-delivered notification IDs,
  because the daemon's notification hub is in-process and best-effort with no replay.
- Configure via the webhook URL only. Absent configuration, the command refuses to start with a
  usage error and nothing else in AO changes behavior.
- **No daemon changes.** No new endpoints, no schema changes, no migrations, no changes to
  `internal/notify`. This is a fork-only consumer of an already-public loopback API.

Non-goals (explicit, and they stay non-goals):

- No inbound Slack handling of any kind — no slash commands, no interactivity, no threads, no
  mention routing, no reply-to-agent.
- No attention system, needs-response resolution, or Slack health loop. The old fork's `ops/`
  layer is dead and stays dead.
- No filtering, routing, or muting logic beyond "configured or not configured".

## Capabilities

### New Capabilities

- `slack-notifications`: one-way mirroring of AO's existing notifications to a Slack channel via a
  read-only CLI sidecar, including connection lifecycle, gap reconciliation, delivery rendering,
  and configuration/teardown behavior.

### Modified Capabilities

None. `openspec/specs/` currently carries no capabilities, and this change adds no requirement to
any existing daemon behavior — it only consumes an endpoint that already exists.

## Impact

- **New code**: one CLI command package under `backend/internal/cli`, plus a small Slack webhook
  client. Registered on the existing root command.
- **Consumed, unchanged**: `GET /api/v1/notifications/stream` and `GET /api/v1/notifications`
  (`backend/internal/httpd/controllers/notifications.go`), reached through the shared CLI daemon
  client helpers.
- **Untouched**: `backend/internal/notify`, the notifications migration, the OpenAPI spec, the
  frontend, and every other daemon surface. No generated-code regeneration required.
- **Dependencies**: none added; Slack incoming webhooks are plain HTTP POST via the standard
  library.
- **Removal**: deleting the command file and its registration removes the entire feature. No
  residue in the daemon, the database, or the UI.
- **Upstream**: fork-only. Because it adds no daemon surface, it carries no upstream rebase risk.
