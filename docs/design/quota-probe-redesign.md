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

### Source of record = daemon probes, over the existing harness surface

**Reuse the canonical harness registry — do not hardcode a harness list.** The
`model-management` spec already mandates that consumers use the dynamic harness
registry, and sibling ticket #98 applies that same contract to the setup dialog.
#97 applies it to quota. Concretely:

- Quota probing is a new **optional adapter capability port**
  `ports.AgentQuotaProber` (parallel to `AgentModelValidator` /
  `AgentModelCatalog` / `AgentBinaryResolver`). Each adapter that can report
  usage implements it; adapters that can't simply don't.
- The daemon `QuotaProber` component (started like the metrics Observer)
  enumerates harnesses via the agent `Service` inventory
  (`adapters/agent/registry.Harnessed()` gated by `Service` `Installed` /
  `Authorized`), capability-casts each adapter to `AgentQuotaProber` (the same
  cast pattern `service/agent.probeAgent` already uses for
  `AgentBinaryResolver`), and probes only harnesses that are both available and
  implement the port. No hardcoded `[claude-code, codex]` list anywhere;
  codex-fugu falls out of the registry automatically.

Per-adapter probe implementations:

- **claude-code adapter**: `claude -p "/usage"`, bounded
  (`context.WithTimeout`), env scrubbed of `AO_SESSION_ID` / `AO_RUNTIME_TOKEN` /
  `AO_RUN_FILE`, stdin closed, process-group cleanup — mirroring the existing
  bounded-probe pattern in `adapters/agent/claudecode/claudecode.go`
  `ValidateModel`. Parse the `N% used · resets <time>` lines. Costs a small quota
  turn → **hourly** cadence.
- **codex / codex-fugu adapters**: passive read of the newest rollout
  `rate_limits` event under `CODEX_HOME/sessions/**` (cwd-independent), via the
  shared `codexrollout` package. Zero cost → probes freely. codex-fugu shares
  `CODEX_HOME` with codex (one adapter family, `NewFugu`), so the two report the
  same pool; the widget shows a single **combined codex chip** (ticket decision
  default). Verify the shared-home assumption in Phase 2 and note it in the PR.

Cadence: probe-all on daemon start, then hourly when idle. Force-probe via
`POST /api/v1/metrics/probe` (optional `{harness}`) and a widget button.
Single-flight per harness (per-harness lock; force-probe skips if in flight).

Real snapshots are **persisted to `quota_snapshots`** (upsert), so they feed
alerts + history and render immediately on load. Probe _status_ (not_probed /
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

### Placement + harness labels from the shared inventory

Move `<QuotaPanel />` from `SessionsBoard` into `Sidebar` `SidebarFooter`, pinned
above the Settings button (mirror `RestartToUpdateRow`). Collapsible. A persisted
`ui-store` flag (`ao.quota.visible`, default on) + a Settings `Switch` toggle
show/hide it.

The widget derives its harness set and human labels from the **same
`useAgentsQuery` inventory the pickers use** (labels = adapter `Manifest.Name`),
reusing `AGENT_OPTIONS`/`AgentProvider` only for the id vocabulary — no parallel
hardcoded harness list. This keeps it aligned with #98's picker-registry work.

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

## Coordination with #98 (harness/model surface)

#98 ("clarify project setup harness and model defaults", Codex-owned, not yet
landed) reworks the frontend setup dialog to use the dynamic harness registry.
It is **not** the source of the lookup surface — that surface already exists in
`main` (`registry.Harnessed()`, the agent `Service`, `useAgentsQuery`) and is
already required by the archived `model-management` spec. #97 does not depend on
#98 and does not wait for it. Both build on the same functions, so they converge
and stay rebase-friendly. #97 must not touch #98's files
(`CreateProjectAgentSheet.tsx`, `ModelAvailabilityField.tsx`, `_shell.tsx`);
rebase when #98 lands.

## Phases

0. Reproduce-first: failing test — collector must not write `none` placeholders.
1. Backend: stop writing placeholders; `CollectQuota` → pure read; migration 0041.
2. Backend: shared `codexrollout` package + `AgentQuotaProber` capability port;
   codex/codex-fugu adapters implement it (cwd-independent rollout read).
3. Backend: claude-code adapter implements `AgentQuotaProber` (pure parse +
   bounded env-scrubbed subprocess); `QuotaProber` daemon component drives it off
   the agent `Service` inventory (schedule, single-flight, in-memory status,
   persist real snapshots). No hardcoded harness list.
4. API: `ProbeStatuses` on `MetricsResponse` + `POST /metrics/probe`; wire prober
   into daemon + controller; regen OpenAPI + TS.
5. Frontend: QuotaPanel honest-states rewrite + probe-now; harness set/labels from
   `useAgentsQuery`; relocate to sidebar footer; settings toggle + ui-store flag;
   tests.
6. Alerting regression coverage; full CI.

## Phase 2 findings

**codex-fugu shares CODEX_HOME with codex (verified).** Reading the codex
adapter (`backend/internal/adapters/agent/codex/`), `NewFugu()` parameterizes
only the binary name, manifest, and hook token; it never sets or overrides
`CODEX_HOME`, and no launch/hook/env path in the package points fugu at a
separate home (`grep -rn CODEX_HOME internal/` finds only the CLI extractor and
comments). Fugu's own catalog read (`catalog.go readFuguCatalog`) loads
`~/.codex/fugu.json`, confirming fugu treats `~/.codex` as its home. The
codex-fugu binary is a wrapper around the same codex binary and writes its
rollouts under the same `CODEX_HOME/sessions/**`.

**Choice made: one combined codex chip.** `ProbeQuota` is implemented once on
the shared `Plugin` type (`quota.go`), so both `New()` and `NewFugu()` satisfy
`ports.AgentQuotaProber`. It resolves the home identically for both (env
`CODEX_HOME` else `~/.codex`) and tags every snapshot with `domain.HarnessCodex`
(the `codexrollout.RateLimits.Snapshots` parser hardcodes that harness). Because
the two adapters therefore read the same pool and report identical
codex-tagged data, the daemon/widget collapse them into a single codex signal;
no separate `codex-fugu` chip carrying duplicate data is emitted. This is the
simplest correct realization of the ticket decision default — it required no
fugu-specific probe code and no home divergence to reconcile.

## Non-goals (per ticket)

Per-account/multi-login tracking (#93 — schema must not preclude it;
`account_id` already exists; build single-account). Usage-based scheduling. New
notification channels.
