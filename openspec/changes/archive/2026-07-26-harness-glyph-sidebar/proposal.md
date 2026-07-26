## Why

The unified session-naming grammar (`session-naming`) deliberately keeps the
harness out of a session's display name: the name is capped at 20 runes and
encoding the harness textually muddies the identity the name is meant to carry.
That leaves an operator with no way to tell a `claude-code` session from a
`codex` or `codex-fugu` session in the sidebar without opening it. The sidebar
already renders a per-session status dot, which is the natural anchor for a
second, equally small indicator.

## What Changes

- Each session row in the AO sidebar renders a small harness glyph immediately
  after the existing 6px status dot and before the session name.
- The glyph is a 13px rounded-square chip carrying the harness's brand mark,
  visually weighted like the status dot rather than competing with the name.
- Every harness AO can spawn resolves to a glyph. Harnesses with no published
  brand mark — and any unrecognised provider value — resolve to a defined
  neutral fallback rather than rendering nothing.
- The glyph carries an accessible name, so the indicator is never colour- or
  shape-only.
- Session name strings are unchanged. The glyph adds information; it does not
  move information out of the name.

## Capabilities

### New Capabilities

- `sidebar-harness-indicator`: how the AO sidebar surfaces which harness is
  running a session — the indicator's placement, its total coverage of the
  harness roster, its fallback for unknown harnesses, its accessible name, and
  the layout invariants it must not disturb.

### Modified Capabilities

None. `session-naming`'s requirements are untouched — this change exists
precisely because the name stays as it is.

## Impact

- `frontend/src/renderer/components/Sidebar.tsx` — session rows gain the glyph
  beside the existing `SessionDot`.
- New frontend presentation module holding the harness → glyph mapping,
  following the existing vendored-SVG convention in
  `frontend/src/renderer/components/icons.tsx`.
- No backend, API, or schema change: `WorkspaceSession.provider` already carries
  the harness adapter id and the full session object already reaches the
  sidebar rows. No new npm dependency — mark path data is vendored inline.
