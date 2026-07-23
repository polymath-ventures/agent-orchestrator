## Context

`PrimeSection` currently owns bespoke form fields for `agent`,
`agentConfig.model`, `agentConfig.effort`, `wakeInterval`, `rules`, and
`rulesFile`. Project settings and first-run setup already have better
harness/model primitives through `ModelAvailabilityField` and surrounding
selector helpers. Reusing those primitives keeps Prime aligned with model
catalog behavior and avoids teaching operators backend-specific duration or
runtime terms.

The stored Prime settings shape can remain stable. The UI can convert between
minutes and the existing Go duration string, and the backend can continue to
validate and persist `wakeInterval` as a duration. The legacy environment
migration state is dead surface for this product shape and should be removed at
the owner layer instead of hidden by frontend copy.

## Decisions

### Keep storage shape and convert wake interval at the UI/API edge

Prime settings continue to persist `wakeInterval` as the existing duration
string. The UI renders a bounded integer minute input and serializes it back to
`<n>m` on save. Backend validation rejects values below 1 minute and above 360
minutes so non-UI clients get the same contract.

Alternative considered: add a second `wakeIntervalMinutes` field. That would
make the UI simpler but duplicate one fact in the API and storage contract.

### Share model-selection behavior, not a Prime-specific clone

Prime should use the same harness-aware model and effort control behavior as
project role and setup flows. If extracting a thin wrapper around
`ModelAvailabilityField` removes duplication, do that; otherwise compose the
existing field directly in `PrimeSection`.

Alternative considered: copy the model picker into `PrimeSection`. That would
ship fast but would keep Prime drifting whenever model availability behavior
changes.

### Remove legacy Prime migration state at the API owner

Delete the legacy Prime environment/project warning from the service DTO and
generated API schema, then remove the CLI/UI presentation paths that consumed
it. `AO_PRIME_PROJECT_ID` should not reactivate Prime, and the old warning no
longer needs a replacement operator flow.

Alternative considered: leave the API field and only hide the frontend warning.
That keeps dead compatibility surface alive and invites clients to keep
depending on it.

## Risks / Trade-offs

- UI conversion can drift from backend validation. Add focused frontend tests
  for minute conversion and backend tests for duration bounds.
- Removing API fields is a compatibility break for any client still reading the
  migration warning. The issue explicitly treats this as dead surface; keep the
  removal tight and regenerate the schema.
- Shared controls can grow too broad. Prefer a small component or helper that
  matches existing settings patterns over a new generic framework.

## Open Questions

None. The issue's decision defaults settle the path labels, wake interval
storage shape, custom model fallback behavior, and legacy-surface removal.
