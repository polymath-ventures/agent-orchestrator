## Context

AO already accepts harness activity through the session activity route and
persists structured telemetry events. The old fork contains a working
best-effort extractor for Claude Code transcript JSONL and Codex rollout JSONL,
but this checkout does not yet connect real token deltas to telemetry or expose
metrics rollups. The quota half is new and intentionally discovery-led because
subscription harnesses differ in whether they expose exact quota windows, local
state, response headers, or no usable signal.

## Goals / Non-Goals

**Goals:**

- Port the old extractor and tests as the producer for real session token
  telemetry.
- Preserve the hook contract: extraction failure never breaks the agent.
- Add durable quota snapshots with explicit signal quality.
- Surface usage and quota state through API, CLI, and UI.
- Notify the operator when a known quota window crosses a configurable low
  threshold.

**Non-Goals:**

- Usage-based scheduling, dispatch, worker mix automation, or any automatic
  throttling.
- Fabricated dollar costs or fabricated quota remaining values.
- A mandatory external billing integration.

## Decisions

### Extract usage at the harness turn boundary

Use the existing hook activity path as the producer and extend the activity
payload with optional usage fields. The extractor reads harness-local records
only from the hook process context and returns a commit callback that advances
the cursor after the activity POST succeeds.

Alternative considered: poll transcript and rollout files from the daemon. That
would add a second source of session identity, require global filesystem scans,
and make duplicate suppression harder than using the hook turn boundary that
already owns activity delivery.

### Keep token usage in telemetry and aggregate at read time

Emit `ao.session.usage` rows into the existing telemetry store, then build
metrics rollups from those rows. This keeps the event source append-only and
avoids a second write-side usage table that would need to agree with telemetry.

Alternative considered: write a dedicated usage aggregate table at hook time.
That is faster to read but introduces cross-table consistency and replay
problems before the feature has proven it needs precomputed aggregates.

### Store quota snapshots separately from usage telemetry

Add a quota snapshot store keyed by harness, account identity when known, and
window. A snapshot records window start/end, used, remaining, limit, observed
time, source, and signal quality (`exact`, `estimated`, `none`). Snapshots are
state, not events: clients need the current quota picture, while telemetry keeps
the historical usage stream.

Alternative considered: represent quota as telemetry events only. That would
make the current state a query over event history and complicate notification
dedupe per window.

### Make quota collectors adapter-owned

Introduce a small quota probe interface implemented by harness adapters that
can report exact, estimated, or no-signal snapshots. The daemon quota observer
iterates installed subscription harnesses on a configurable interval and after
relevant usage signals. Each adapter owns its local file, CLI, header, or API
knowledge so quota rules stay close to the harness that understands them.

Alternative considered: one central collector that knows every harness format.
That would duplicate adapter knowledge and make future harness additions more
fragile.

### Dedupe low-quota notifications at snapshot ingestion

When a new snapshot crosses the threshold, the quota service checks whether it
has already notified for the same harness/account/window/threshold. If not, it
creates a normal notification intent with a new `low_quota` notification type.

Alternative considered: let the UI notice low quota and show alerts locally.
That would miss CLI/headless operation and could alert repeatedly across
clients.

## Risks / Trade-offs

- Harness formats can change -> extractor and quota probes must be
  best-effort, fixture-backed, and fail closed to no signal.
- Exact quota APIs may require credentials or network calls -> probes must use
  existing local auth where possible and never block daemon readiness.
- Estimates can mislead -> every estimate must carry signal quality and basis,
  and no-signal must remain visible.
- Metrics reads over telemetry can become expensive -> start with bounded
  queries and add precomputed aggregates only if measurement shows a need.

## Migration Plan

1. Add usage extraction tests and port the extractor.
2. Extend activity DTOs and lifecycle handling to emit usage telemetry.
3. Add metrics read models over telemetry and expose `/api/v1/metrics`.
4. Add quota snapshot storage and adapter quota probe interfaces.
5. Add low-quota notification type and dedupe logic.
6. Regenerate API schema and wire CLI/UI consumers.

Rollback is straightforward for partial rollout: disabling extraction or quota
observation leaves existing activity and session behavior unchanged. New
database migrations must be additive and reversible.

## Open Questions

- Which Claude Code surfaces provide exact quota windows today versus only
  usage observations?
- Which Codex subscription surfaces provide quota windows today versus only
  usage observations?
- Should default low-quota thresholds be percent-based, absolute remaining
  usage, or both? The implementation should support configuration flexible
  enough for either while choosing one conservative default.
