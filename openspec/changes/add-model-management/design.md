## Context

AO already has the first layer of model-aware spawning on `main`: session rows
carry `model`, spawn requests accept `model`, worker-mix buckets include a model
axis, and `domain.ClassifyModelProvider` can distinguish Anthropic and OpenAI
model families. The remaining model-management work is to turn those pieces into
one coherent contract across project config, spawn resolution, restore,
availability state, reviewer launches, and UI/API surfaces.

The old fork at `~/agent-orchestrator-fscked` provides useful reference code for
provider compatibility, per-harness model maps, resolver precedence, and
availability probing. The clean fork has since gained worker-mix and quota work,
so this port should not copy the old fork wholesale. It should lift the
behavioral contracts and adapt them to the current package boundaries.

## Goals / Non-Goals

**Goals:**

- Make model resolution harness-aware for workers, orchestrators, worker-mix
  buckets, per-session overrides, and reviewers.
- Reject known cross-provider model pins before any spawn worktree or session is
  created.
- Keep restore faithful to the model the session actually launched with.
- Expose model availability as cached, reason-coded state without adding network
  I/O to the spawn path.
- Keep the settings UI as a focused delta on the existing form.
- Preserve upstream-candidate cleanliness by keeping fork-only `codex-fugu`
  spawnability out of this change.

**Non-Goals:**

- Usage-based scheduling, automatic throttling, or quota-aware routing.
- A full external model catalog service.
- Moving `codex-fugu` into an upstreamable feature; that remains covered by the
  separate fork-only spec.
- Replacing the current worker-mix selector.

## Decisions

### Use one shared resolver for all launch surfaces

Introduce a small shared resolver near the session manager boundary that accepts
base project agent config, role/reviewer override config, an optional per-spawn
model, and the resolved harness. It returns the adapter-facing
`ports.AgentConfig` plus a typed mismatch error for incompatible explicit pins.

Alternative considered: keep adding local `effectiveModel` branches in the
session manager, reviewer launcher, and future model-health code. That would
duplicate precedence rules and make scalar cross-provider leakage likely to
return in one path.

### Add per-harness model maps to `domain.AgentConfig`

Extend `AgentConfig` with `modelByHarness`, whose values carry model and
reasoning effort. Validation should reject unknown harness keys, invalid effort
values, whitespace-padded models, and known provider mismatches. The existing
scalar `model` remains as a compatibility fallback, but resolution applies it
only when compatible with the resolved harness.

Alternative considered: make `model` a map-only field and migrate away the
scalar. That would be a larger config break than the problem needs; scalar
config is useful for single-harness projects and already exists in API/UI data.

### Keep cheap defaults data-driven where the repo already has config shape

Define the `claude-code` default model in one model catalog/config source rather
than as scattered Go literals. The first implementation can be a checked-in
catalog file consumed by the resolver and settings UI; adding a dynamic catalog
service is out of scope. Explicit user choices always win, including explicit
expensive models.

Alternative considered: preserve the old fork's hardcoded
`DefaultClaudeCodeModel` constant. That works but repeats the old smell the
issue calls out and makes the UI learn defaults from a second place.

### Validate before the seed row

The session manager should resolve and validate the `(harness, model)` pair
before creating the seed session row or materializing a workspace. HTTP maps the
typed mismatch error to a 400 that names the bad model and target harness.
Project-config validation catches saved bad defaults; spawn validation catches
per-request overrides and legacy/stale config that predates the validator.

Alternative considered: let adapters reject bad models. That fails too late:
some CLIs hang or launch interactively, and by then AO has already created
durable state it must roll back.

### Availability probes are advisory and cached

Add an internal model-health service that enumerates configured pins through the
shared resolver, asks adapters that implement model validation to probe them,
and stores cached verdicts with explicit reason codes. Spawn consults only the
cache and remains fail-open on missing, stale, or probe-unavailable verdicts.
Background revalidation owns provider/network calls and emits transition
notifications.

These probes are real CLI model calls. They run outside the spawn path, but
operators should expect them to consume provider quota or billable usage for
each configured pin on each refresh interval.

Alternative considered: probe every explicit spawn synchronously. That gives
fresh answers but turns provider/network health into spawn latency and failure
surface, violating the issue's no-network spawn-path requirement.

### Keep adapter model probes narrow

Claude Code and Codex adapters should expose a model-validation capability that
returns reachable, unreachable, or probe-unavailable. The Claude probe should
use the old fork's JSON envelope contract with operator hooks disabled. Codex
should use its adapter-equivalent probe. HTTP/provider/auth/rate-limit failures
map to probe-unavailable; provider 400/404/422 style model failures map to
unreachable.

Alternative considered: classify availability from model names alone. Names can
catch provider families, but they cannot prove account-specific access or
regional availability.

### Model reviewer pins as reviewer config, not a second reviewer map

Extend `ReviewerConfig` with an `AgentConfig` delta. Review launch resolution
uses the same base-plus-override resolver with the reviewer harness converted to
an agent harness. This keeps reviewer model pins a small delta on existing
`reviewerHarness` behavior.

Alternative considered: add a separate `reviewerModelByHarness` top-level map.
That splits harness and model for the same reviewer and adds a second precedence
scheme clients would need to learn.

### UI changes stay inside `ProjectSettingsForm`

The current settings form already edits project agent defaults, reviewers,
worker mix, intake, and hidden-config preserving PUTs. Add compact model-map,
reviewer model, and availability affordances there. Avoid copying the old
fork's large settings form, which would conflict with newer upstream structure
and increase rebase cost.

Alternative considered: build a replacement settings page. That would ship more
UI than the issue needs and risk dropping newer upstream settings fields.

## Risks / Trade-offs

- Provider classifiers can be over-eager -> keep boundary-aware matching and
  permit unknowns.
- Cached availability can be stale -> report timestamps and reason codes, and
  keep spawn fail-open unless the cached state is only advisory.
- CLI probe output can change -> fixture probes and map ambiguous output to
  `probe-unavailable`, not unreachable.
- Adding `modelByHarness` changes API schema -> regenerate OpenAPI and
  TypeScript together and preserve unknown hidden config in settings saves.
- The current branch already contains partial model work -> start
  implementation with an audit phase and delete duplicated old-fork concepts
  rather than adding parallel code.

## Migration Plan

1. Add or reuse migrations for any new persisted model-health state; never edit
   already-applied session migrations.
2. Extend typed project config with additive JSON fields. Existing configs
   continue to decode with zero values.
3. Regenerate API and frontend schema after DTO/config changes.
4. Ship with cached availability empty on upgrade; the first background
   revalidation fills `not-probed` pins without blocking spawns.
5. Rollback by disabling the background monitor and leaving additive JSON fields
   ignored by older resolution code.

## Open Questions

None. The issue defines the behavior and split-out fugu boundary; implementation
can choose the smallest repo-compatible insertion points during planning.
