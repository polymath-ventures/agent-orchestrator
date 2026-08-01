## Why

The archived session-naming spec made Claude's launch-time `-n` flag
load-bearing and treated an idle prompt like a pending decision. The operator
verified that names still did not reach the Claude Code / Codex apps, so the
contract needs to match the durable in-harness `/rename` path the harnesses
actually persist.

## What Changes

- Treat launch-time name arguments as a spawn-time accelerator only; every
  spawn that supports in-harness rename still receives the daemon-owned name
  through the universal post-readiness rename path.
- Clarify spawn safety: a spawn-owned naming write may proceed while the session
  is active, and may proceed at `waiting_input` only when the harness can report
  pending decisions as `blocked`; ambiguous waiting-input states remain
  suppressed.
- Keep operator rename and restore delivery guarded as unsolicited writes: they
  still skip busy, blocked, or idle-prompt sessions unless the write is part of
  the spawn AO just created.

## Capabilities

### New Capabilities

<!-- None. -->

### Modified Capabilities

- `session-naming`: correct harness delivery and spawn-safety requirements for
  app-visible daemon-owned names.

## Impact

- **Backend:** session manager naming delivery policy and focused tests.
- **Adapters:** Claude Code naming comments only; no API shape changes.
- **Specs:** `session-naming` canonical requirements updated through OpenSpec.
