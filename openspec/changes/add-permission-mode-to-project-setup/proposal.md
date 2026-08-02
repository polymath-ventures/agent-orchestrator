## Why

Project permission mode can be changed after creation, but first-run setup cannot set it. The initial orchestrator therefore starts with the harness default even when the operator needs a deliberate mode such as bypass permissions, and recovering requires a difficult restart through project settings.

## What Changes

- Add a permission-mode control to first-run project setup using the same choices and labels as project settings.
- Persist the selected mode in the new project's project-level `agentConfig.permissions` so the first orchestrator launch receives it.
- Preserve the existing default behavior when the operator leaves the setup selection at the project default.

## Capabilities

### New Capabilities

- `project-permission-mode`: Project creation and project settings expose one consistent project-level permission-mode configuration that applies to launched roles, including the initial orchestrator.

### Modified Capabilities

None.

## Impact

- First-run project setup UI and its selection payload.
- Project creation config assembly in the web-first React supervisor.
- Focused frontend tests for setup submission and project-config persistence.
- No daemon API or storage change; `ProjectConfig.agentConfig.permissions` already exists and is validated and consumed by the backend.
