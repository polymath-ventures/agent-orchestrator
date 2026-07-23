# Quota redesign: daemon-driven harness probes (GH #97 / ao-7mp)

## Problem

The quota widget (PR #16, corrected by PR #88) is architecturally wrong:

1. `StoreQuotaCollector` re-asserts a hardcoded `unknown / no signal / none`
   placeholder row for each harness on every 30s tick
   (`observe/metrics/quota.go` `noSignalSource`/`noSignalBasis`). These render as
   stale garbage and the only explanation is a tooltip.
2. Real quota is only written when an **AO-managed Codex session** completes a
   turn (PR #88 Stop-hook). Harness usage is a property of the harness login on
   the machine, independent of AO projects/sessions. A fresh install shows "no
   signal" forever until an agent happens to run.
3. The "Claude Code has no machine-readable quota source" claim is false:
   `claude -p "/usage"` returns parseable text headlessly.

## Behavior (operator spec, 2026-07-23)

Daemon probes each configured harness for current usage, independent of
projects/sessions; honest inline widget states; widget relocated to sidebar
bottom above Settings.

## Design

### Source of record = daemon probes

A dedicated `QuotaProber` component (started by the daemon like the metrics
Observer) probes each harness:

- **claude-code**: `claude -p "/usage"`, bounded (`context.WithTimeout`), env
  scrubbed of `AO_SESSION_ID` / `AO_RUNTIME_TOKEN` / `AO_RUN_FILE`, stdin closed,
  process-group cleanup — mirroring the existing bounded-probe pattern in
  `adapters/agent/claudecode/claudecode.go` `ValidateModel`. Parse the
  `N% used · resets <time>` lines. Costs a small quota turn → **hourly** cadence.
- **codex / codex-fugu**: passive read of the newest rollout `rate_limits` event
  under `CODEX_HOME/sessions/**` (cwd-independent). Zero cost → probes freely.
  codex-fugu shares `CODEX_HOME` with codex (one adapter, `NewFugu`), so a single
  **combined codex chip** covers both (per the ticket's decision default). Verify
  the shared-home assumption in Phase 2 and note it in the PR.

Cadence: probe-all on daemon start, then hourly when idle. Force-probe via
`POST /api/v1/metrics/probe` (optional `{harness}`) and a widget button.
Single-flight per harness (per-harness lock; force-probe skips if in flight).

Real snapshots are **persisted to `quota_snapshots`** (upsert), so they feed
alerts + history and render immediately on load. Probe *status* (not_probed /
failed+reason / no_source / ok+age) is held **in memory** on the prober and
exposed via the metrics API — it is rebuilt on daemon start, so no new table.

### Honest states (no tooltip-only)

`ProbeState`: `ok` | `not_probed` | `failed` | `no_source`. Each carries a short
reason (a raw excerpt for `failed`; recorded evidence for `no_source`). The
widget renders exactly one inline per harness chip:

- **ok** → window used % + reset time + data age.
- **not_probed** → "not probed yet" + probe-now button.
- **failed** → "probe failed: <reason>" + probe-now button.
- **no_source** → "no machine-readable source" (only with recorded evidence).

Tooltips may add detail but never carry the only explanation.

### Stop the stale rows (fix the cause, not the case)

- Delete the placeholder-writing block in `CollectQuota`; the collector becomes a
  pure read of persisted snapshots for the alert/history tick. Nothing writes
  `signal_quality='none'` anymore.
- Migration `0041_purge_placeholder_quota_snapshots.sql`:
  `DELETE FROM quota_snapshots WHERE signal_quality='none';` — one-time cleanup of
  rows the pre-#88 impl left behind. (Fix at the layer that owns the data, at the
  moment it is written: we stop creating the bad state rather than filtering it on
  read forever.)

### Placement

Move `<QuotaPanel />` from `SessionsBoard` into `Sidebar` `SidebarFooter`, pinned
above the Settings button (mirror `RestartToUpdateRow`). Collapsible. A persisted
`ui-store` flag (`ao.quota.visible`, default on) + a Settings `Switch` toggle
show/hide it.

### Alerting

`lowQuotaAlerts` already reads persisted `quota_snapshots` via `CollectQuota`;
because probes persist real rows, alerts key off probe data with no change beyond
a regression test.

### Reuse (keep each fact in one place)

Lift the pure codex `rate_limits` parsing (`codexRateLimits`/`codexRateWindow`
types + `snapshot`) out of `internal/cli/usage_extract.go` into a shared
`codexrollout` package used by both the Stop-hook path and the daemon probe. The
cwd-keyed `locateCodexRollout` stays in `cli` (hook-specific); the daemon uses a
cwd-independent "newest rollout with rate_limits" locate in the shared package.

## Smaller alternatives considered & rejected (Rule 9)

- **Filter stale rows on read only** — leaves the bad-state generator in place;
  every future tick re-creates the garbage. Rejected: fix the cause (stop writing
  `none`) + a one-time purge migration.
- **New `probe_status` table** — probe status is ephemeral (rebuilt each daemon
  start from live probes); persisting it adds a migration + store surface for no
  durability benefit. Rejected in favor of in-memory status on the prober.
- **Fold probing into the 30s Observer tick** — would run `claude -p` every 30s
  (real quota cost) unless per-prober due-time logic is threaded through the
  cost/alert collector, muddying two concerns. Rejected for a dedicated
  hourly-cadence `QuotaProber` that also owns force-probe + single-flight.
- **Two separate codex/codex-fugu chips** — they share one usage pool and one
  `CODEX_HOME`; two chips would show identical data. Rejected for one combined
  codex chip (ticket decision default).
- **Duplicate the rollout parser in the daemon** — two copies that must agree
  will drift. Rejected for the shared `codexrollout` extraction.

## Phases

0. Reproduce-first: failing test — collector must not write `none` placeholders.
1. Backend: stop writing placeholders; `CollectQuota` → pure read; migration 0041.
2. Backend: shared `codexrollout` package + codex prober (cwd-independent).
3. Backend: claude-code prober (pure parse + bounded env-scrubbed subprocess) +
   `QuotaProber` component (schedule, single-flight, in-memory status, persist).
4. API: `ProbeStatuses` on `MetricsResponse` + `POST /metrics/probe`; wire prober
   into daemon + controller; regen OpenAPI + TS.
5. Frontend: QuotaPanel honest-states rewrite + probe-now; relocate to sidebar
   footer; settings toggle + ui-store flag; tests.
6. Alerting regression coverage; full CI.

## Non-goals (per ticket)

Per-account/multi-login tracking (#93 — schema must not preclude it;
`account_id` already exists; build single-account). Usage-based scheduling. New
notification channels.
