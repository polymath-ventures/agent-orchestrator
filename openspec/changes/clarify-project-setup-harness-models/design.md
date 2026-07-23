## Design

Use the existing `ProjectConfig` shape. First-run setup will emit:

- `worker.agent` for the selected worker harness.
- `orchestrator.agent` for the selected orchestrator harness.
- `reviewers[0].harness` only when the operator selects a concrete reviewer
  harness.
- `agentConfig.modelByHarness` entries for selected concrete harnesses with a
  non-empty model or effort value.

The create sheet keeps a small local map keyed by harness id. The selected
harness list is derived from worker, orchestrator, and reviewer selections,
deduped in display order. Switching role harnesses does not delete a saved row
for a still-selected duplicate harness, and unselected harness entries are
filtered out by the payload builder.

Reviewer automatic behavior stays owned by the existing domain resolver. The UI
therefore stores no reviewer config for the automatic choice and labels the
empty reviewer value as "Automatic independent reviewer."

The model rows reuse `ModelAvailabilityField` with the harness fixed per row.
Manual model IDs remain ordinary text input values; a small notice below each
row tells operators that launch may fail if the harness rejects a manually
entered model.

## Alternatives

- Add role-level model defaults in the create flow. Rejected because the issue
  asks for project-level defaults by selected harness, and the current create
  payload already supports `agentConfig.modelByHarness`.
- Reuse the full settings form. Rejected because first-run setup needs a compact
  path and most settings fields are unrelated to creation.
