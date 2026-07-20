## Context

AO's notification pipeline is small and already complete on the producing side. `notify.Manager`
persists a notification and publishes it to an in-process `notify.Hub`
(`backend/internal/notify/manager.go`, `hub.go`). The HTTP layer exposes two endpoints the desktop
bell already uses (`backend/internal/httpd/controllers/notifications.go`):

- `GET /api/v1/notifications/stream` — SSE emitting `event: notification_created` with the **full
  rendered `NotificationResponse`** as the data payload.
- `GET /api/v1/notifications?status=unread&limit=N` — the current unread list.

The loopback listener is unauthenticated (`backend/internal/httpd/router.go`), so a local process
needs no credentials.

Two constraints shape everything below. First, this is a **fork-only** feature, so it must not
touch upstream-owned daemon surface. Second, the hub is **best-effort with no replay**: `Publish`
drops on a full buffer and there is no `Last-Event-ID` support, so the stream alone cannot be
trusted as a complete record.

The ticket (GH #9) proposed a sidecar over `/api/v1/events`. That endpoint is factually unusable
here: the `notifications` table has no CDC trigger, so notification rows never reach `change_log`
and never reach that stream. `/api/v1/notifications/stream` is the endpoint that already carries
what the bell shows.

## Goals / Non-Goals

**Goals:**

- Mirror every notification the bell displays into one Slack channel, one-way.
- Zero daemon changes: no endpoints, no migrations, no `internal/notify` edits, no regenerated
  OpenAPI or TypeScript schema.
- Survive the hub's lack of replay without re-implementing any notification policy.
- Be deletable: removing one command file and its registration removes the whole feature.

**Non-Goals:**

- Any inbound Slack surface — no slash commands, interactivity, threads, mention routing, or
  reply-to-agent.
- Notification filtering, muting, routing, or per-type policy.
- An in-daemon notification-channel abstraction.
- Reviving anything from the old fork's `ops/` layer.

## Decisions

### Consume `/api/v1/notifications/stream`, not `/api/v1/events`

The notifications stream already carries the fully enriched payload — `enrich.go` has computed
title and body before the record is published — so the consumer renders and forwards, and owns no
notification semantics. Consuming `/api/v1/events` instead would mean re-deriving `needs_input` and
`ready_to_merge` from raw `session_updated` / `pr_updated` rows, duplicating lifecycle policy and
its dedupe rule. _Alternative rejected: adding a CDC trigger to the `notifications` table._ That
works and is arguably cleaner long-term, but it is a migration plus a new `cdc.EventType` plus a
`change_log` CHECK change — upstream-owned surface for a fork-only feature, and unnecessary because
a purpose-built stream already exists.

### A `ao notify slack` CLI subcommand, not a separate deployable

A parent command with a single child matches existing nesting idiom (`review.go`, `preview.go`).
It ships inside the binary the operator already has and reuses the run-file/loopback resolution in
`cli.commandContext`. _Alternative rejected: a standalone process under a new `ops/` directory with
a systemd unit_ — that mirrors the old fork, but this repo has no `ops/`, no `deploy/`, and no
sidecar precedent, so it would add a top-level surface and a second thing to build and ship.

_Alternative rejected: an in-daemon webhook channel._ `notify.Manager` holds a single `publisher`
field and `notify.Publisher` has exactly one implementation — there is no plurality to extend. That
path means inventing a delivery-target registry, a settings surface, secret storage, and retry, all
in files upstream owns.

### One delivery worker; reconciliation on a periodic ticker

The command runs three concurrent parts: a stream reader that enqueues live notifications, a
reconcile loop, and a single delivery worker that drains the queue and posts to Slack. The worker is
the only poster and the only reader of the delivered-ID ledger, so dedupe needs no lock — and, more
importantly, **no lock is ever held across the network POST**. The stream reader keeps draining the
daemon's 64-slot subscriber buffer while the worker catches up, so a slow webhook cannot cause the
hub to drop. (Two earlier iterations got this wrong: the first reconciled _before_ consuming; the
second reconciled concurrently but held the deliverer's mutex across the POST, which serialized the
producers anyway. Both were found in review.)

Reconciliation runs **both on every successful (re)connect and on a periodic tick**. The on-connect
pass is what the spec requires: a notification the hub dropped while the process was disconnected is
recovered the moment the stream comes back, not after an arbitrary wait. The periodic tick is the
independent safety net for the case reconnect does not cover — a transiently-failed Slack post on a
stream that never drops — since the hub is best-effort with no replay. The stream loop pokes a small
buffered channel on each connect; the reconcile loop selects on that poke, the ticker, or
cancellation, and re-enqueues anything the worker has not delivered. Dedupe by ID keeps that
at-most-once. This subsumes the old "subscribe before reconcile" ordering argument: a notification
published in the gap between the first listing and the subscription is delivered by the next pass.

One residual gap is inherent and accepted: this command has no persistence and delivers only what
the unread listing still shows, so a notification whose live post fails **and** which the operator
marks read in the UI before the next reconcile is not delivered. The retry requirement is met (the
periodic pass retries), but the window cannot be closed without either persistence or reading the
operator's read-state faster than they set it — neither justified for a best-effort mirror whose
source, the bell, has the same horizon.

### Dedupe by notification ID, recorded only on success, in an unbounded ledger

Delivered notification IDs are held in memory, and an ID is recorded **only after Slack accepts the
message**. Recording before the post — the original version — meant a failed delivery permanently
suppressed that notification, because reconciliation would then skip it forever. Recording only on
success means a failed post is simply re-offered by the next periodic reconcile.

The ledger is unbounded. An earlier version evicted the oldest entries, arguing that a notification
too old to appear in a capped unread listing could never be re-offered. That argument is false: this
command never marks anything read, so a delivered notification stays _unread_ and can re-enter the
listing once newer ones are read in the UI. Eviction therefore reintroduces exactly the duplicates
dedupe exists to prevent. Entries are one notification id apiece, so the ledger stays small in any
realistic run. (Both defects were found in review.)

_Alternative rejected: persisting delivered IDs to disk._ It buys only cross-restart dedupe, whose
failure mode is a duplicate Slack line after a restart, and it introduces state that violates the
"removing the config leaves zero residue" requirement.

### Add one streaming helper to `client.go`

`doJSONPath` already owns config load → run-file read → liveness check → loopback URL construction.
The streaming path needs the same resolution but must return the response body undecoded, with the
2-minute command timeout removed (following the copy-the-client-and-clear-`Timeout` pattern in
`start.go`'s asset download). Putting it beside `doJSONPath` keeps daemon-URL resolution in the one
place that owns it instead of re-deriving it in the new command.

### Signal handling scoped to the command

`root.go` calls `cmd.Execute()` with a background context, so no CLI command is currently
cancellable. Rather than change the shared execution path for one command, the command wraps its own
context with `signal.NotifyContext` and returns nil on cancellation, so Ctrl-C and SIGTERM exit 0.

### Configuration: env var primary, flag secondary

The webhook URL is a secret, and a flag on a long-lived process is visible in `ps`. `AO_SLACK_WEBHOOK_URL`
is therefore the documented path, with `--webhook-url` accepted for convenience and testing. Neither
present is CLI misuse: return `usageError` so the exit code is 2, matching `send.go` / `review.go`.
Everything else — daemon unreachable, API errors — is a plain error, exit 1.

### Rendering is a pure function

`renderNotification(NotificationResponse) string` takes the DTO and returns the Slack message text,
with no I/O. It is the entire "presentation" of this feature and is directly table-testable per
notification type, including the unknown-type fallback to the payload's own title and body. Slack
delivery is a plain `POST` of `{"text": ...}` to the webhook — no SDK, no new dependency.

### Failure policy: fail fast at startup, retry while running

An unreachable daemon at startup is an operator error worth surfacing immediately (exit 1). Once
running, a dropped stream reconnects with backoff and re-reconciles, and a failed Slack post is
reported to stderr without interrupting stream consumption — a notification channel that exits
because Slack hiccuped is worse than one that misses a line. Backoff timing goes through
`deps.Now` / `deps.Sleep`, following `stop.go`'s `waitForStopped`, so the loop is deterministic
under test.

## Risks / Trade-offs

- **Hub drops under load** → `notify.Hub.Publish` is non-blocking and drops on a full 64-slot
  buffer. The stream reader drains it independently of Slack latency, so this only bites on a burst
  of more than 64 notifications faster than the reader can enqueue them — and even then the periodic
  reconcile recovers the dropped ones from the unread listing within one interval. Accepted: the
  notification volume this fork produces is far below that threshold. Noted here so a future operator
  recognizes the symptom.
- **Backlog deeper than one unread page** → reconciliation asks for the daemon's maximum unread page
  (100). A disconnection that accumulates more than that many unread notifications cannot recover
  the oldest ones from the listing, and they were never on the stream. Accepted: recovering deeper
  history needs pagination or daemon changes, and the bell has the same horizon.
- **Duplicate line after a restart** → in-memory dedupe means a restart can re-deliver notifications
  still unread. Accepted deliberately over on-disk state; a repeated Slack line is cheap.
- **Secret in a flag** → mitigated by making the env var the documented path and the flag the
  fallback.
- **New CLI SSE reader with no precedent** → this is the first streaming consumer in the CLI, so the
  parsing is hand-rolled. Mitigated by keeping it minimal (the daemon emits one event type with a
  single-line `data:` frame) and by driving it from a `httptest` server in tests.
- **Upstream adds notification types** → the renderer's fallback path delivers unknown types using
  their own title and body, so a new type degrades to a plain line rather than being dropped.

## Migration Plan

No migration. The command does not exist until it is run; running it changes no daemon state.
Rollback is stopping the process and unsetting `AO_SLACK_WEBHOOK_URL`.

## Open Questions

None. Delivery shape and packaging were settled with the operator before this design
(recorded on GH #9); the remaining choices above are implementation-level and resolved here.
