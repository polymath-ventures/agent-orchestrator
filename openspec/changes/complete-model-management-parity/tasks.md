## 1. Freeze the port boundary

- [x] 1.1 Record the reference-to-current file map for all 25 verified gaps and the 11 history-mined edge cases, including the exact reference `file:line` evidence in the implementation plan and PR body.
- [x] 1.2 Identify and preserve the current tree's verified-better behaviors and exclude issue #9's `backend/internal/cli/slack.go` and notify-channel delivery surfaces from the diff.
- [x] 1.3 Add a failing baseline test proving catalog failures are not empty-success, and verify the existing process-tree/pre-prompt liveness and hidden-config/effective-prompt regression tests before changing their seams.

## 2. Port the authoritative model engine and API

- [x] 2.1 Port the catalog, request-cache, bounded-concurrency, pin-verdict TTL/eviction, and three-way validation core from reference `backend/internal/service/agent/service.go:341-613,728-745`, replacing static-primary catalogs with an adapter catalog capability and visible fallbacks.
- [x] 2.2 Add failing then passing tests for config-save validation, cache-only fail-open spawn validation, definitive cached rejection, per-pin timeout independence, force refresh, configured-pin merging, and bounded eviction.
- [x] 2.3 Port `GET /agents/models` with `force` support from reference controller/spec-generator seams, enrich the read model with model labels, efforts, defaults, verification/fallback metadata, and register telemetry templates.
- [x] 2.4 Regenerate OpenAPI and TypeScript schema output from the controller/spec generator; do not hand-edit generated API artifacts.

## 3. Restore harness-native discovery and safe probes

- [x] 3.1 Add a plain Codex app-server `initialize` → `initialized` → `model/list` catalog capability with parser/handshake tests and preserve native reasoning efforts.
- [x] 3.2 Port the reference Codex validation probe flags, HTTP-verdict classification, per-probe 45-second timeout, `WaitDelay`, and process-group descendant cleanup with regression tests for TUI-only flag rejection and leaked child processes.
- [x] 3.3 Implement Claude maintained aliases and per-process effort, ensure save/catalog paths issue no paid prompts, and port the hermetic JSON-envelope probe contract for explicit validation/runtime paths.
- [x] 3.4 Implement OpenCode `models --refresh --verbose` discovery, provider/model variant parsing, visible cached fallback, and explicit `--model`/`--variant` launch and restore flags.
- [x] 3.5 Implement Codex-Fugu catalog loading from `~/.codex/fugu.json`, supported reasoning levels, `max`→`xhigh` normalization, visible known fallback rows, and long-running execution semantics.
- [x] 3.6 Restore Fugu-first provider classification and add cross-provider compatibility regression coverage without moving fork-only harness construction into the upstream-candidate portion of the change.

## 4. Fit enforcement, revalidation, and health into current seams

- [x] 4.1 Wire the shared three-way validator into project config save without paid Claude prompts and into session creation before any session row, runtime, or worktree is created.
- [x] 4.2 Wire cache-only `ValidateSpawnModel` into the resolved launch path so only a fresh definitive rejection blocks and missing/stale/unknown verdicts proceed with a loud warning.
- [x] 4.3 Port model transition behavior from reference `backend/internal/service/modelhealth/monitor.go:101-164`: preserve prior real state across unknown probes, alert on born-unreachable/recovered transitions, remove unconfigured pins, and use `AO_MODEL_REVALIDATION_INTERVAL` with 24-hour default and zero-disable.
- [x] 4.4 Keep model transition data typed with project, harness, model, and scope while emitting intents only; do not change notification-channel delivery owned by issue #9.
- [x] 4.5 Port `/agents/health` and the install/auth monitor with `AO_AGENT_HEALTH_INTERVAL`, transition logging/callbacks, actionable `missing`/`unauthorized` remedies, advisory `unknown`, and no session-notification persistence.
- [x] 4.6 Preserve process-tree session liveness and the pre-prompt liveness check; add explicit tests for tmux reporting `sh` and for immediate agent exit.

## 5. Replace static UI model inputs with one live picker

- [x] 5.1 Port and extend reference `ModelAvailabilityField.tsx:22-131` plus its query hook to render models, efforts, status/reason, checked time, refresh, and visible fallback provenance from `/agents/models`.
- [x] 5.2 Replace `MODEL_HARNESS_OPTIONS` and hardcoded rows with the canonical dynamic agent/model registry, including Codex-Fugu and synthetic configured pins without model-capable catalog rows.
- [x] 5.3 Add a reusable harness/model/effort row for project, worker, orchestrator, prime, and reviewer configuration; on harness switch restore that harness's saved pair or clear stale cross-provider values.
- [x] 5.4 Replace the worker-mix model text input while preserving the current standalone component, numeric parsing, exponent validation, and live totals; explicitly carry or inherit effort according to the resolved launch contract.
- [x] 5.5 Add an optional harness-native model/effort override to create-project while preserving blank daemon-owned defaults and preventing a scalar model from leaking across harnesses.
- [x] 5.6 Add frontend tests for dynamic harness discovery, model/effort filtering, role persistence, harness-switch clearing, cached fallback, explicit create override, hidden-config preservation, and existing worker-mix parsing.

## 6. Verify parity and prepare review

- [x] 6.1 Run focused Go, frontend, controller, adapter, storage, and generated-schema tests after each phase; update the delta-spec checkboxes only when each behavior is verified.
- [ ] 6.2 Exercise every history-mined edge case from issue #4 and cite the passing test or current invariant in the PR body, including the rejected `--skip-probe` escape hatch and independent per-probe deadlines.
- [x] 6.3 Run `ao preview` and capture desktop and narrow-panel verification of project settings, worker mix, create-project, refresh failure with cached data, and unverified fallback states.
- [ ] 6.4 Run the repository's full CI commands, rebase on the fetched remote default branch, push the final head, and complete independent cross-family final review before declaring the PR merge-ready.
