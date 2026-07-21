## Why

The first model-management implementation merged in PR #34, but a verified
comparison against the reference fork found that it omitted the closed-loop
catalog, validation, spawn-gate, revalidation, and dynamic picker behavior that
made the original feature reliable. Issue #4 was reopened to restore that
parity while retaining improvements already present on `main`.

## What Changes

- Replace the gap-listed model-management regions with the reference
  implementation using the binding PORT → FIT → IMPROVE → FIX strategy:
  transplant the proven behavior, adapt only current-tree seams, preserve
  verified-better current behavior, then apply the operator's refinements.
- Make one model service own harness catalogs, three-way validation verdicts,
  TTL-bounded cached pin state, config-save validation, the cache-only spawn
  gate, request-path refresh, and scheduled revalidation.
- Expose a force-refreshable model catalog/availability API and drive every
  model and effort picker from the dynamic harness registry rather than
  hardcoded harness rows or bare text inputs.
- Restore hermetic, provider-aware probe behavior, including independent
  per-probe timeouts and process-tree cleanup, while keeping uncertain
  infrastructure/auth failures fail-open.
- Apply harness-native discovery and invocation rules: maintained Claude
  aliases without paid save-time probes, queried OpenCode provider/model
  variants with cached fallback, and installed Codex-Fugu catalog data with
  `max` normalized to `xhigh`.
- Preserve visible, recorded fallback behavior and treat the installed harness
  as the final authority. Do not silently substitute both model and effort.
- Keep the current tree's verified improvements and the issue's explicit
  non-goals: no capacity dashboard, tracker-label routing, behavior-version
  convergence, attention system, usage-based scheduling, or two-way Slack
  integration.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `model-management`: Reconcile the existing capability with the reopened
  parity contract, including authoritative catalogs, enforcement points,
  dynamic UI surfaces, harness-specific discovery, and incident-derived
  liveness and failure semantics.

## Impact

- Backend agent adapters, agent/model services, configuration validation,
  session creation, daemon wiring, notifications, storage, and API schema.
- CLI/API model catalog and refresh surfaces.
- Electron/React project creation, settings, role, reviewer, and worker-mix
  model selectors.
- New additive migration(s) and regenerated sqlc/OpenAPI/TypeScript outputs as
  required; already-merged migrations remain untouched.
- Implementation must cite the lifted reference files and demonstrate each
  history-mined edge case from issue #4 in tests or record why it no longer
  applies.
