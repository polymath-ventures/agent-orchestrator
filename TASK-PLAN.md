# Issue 4 Implementation Plan

## Phase 1: Harness-aware model resolution

Done when project config supports per-harness model/effort entries, resolution
uses one shared cascade for worker/orchestrator spawns and restore, known
cross-provider explicit pins fail before session/worktree creation, and
claude-code unpinned launches receive the configured default.

Primary files: `backend/internal/domain/agentconfig.go`,
`backend/internal/domain/projectconfig.go`, `backend/internal/session_manager/manager.go`,
session manager/domain tests.

## Phase 2: Adapter launch flags

Done when Codex launch/restore receive `--model` and reasoning effort from
resolved config, Claude Code remains compatible, and tests pin launch argv.

Primary files: `backend/internal/adapters/agent/codex/*`,
`backend/internal/adapters/agent/claudecode/*`.

## Phase 3: Reviewer model pins

Done when reviewer config carries an agent-config delta, reviewer launches pass
the resolved model to adapters, and cross-provider reviewer pins fail config
validation. A parallel worker owns this slice.

Primary files: `backend/internal/domain/projectconfig.go`,
`backend/internal/review/*`, `backend/internal/adapters/reviewer/*`.

## Phase 4: Model availability

Done when configured model pins enumerate through the shared resolver, adapter
model probes produce reason-coded cached availability, spawn reads only cached
state, and background revalidation emits transition notification intents.

Primary files: `backend/internal/ports/agent.go`,
`backend/internal/service/agent` or `backend/internal/service/modelhealth`,
adapter probe implementations, notification/storage/API files.

## Phase 5: UI/API polish and verification

Done when generated schemas are refreshed, `ao spawn --model`/docs are correct,
project settings can edit model-management fields without dropping hidden
config, availability reason codes render honestly, and backend/frontend gates
pass.

## Merit

Smallest shape: reuse the existing session `model` column, worker-mix model
axis, provider classifier, candidate-health style transition pattern, and
settings form. Avoid a central dynamic model catalog service or a replacement
settings page until a measured need appears.
