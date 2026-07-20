## 1. Harness Usage Producer

- [ ] 1.1 Port the old fork's Claude Code and Codex usage extractor with fixture-backed tests.
- [ ] 1.2 Extend the hook activity request to carry optional token usage deltas and defer cursor commit until the activity POST succeeds.
- [ ] 1.3 Emit `ao.session.usage` telemetry only after lifecycle accepts an activity signal with valid usage.
- [ ] 1.4 Add regression coverage for malformed records, missing data directories, cumulative resets, zero-growth reads, and non-fabricated cost fields.

## 2. Metrics API

- [ ] 2.1 Add a metrics read service that aggregates `ao.session.usage` telemetry by project and harness.
- [ ] 2.2 Add `/api/v1/metrics` controller DTOs, route wiring, OpenAPI generation, and generated TypeScript schema updates.
- [ ] 2.3 Add API and store tests proving invalid telemetry payloads are ignored and valid token usage appears as nonzero rollups.

## 3. Quota Discovery and Storage

- [ ] 3.1 Document discovered Claude Code and Codex quota surfaces, including exact, estimated, and no-signal outcomes.
- [ ] 3.2 Add an additive SQLite migration and store methods for per-harness/account quota-window snapshots.
- [ ] 3.3 Add a quota probe interface on harness adapters with Claude Code, Codex, and Codex Fugu implementations returning exact, estimated, or no signal.
- [ ] 3.4 Add a daemon quota observer that refreshes snapshots without blocking readiness.

## 4. Low-Quota Alerts

- [ ] 4.1 Add configurable low-quota thresholds with safe defaults.
- [ ] 4.2 Add a `low_quota` notification type and persist one notification per harness/account/window/threshold crossing.
- [ ] 4.3 Add tests that repeated below-threshold snapshots dedupe and no-signal snapshots do not alert.

## 5. Operator Surfaces

- [ ] 5.1 Include quota snapshots in metrics/status API responses with signal quality and last-observed metadata.
- [ ] 5.2 Update `ao status` to show per-harness/account quota state honestly.
- [ ] 5.3 Add a supervisor quota panel that displays remaining quota, window timing, and signal quality.
- [ ] 5.4 Add frontend tests and visual verification for the quota panel.

## 6. Verification

- [ ] 6.1 Run backend unit tests covering extractor, telemetry, metrics, quota storage, quota observer, and notifications.
- [ ] 6.2 Run API generation and confirm `openapi.yaml` and `frontend/src/api/schema.ts` are current.
- [ ] 6.3 Run frontend typecheck/build and the relevant UI tests.
- [ ] 6.4 Run an end-to-end manual check with at least one supported harness showing nonzero usage and either quota state or no signal.
