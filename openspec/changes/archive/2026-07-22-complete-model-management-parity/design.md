## Context

PR #34 established the first model-management slice, then issue #4 was reopened
after a code-to-code comparison found 25 concrete gaps against the accumulated
reference implementation in `~/agent-orchestrator-fscked`. The issue's final
operator ruling supersedes the earlier greenfield plan: implementation follows
PORT → FIT → IMPROVE → FIX. The reference is the regression corpus; the current
tree supplies newer seams and several behaviors that must not be displaced.

The reference port manifest is intentionally falsifiable:

- Catalog, cache, and three-way validation engine:
  `backend/internal/service/agent/service.go:341-613,728-745`.
- Spawn/config enforcement seams:
  `backend/internal/session_manager/manager.go:1005-1031` and
  `backend/internal/service/project/service.go:703-737`.
- Codex probe isolation and provider-verdict parsing:
  `backend/internal/adapters/agent/codex/codex.go:226-356`.
- Claude JSON-envelope and hermetic probe contract:
  `backend/internal/adapters/agent/claudecode/claudecode.go:353-510` plus
  `backend/internal/adapters/agent/claudecode/probe_unix.go:12-23`.
- Model transition monitor:
  `backend/internal/service/modelhealth/monitor.go:101-164`.
- Typed model notification wiring:
  `backend/internal/daemon/model_health_wiring.go:127-151`.
- Dynamic settings picker:
  `frontend/src/renderer/components/ModelAvailabilityField.tsx:22-131` and
  the harness/model form regions in
  `frontend/src/renderer/components/ProjectSettingsForm.tsx:547-675,958-1020`.
- Agent install/auth monitor:
  `backend/internal/service/agenthealth/monitor.go`.

The current tree already contains useful model resolution, session persistence,
API schemas, cached DB rows, UI wiring, effective-prompt inspection, hardened
rules confinement, and unrelated lifecycle improvements. The port replaces only
gap-listed regions. It must not touch `backend/internal/cli/slack.go` or notify
channel delivery surfaces while issue #9 is active.

## Goals / Non-Goals

**Goals:**

- Restore one closed-loop model engine that owns catalogs, validation verdicts,
  bounded cached pin state, config validation, the cache-only spawn gate,
  request refresh, and scheduled revalidation.
- Port proven probe and transition behavior near-verbatim, adapting only the
  package, registry, persistence, and API seams in the current tree.
- Make model/effort selection dynamic across project creation, settings, roles,
  reviewers, and worker-mix buckets.
- Apply the operator refinements that the reference lacks: alias-driven Claude
  without paid save probes, query-driven OpenCode variants, installed-catalog
  Codex-Fugu behavior, explicit recorded fallbacks, and installed-harness
  authority.
- Prove the incident-derived edge cases in tests or record why a case is
  structurally unaffected by the port.

**Non-Goals:**

- Capacity dashboards, usage-based scheduling, quota routing, tracker-label
  routing, behavior-version convergence, or an attention system.
- Two-way Slack or notification-channel delivery work.
- Replacing current effective-prompt, rules-confinement, SSE, drain, or prime
  hardening with older reference code.
- Inventing a provider-neutral model or effort vocabulary.

## Decisions

### Port the model service as the owner of model truth

Lift the reference service's catalog aggregation, five-minute request cache,
four-way bounded probing, 48-hour pin-verdict TTL, 256-entry cap, unconfigured
pin eviction, and three-way verdict mapping into the current service boundary.
Configured pins are merged into installed harness catalogs, and a discovery
failure returns an error plus an explicit fallback path rather than an empty
successful catalog.

Alternative considered: add missing checks to the existing DB-backed
`modelhealth` service. That would preserve two owners for the same verdict and
continue forcing callers to reconcile catalog rows with health rows. One owner
is smaller and keeps the validation property true by construction.

### Keep three enforcement points behind one verdict contract

Config save, session spawn, and background revalidation consume the same typed
reachable / unreachable / unknown contract. Save-time discovery is bounded and
fails open on missing capability, auth, rate limit, timeout, provider 5xx, or
other infrastructure uncertainty. Spawn performs zero network I/O and blocks
only on a fresh definitive provider rejection. Revalidation refreshes the cache
and emits transitions without allowing an unknown result to overwrite a prior
real state.

Claude is a deliberate refinement: save-time validation checks maintained
aliases and local capability metadata; it never issues a paid prompt merely to
approve configuration. Runtime execution remains the final validator.

Alternative considered: keep separate advisory structs at each call site. The
reference history shows that this drifted into false hard failures; a shared
typed contract prevents the error class instead of adding another detector.

### Use harness-native discovery and invocation

- Claude Code exposes maintained semantic aliases (`fable`, `opus`, `sonnet`,
  `haiku`) and per-process effort. Prefer `CLAUDE_CODE_EFFORT_LEVEL`; use
  `--effort` only when the installed CLI reports support. Do not enumerate by
  paid prompts.
- OpenCode refreshes provider/model IDs with
  `opencode models --refresh --verbose`, models efforts as native variants, and
  retains the last successful catalog when refresh fails.
- Codex-Fugu reads the installed `~/.codex/fugu.json` model catalog first,
  derives `supported_reasoning_levels`, normalizes `max` to `xhigh`, and falls
  back visibly to known `fugu`/`fugu-ultra` rows when the catalog is unavailable.
- Ordinary Codex uses the installed app-server catalog where available and its
  native reasoning-effort values.

Alternative considered: copy the reference hardcoded known sets unchanged.
Those sets are useful fallback data, but making them primary would repeat the
reference's main smell and ignore installed-harness capability.

### Preserve probe isolation exactly at the process boundary

Codex probes use `exec --skip-git-repo-check --sandbox read-only --ephemeral`
and never pass TUI-only flags. Claude probes use stdin plus a JSON result
envelope, `--setting-sources ''`, an empty MCP config, and no session
persistence. Each target receives its own 45-second timeout. Unix probes run in
a process group with `WaitDelay` and kill descendants on cancellation. Only
provider HTTP 400/404/422 is a definitive model rejection; signals,
OOM/exit -1, auth, 401/403, 408, 429, 5xx, missing status, and malformed output
are unknown and fail open.

Alternative considered: infer verdicts from exit codes and stderr substrings.
The incident history demonstrates that this misclassified CLI and
infrastructure failures as model rejection.

### Drive UI rows from the dynamic registry

Expose a force-refreshable catalog read model from `GET /agents/models`; include
harness labels, catalog source, checked time, model status/reason, supported
efforts, defaults, and whether a row is verified or fallback. Reuse one focused
availability field across creation, project/role/reviewer defaults, and worker
mix. Switching harness selects a valid default only when policy permits and
never silently downgrades an explicit model.

Alternative considered: extend the current text inputs. Text inputs cannot
express installed availability, supported effort, or fallback provenance and
would keep validation logic duplicated in clients.

### Keep health monitoring separate from session notifications

Port the reference install/auth monitor and `/agents/health` read model with an
environment-tunable interval. Missing and unauthorized transitions are
operator-health signals; unknown is advisory. Model notifications remain typed
by model subject and carry harness/model/scope. This change emits intents only
and does not alter Slack delivery.

Alternative considered: fold harness health into model rows. Installation/auth
health and model reachability have different remediation and transition keys;
combining them makes both contracts less honest.

## Risks / Trade-offs

- [Reference code assumes older package seams] → Lift behavior in cohesive
  units, adapt imports/types/wiring only, and compare ported cores directly.
- [Dynamic CLI output changes] → Preserve last successful catalogs, expose
  fallback provenance, and classify ambiguous output as unknown.
- [Probes consume time or quota] → Cache results, bound concurrency and cadence,
  and never issue Claude prompts on save or perform network work on spawn.
- [Large cross-cutting diff conflicts with concurrent work] → Avoid the issue #9
  notification-channel files, rebase before every push, and renumber only new
  migrations if main advances.
- [UI can silently rewrite config] → Preserve hidden fields and require visible
  fallback/normalization state in both payload tests and rendered verification.
- [Port regresses launch liveness] → Keep process-tree liveness before
  title/prompt delivery and add explicit regression coverage for the historical
  tmux launcher-shell failure.

## Migration Plan

1. Introduce the unified service and adapter contracts behind existing API
   surfaces, with failing tests copied/adapted from the reference first.
2. Wire config-save and cache-only spawn enforcement before removing the
   superseded health ownership; preserve existing persisted rows during the
   transition.
3. Add only new migrations needed by the final read model; do not edit merged
   migration 0031.
4. Regenerate sqlc, OpenAPI, and TypeScript clients together.
5. Replace UI text fields only after the dynamic endpoint is available; verify
   settings and creation flows visually.
6. Roll back by disabling refresh monitors and retaining additive config/data;
   older clients continue to send accepted scalar/model-map fields.

## Open Questions

None. Issue #4's final strategy revision and operator design document settle the
behavior; planning chooses only current-tree seam locations.
