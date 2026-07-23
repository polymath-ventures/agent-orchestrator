## Why

Prime settings are now fleet-owned, but the global Settings UI still uses a
one-off form that asks operators for raw Agent, Model, Effort, wake duration,
rules, and rules-file values. That diverges from the project settings and
first-run setup flows that already understand harness catalogs, model
availability, effort options, and manual model fallback warnings.

The previous fleet Prime migration warning also no longer matches the restarted
product model. Operators should configure Prime directly through persisted
fleet settings; legacy project/environment state should be removed from the
settings, API, CLI, and config surfaces instead of explained in the UI.

## What Changes

- Reuse the shared harness/model/effort picker behavior for Prime in global
  Settings, including the known-model dropdown, custom model path, and warning
  used by other role selectors.
- Rename Prime's runtime selector from Agent to Harness.
- Present Prime wake interval as minutes in the UI while saving the existing
  backend `wakeInterval` duration field.
- Validate Prime wake intervals below 1 minute and above 360 minutes before
  accepting settings.
- Rename Prime inline rules copy toward instructions/rules content and rename
  the file field as an instructions file path.
- Explain rules assembly order consistently: inline content loads first and
  configured file content is appended after it.
- Clarify project-scoped role instructions file paths as repo-relative or
  absolute, and fleet Prime instructions file paths as absolute.
- Remove legacy Prime environment/project warning state from UI, API, CLI, and
  config surfaces.
- Extract shared UI primitives where useful so setup, project settings,
  reviewer config, worker mix, and Prime do not duplicate model and
  instructions controls.

## Non-Goals

- Moving Prime into project startup.
- Changing Prime supervision lifecycle or wake backoff semantics.
- Rewriting every settings page.
- Changing the prompt assembly order.
- Preserving the legacy Prime project migration UI.
