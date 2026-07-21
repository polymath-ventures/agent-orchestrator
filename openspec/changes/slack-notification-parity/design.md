## Context

PR #20 shipped `ao notify slack`: a one-way sidecar consuming
`GET /api/v1/notifications/stream` and posting each notification to a Slack incoming webhook, with
an in-memory delivered-ID ledger, a periodic + on-connect reconcile against `GET
/api/v1/notifications?status=unread`, and a cancellation-aware 2s→30s exponential reconnect backoff
(`backend/internal/cli/slack.go`).

A verified gap analysis against the reference implementation
(`~/agent-orchestrator-fscked/ops/ao-slack-notifier.mjs`) found production-learned behaviors the
reimplementation dropped. This change ports those behaviors under the strategy PORT → FIT → IMPROVE
→ FIX: port the reference's refined behavior, fit it to the current Go tree and domain types, keep
the pieces this fork already does better, and apply only the operator-refined deltas. Every ported
requirement cites reference `file:line` so "studied the reference" is falsifiable.

The reference's own two-way accretions (attention projection, needs-response reply routing, thread
bindings, capacity dashboard, usage scheduling, label routing, behavior-version convergence) remain
ruled dead and are NOT ported.

## Goals / Non-Goals

**Goals:**

- Persist the delivery ledger so a restart never re-floods the Slack channel (gap 1, must).
- Seed the initial unread snapshot as delivered-without-posting on first run so the first start
  against an existing backlog is silent (gap 1, must).
- Mention a configured Slack member for attention-class notification types; leave routine types
  unmentioned; delivery stays mention-out only (gap 2, must).
- Recover a full unread backlog larger than one page (gap 3, should).
- Latch a single daemon-unreachable alert after N consecutive post-connect failures, reset on
  recovery (gap 4, should).
- Give every current `domain.NotificationType` a distinct one-line rendering (gap 5, should).
- Preserve the current exponential reconnect backoff (verified better than the reference's flat
  10s retry).
- Verify each history-mined incident lesson survives, or record why it no longer applies.

**Non-Goals:**

- No inbound Slack surface of any kind (listener, slash command, interactivity, reply routing).
- No Slack message resolution edits / strike-throughs (gap 6): they require bot-token delivery and
  persisted message timestamps — disproportionate to a thin one-way webhook channel.
- No attention projection, capacity dashboard, usage-based scheduling, tracker label routing, or
  behavior-version convergence.
- No generic in-daemon delivery-target abstraction; the channel stays a CLI sidecar.

## Decisions

### D1 — Persistent delivery ledger under the AO data dir (gap 1)

Replace the in-memory `delivered map[string]struct{}` (`slack.go:261`) with a JSON ledger persisted
atomically (temp file + rename, parent dir created on write), mirroring the reference's `saveState`
(`ao-slack-notifier.mjs:610-646`, `STATE_FILE` `:74`). The ledger stores the set of delivered
notification IDs plus a bootstrap-`initialized` flag (`:622-626`).

- **Path:** default under AO's configured data dir (`AO_DATA_DIR`, else `~/.ao`), e.g.
  `<dataDir>/slack-notifier-state.json`, overridable by an env var. This honors the repo rule that
  all app state lives under `~/.ao` unless `AO_DATA_DIR`/`AO_RUN_FILE` overrides it — the code
  guarantees the path (`mkdir -p`), not the systemd unit (reference gap 6/history item 6:
  `:612`; the reference unit had no `StateDirectory`).
- **Write-order invariant (history item 5, ref PR #198 cycle 1):** the on-disk ledger and the
  in-memory set are only updated AFTER Slack accepts a post and the persist write succeeds. A
  delivered ID is never marked before the write, so a persist failure re-offers the notification
  rather than silently suppressing it forever.
- **Ledger stays append-only / tail-bounded, never evicted below what can re-appear unread.** The
  round-1 comment (`slack.go:226-232`) already established that eviction reintroduces duplicates
  because this command never marks anything read; persisting preserves that property. Bound growth
  by tail-capping to a large limit (reference `SEEN_LIMIT` 2000, `:75,:613`).

**Alternatives rejected:** in-memory only (status quo — the flood cause, gap 1); marking
notifications read after posting (would mutate shared daemon state the UI bell owns, and history
item 3 shows read-after-post reintroduces spam via fresh unread rows).

### D2 — Seed-without-posting on first run (gap 1)

On first start (ledger absent or `initialized=false`), list the current unread snapshot and record
every ID as delivered WITHOUT posting, then set `initialized=true` and persist
(ref `:717,:789-790,:808` bootstrap; `:725-745` cursor-seed). This removes the flood at its source
(history items 1 and 6: dedupe state that died each restart re-flooded). Subsequent starts read the
ledger and deliver only genuinely new notifications.

### D3 — Attention-class @mention, mapped to current domain types (gap 2)

Attention-class types get an `<@MEMBER_ID>` prefix when a member id is configured; all other types
render unmentioned; a missing member id degrades to no mention (never an error). This ports
`MENTION_KINDS` + `renderAlert`'s conditional prefix (`:154-165,:891-915`,
`attention-core.mjs:133-143`) but maps to THIS fork's `domain.NotificationType` values rather than
the reference's projection kinds.

- **Attention-class set (per ticket gap 2):** `needs_input`, `prime_restart_capped`,
  `model_unreachable`.
- **Reference-kind discrepancy (recorded, not silently changed):** the reference's mention set used
  projection kinds — `needs_input`→`decision`, `prime_restart_capped`→`orchestrator_replacement_capped`
  (migration map `ao-slack-notifier.mjs:430-439`), and there `model_unreachable` was informational,
  not attention-class. This fork has no attention projection; it renders `domain.NotificationType`
  directly, so the ticket's current-type names are authoritative here.
- **Config:** member id from an env var (reference `SLACK_MEMBER_ID`, `:73`), under the AO_ prefix
  convention plus the conventional name as a fallback, matching the existing webhook-URL precedence.
- **One-way:** mention-out only. No `needsResponseMessages` ledger, no thread binding, no resolution
  edit (that reference machinery, `:901-913`, is the two-way path and is excluded).

### D4 — Full unread-backlog pagination (gap 3)

The reference paged unread via `&before={createdAt}&beforeId={id}` (`:746-778`). This fork's list
endpoint supports only `status=unread&limit` capped at 100 (`controllers/dto.go:549-552`,
`service/notification/service.go:12-15`, `queries/notifications.sql:7-12`) — no cursor. So the FIT
adds a keyset cursor to the existing endpoint and pages from the sidecar:

- **Daemon:** add a `ListUnreadNotificationsBefore` keyset query
  (`WHERE status='unread' AND (created_at < :before OR (created_at = :before AND id < :beforeId)) ORDER BY created_at DESC, id DESC LIMIT ?`),
  thread `Before`/`BeforeID` through `ListFilter` and `ListUnread`, and accept optional `before` /
  `beforeId` query params on `GET /api/v1/notifications`. Regenerate sqlc, OpenAPI, and the TS
  client. This is a bounded, upstreamable read-only addition; existing callers that omit the cursor
  are unaffected.
- **Sidecar:** during reconcile, page newest-first using the last row's `(createdAt, id)` as the
  next cursor until a short page is returned, deduping by the ledger as today. Deliver oldest-first
  within the drained set so Slack ordering matches creation order (ref `:780`).
- **Cursor stability:** `created_at` alone is not unique; the `(created_at, id)` keyset is the
  stable tiebreak. `id` ordering is lexical and only breaks ties within an identical timestamp,
  which is sufficient for a monotonic drain.

**Alternative rejected:** leave the 100-cap and defer. Rejected because with seed-without-posting
the residual risk is small but the operator reopened specifically to close this gap with
falsifiable evidence, and no in-sidecar-only fix exists without the cursor.

### D5 — Latched daemon-unreachable alert (gap 4)

After N=3 consecutive stream failures that occur AFTER at least one successful connect, post one
`daemon_unreachable`-style message to Slack (mentioning the member if configured), then latch so it
is not repeated; reset the latch and the failure counter on the next successful connect (ref
`:1017-1045,:1179-1180,:1211`). The very first connection failure still exits non-zero as today
(startup misconfig, `slack.go:374-378`); the latched alert covers a daemon that dies mid-run. The
alert is delivered to the same webhook (this fork has one channel, not the reference's two).

### D6 — Per-type rendering for every current type (gap 5)

Extend `notificationIcon`/`renderNotification` (`slack.go:153-166,181-202`) so each of the eight
current `domain.NotificationType` values renders a distinct one-liner instead of four falling back
to `:bell:` (ref `ICONS` `:166-172`): `needs_input`, `ready_to_merge`, `pr_merged`,
`pr_closed_unmerged`, `low_quota`, `model_unreachable`, `model_recovered`, `prime_restart_capped`.
An unknown future type still falls back to a generic icon (never dropped) — the existing
forward-compatible default is preserved.

### D7 — Preserve the exponential reconnect backoff (IMPROVE)

Keep the current 2s→30s cancellation-aware backoff + periodic reconcile
(`slack.go:56-67,398-419`). Do NOT port the reference's flat 10s `RECONNECT_MS` retry (`:80`) — the
gap analysis marked the current behavior as verified-better.

## History-mined checklist disposition

Each incident lesson from the issue's history-mined comment, and whether this change adopts it:

1. **SSE ~5-min death (undici bodyTimeout), needs server `: keepalive` heartbeat (#86).** The Go
   consumer uses `net/http` with the client timeout cleared for the stream (`slack.go:457-458`), so
   it does not hit undici's bodyTimeout. Verify the daemon SSE handler emits periodic heartbeat
   comments for browser/EventSource consumers; if absent, add a `: keepalive` tick to
   `notifications.go stream` since the consumer already tolerates SSE comment lines
   (`slack.go:509-510`). Adopt (verify-or-add).
2. **Reconnect must resume via `?after=`/Last-Event-ID or replay from seq 0 (#86).** This fork's
   hub is in-process best-effort with no replay/Last-Event-ID (round-1 design decision;
   `notify/hub.go`). The ledger + reconcile is the recovery mechanism instead; no stream cursor is
   added. Recorded as not-applicable with evidence.
3. **Read-after-post reintroduces spam; dedupe belongs at the lifecycle layer (#190/#198).** This
   channel never marks read and dedupes by persisted ID, so this class cannot occur here. Adopt by
   construction; no lifecycle change needed for the Slack channel.
4. **Signature must clear on non-ready observations (#198).** N/A — no per-PR ready signature in a
   one-way ID-deduped mirror. Recorded not-applicable.
5. **Never mutate the seen-set before the persist write succeeds (#198).** Adopt as the D1
   write-order invariant.
6. **Dedupe state died each restart → re-flood (#87).** Adopt via D1 (persisted, code-guaranteed
   path) + D2 (seed-without-posting).
7. **needs_input emission must be role/transition-aware (#189).** This is a daemon lifecycle-emission
   concern upstream of the Slack channel; the sidecar mirrors whatever `needs_input` the daemon
   publishes and adds no emission policy. Recorded not-applicable (out of the sidecar's layer).
8. **Shutdown paths must be abort-signal-aware (#197/#200).** Already satisfied — every sleep/post
   is `ctx`-aware via `waitOrDone`/`NotifyContext` (`slack.go:69-78,310-312`); the new persist and
   pagination paths MUST also honor `ctx`. Adopt and extend.
9. **The notifier must carry fleet-dies-silently classes (#153).** The gap-2 attention set covers
   the fork's current fleet-health notification types (`prime_restart_capped`, `model_unreachable`);
   projection-only classes (orchestrator_dead/no_signal) have no `domain.NotificationType` here and
   are out of scope. Adopt within the current type set.
10. **Alerts stay ON during fleet pause (#161).** N/A to the sidecar — it mirrors the daemon's
    stream unconditionally and applies no pause gating. Recorded not-applicable.

## Risks / Trade-offs

- [Ledger path not writable under systemd] → mkdir the parent in code and document the env override;
  a persist failure logs and re-offers rather than crashing (D1 write-order invariant).
- [Cursor query added to a public endpoint] → keyset is additive and optional; omitting the cursor
  preserves current behavior, and the generated OpenAPI/TS are regenerated and committed together.
- [Attention set diverges from reference projection kinds] → recorded explicitly (D3) with the
  migration-map citation so review can falsify the mapping.
- [Seed-without-posting hides a genuine first-run backlog] → intended (gap 1 is precisely "do not
  re-post the backlog"); the UI bell remains the record of unread history.

## Migration Plan

No data migration. A new sqlc query and optional query params are additive. First run after upgrade
seeds the ledger silently (D2), so operators see no backlog flood. Rollback is reverting the PR; the
ledger file is inert if the command is not run and leaves no residue when removed (existing one-way
guarantee).

## Open Questions

None blocking. The daemon SSE heartbeat (checklist item 1) is verify-first: if the handler already
emits keepalives, that task is a test asserting it; if not, it is a minimal additive tick.
