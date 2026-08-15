## Why

Operators cannot currently inspect the complete starting context that a specific AO session actually received, nor identify which source contributed each paragraph. This makes prompt regressions and unexpected per-session differences difficult to diagnose, especially when role prompts, repo instructions, polyscribe output, harness-native files, and spawn-time injections all participate in launch context.

## What Changes

- Add a session-scoped read-only context inspection surface for any active or restorable AO session.
- Return ordered context segments with provenance, byte counts, contribution status, and redaction/reconstruction metadata.
- Ensure concatenating contributed segment content reproduces the assembled initial context exactly when AO recorded enough launch-time material to do so.
- Expose the same data through the daemon API and an `ao` CLI command with both human-readable and JSON output.
- Preserve the existing project/role prompt inspector while treating it as a hypothetical role-level view, not a substitute for session launch context.

## Capabilities

### New Capabilities

- `session-initial-context`: Inspect the fully assembled initial context that a specific AO session was launched with, decomposed by ordered source segments.

### Modified Capabilities

<!-- None. -->

## Impact

- Backend session launch code must record enough context assembly metadata to inspect sessions later without re-running spawn assembly with changed inputs.
- Backend HTTP controllers and API schema gain a session context endpoint.
- CLI gains a session context command with `--json` support.
- Frontend may reuse or supersede the current role prompt inspector for session-scoped viewing, but the headless API/CLI surface is required.
- Secret-bearing sources must be redacted explicitly instead of silently omitted.
