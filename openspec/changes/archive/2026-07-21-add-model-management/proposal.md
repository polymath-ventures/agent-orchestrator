## Why

AO can already choose an agent harness per project and role, but it does not yet
own the model-selection layer that makes those choices safe, restorable, and
observable. Operators need per-harness model defaults, per-session model pins,
fail-loud provider mismatch validation, and honest model availability state
before spawning work across mixed providers at scale.

## What Changes

- Add a model provider classifier and harness compatibility contract so
  cross-provider pins fail with a 400 before worktree creation.
- Add per-harness model defaults to project/agent configuration, including a
  configured cheap-default for unpinned `claude-code` spawns.
- Add a per-session model override to the spawn API, CLI, persistence layer, and
  restore path so a pin applies to exactly one session and survives restore.
- Add model availability probing with explicit reason codes, cached pin
  verdicts, a fail-open spawn gate with no network I/O on the spawn path, and
  background revalidation notifications.
- Add reviewer model pins as a delta on the existing `reviewerHarness`
  configuration.
- Add a focused settings UI delta where upstream already edits project settings;
  do not transplant the old fork's full settings form.
- Keep `codex-fugu` as a fork boundary: provider classification must recognize
  fugu model names when the model catalog exists, but fugu spawnability remains
  covered by the separate fork-only `codex-fugu-harness` capability.

## Capabilities

### New Capabilities

- `model-management`: Per-harness model defaults, per-session model overrides,
  compatibility validation, availability state, and reviewer model pins.

### Modified Capabilities

- None.

## Impact

- Backend domain configuration, provider classification, spawn resolution,
  session persistence, model availability service, background monitor,
  notification intents, and generated API schema.
- CLI spawn flags and project configuration surfaces.
- Frontend spawn controls and the existing project settings form.
- Harness adapters for Claude Code and Codex model validation probes.
- Reviewer configuration and review-run launch resolution.
