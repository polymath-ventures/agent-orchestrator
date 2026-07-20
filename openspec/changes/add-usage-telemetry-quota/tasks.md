## 1. Harness Usage Producer

- [x] 1.1 Port the old fork's Claude Code and Codex usage extractor with fixture-backed tests.
- [x] 1.2 Extend the hook activity request to carry optional token usage deltas and defer cursor commit until the activity POST succeeds.
- [x] 1.3 Emit `ao.session.usage` telemetry only after lifecycle accepts an activity signal with valid usage.
- [x] 1.4 Add regression coverage for malformed records, missing data directories, cumulative resets, zero-growth reads, and non-fabricated cost fields.

## 2. Metrics API

- [x] 2.1 Add a metrics read service that aggregates `ao.session.usage` telemetry by project and harness.
- [x] 2.2 Add `/api/v1/metrics` controller DTOs, route wiring, OpenAPI generation, and generated TypeScript schema updates.
- [x] 2.3 Add API and store tests proving invalid telemetry payloads are ignored and valid token usage appears as nonzero rollups.

## 3. Quota Discovery and Storage

- [x] 3.1 Document discovered Claude Code and Codex quota surfaces, including exact, estimated, and no-signal outcomes.
- [x] 3.2 Add an additive SQLite migration and store methods for per-harness/account quota-window snapshots.
- [x] 3.3 Add a quota collector interface with Claude Code and Codex no-signal implementations.
- [x] 3.4 Add a daemon quota observer that refreshes snapshots without blocking readiness.

## 4. Low-Quota Alerts

- [x] 4.1 Add configurable low-quota thresholds with safe defaults.
- [x] 4.2 Add a `low_quota` notification type and persist one notification per harness/account/window/threshold crossing.
- [x] 4.3 Add tests that repeated below-threshold snapshots dedupe and no-signal snapshots do not alert.

## 5. Operator Surfaces

- [x] 5.1 Include quota snapshots in metrics/status API responses with signal quality and last-observed metadata.
- [x] 5.2 Update `ao status` to show per-harness/account quota state honestly.
- [x] 5.3 Add a supervisor quota panel that displays remaining quota, window timing, and signal quality.
- [x] 5.4 Add frontend tests and visual verification for the quota panel.

## 6. Verification

- [x] 6.1 Run backend unit tests covering extractor, telemetry, metrics, quota storage, quota observer, and notifications.
- [x] 6.2 Run API generation and confirm `openapi.yaml` and `frontend/src/api/schema.ts` are current.
- [x] 6.3 Run frontend typecheck and the relevant UI tests. The frontend package has no `build` script.
- [x] 6.4 Run a browser-rendered manual check showing nonzero usage and no-signal quota states, plus mocked estimated quota rendering for the UI state the first exact/estimated collector will produce.
