## Why

AO currently cannot show real per-harness subscription usage because the
production hook path does not emit token deltas from the places Claude Code and
Codex actually record them. That leaves fleet metrics reading as zero and gives
the operator no warning when a subscription window is near exhaustion.

## What Changes

- Add best-effort extraction of real per-session token deltas for supported
  subscription harnesses and emit `ao.session.usage` telemetry after accepted
  lifecycle activity.
- Add per-harness/account quota-window snapshots with honest signal quality:
  exact when a harness exposes quota state, estimated when AO can derive it, and
  no signal when neither is available.
- Add `/api/v1/metrics` read models and `ao status` output that include usage
  rollups and quota-window state.
- Surface quota state in the supervisor UI with clear window timing, remaining
  usage, and signal quality.
- Emit a user-facing low-quota notification intent when a configured threshold
  is crossed.
- Keep scheduling and dispatch unchanged. Usage-based worker routing is
  explicitly out of scope.

## Capabilities

### New Capabilities

- `session-usage-telemetry`: Real token usage deltas are extracted from
  harness-local records and surfaced through AO metrics.
- `subscription-quota-state`: Subscription quota windows are discovered,
  stored, displayed, and alerted without fabricating unavailable data.

### Modified Capabilities

- None.

## Impact

- Backend hook CLI, lifecycle activity ingestion, telemetry storage, metrics
  aggregation, quota snapshot storage, notification production, and generated
  API schema.
- Frontend and mobile/API clients that consume metrics, status, notifications,
  or generated schema types.
- Harness adapters for Claude Code, Codex, and Codex Fugu, with future harnesses
  reporting no signal until a quota source is implemented.
