## Why

First-run project setup selects runtime harnesses for the worker and
orchestrator, but the dialog currently labels those choices as agents and hides
reviewer configuration until later settings. Its model picker is also framed as
one optional override, even though AO now persists harness-specific model
defaults.

Operators need the setup dialog to preview the harnesses and default models that
will be saved before they create and start a project.

## What Changes

- Rename first-run runtime selections from agent terminology to harness
  terminology while preserving role labels for worker, orchestrator, and
  reviewer.
- Add an optional reviewer harness selector with an automatic independent
  reviewer default.
- Present model configuration as harness-specific default model rows for the
  concrete harnesses selected by worker, orchestrator, or reviewer.
- Keep manually entered model IDs possible and show that launch may fail if the
  selected harness rejects the model.
- Separate model catalog refresh from harness availability refresh copy and
  controls.
- Persist selected role harnesses, optional reviewer harness, and
  `agentConfig.modelByHarness` only for selected harnesses with configured
  values.

## Non-Goals

- Full app-wide terminology migration.
- Reviewer execution behavior changes.
- Backend validation or storage shape changes.
- Prime/global settings UI unification.
