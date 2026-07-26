## Context

The sidebar renders session rows in `frontend/src/renderer/components/Sidebar.tsx`.
Each row already carries a 6px status dot via the local `SessionDot` component,
which delegates its colour to `getSessionDotView` in
`frontend/src/renderer/lib/session-presentation.ts`. `SessionDot` is used at
three row variants (the Prime row, the rename-editing sub-row, and the ordinary
session sub-row).

The harness is already on the client read model:
`WorkspaceSession.provider` is an `AgentProvider` union in
`frontend/src/renderer/types/workspace.ts`, whose members are exactly the
daemon's agent adapter ids — the 24 harnesses AO can spawn plus the `fake` test
adapter. The full session object is already passed into every row, so the
harness reaches the sidebar with no plumbing.

The operator reviewed candidate marks at true rendered size (dark theme, light
theme, and a 152px-narrow sidebar) and approved a specific set before
implementation started. This document records those decisions so a later reader
does not re-open them.

## Goals / Non-Goals

**Goals:**

- Tell `claude-code` from `codex` from `codex-fugu` at a glance in the sidebar,
  without reading the name or opening the session.
- Cover the whole harness roster, with a defined appearance for harnesses that
  have no brand mark and for harness values the mapping has never seen.
- Keep the indicator at status-dot weight: it must not compete with the name,
  and its cost to the name must be bounded to its own slot — one chip width plus
  one row gap, never more.

**Non-Goals:**

- Encoding the harness in the session name. That is the thing `session-naming`
  deliberately decided against, and it stays decided.
- Redesigning the status dot or the sidebar row layout.
- Surfacing the harness anywhere other than the sidebar row (the inspector,
  the board, and the command palette are out of scope).
- Any daemon, API, or schema change.

## Decisions

### Read `session.provider`; add no API surface

`WorkspaceSession.provider` is already the harness adapter id and already
reaches every sidebar row. The indicator is a pure presentation concern derived
from data the client holds.

_Alternative considered:_ adding an explicit `harness` field to the session read
model and regenerating the API client. Rejected — it would duplicate a fact the
read model already carries, and two copies of the same fact eventually
disagree.

### Vendor the mark path data; do not add a dependency

Mark geometry is vendored as inline SVG path data in a frontend presentation
module, matching the existing convention in
`frontend/src/renderer/components/icons.tsx`, which already hand-carries the
`OrchestratorIcon` geometry. The path data derives from
`@lobehub/icons-static-svg` (MIT); attribution belongs in the source file.

_Alternative considered:_ depending on `@lobehub/icons-static-svg` directly.
Rejected — it ships 903 icons to pull in 16, and consuming its `.svg` files
would mean adding an SVG-import build step the frontend does not currently have.

_Alternative considered:_ `simple-icons`. Rejected on coverage — it carries no
OpenAI or Codex mark at all, and also misses Kiro, Kimi, Grok, Goose and
KiloCode.

### One uniform chip treatment for the whole roster

Every harness renders the same shape: a 13px rounded-square chip (26% corner
radius) with a brand background colour and the brand mark knocked out in white.

13px is the floor established by inspecting true-size renders: at 12px the
Claude starburst and the Codex blossom both collapse into indistinct blobs.
Sitting beside a 6px dot, 13px still reads as chrome rather than as content.

_Alternative considered:_ bare monochrome marks with no chip, tinted to the row
text colour. Rejected — it discards the brand colour that does most of the
recognition work at this size, and the several black-brand marks (Cursor,
opencode, Goose, Copilot) become invisible against a dark sidebar.

_Alternative considered:_ rendering each product's authentic app-icon tile
as published. Rejected for inconsistency and for the specific Codex failure
below.

### Codex uses its brand gradient, not its native white app tile

Codex renders as its official gradient tile (`#B1A7FF` → `#7A9DFF` → `#3941FF`,
sampled from the published mark) with the blossom knocked out white.

The native white app tile was tried and rejected on three counts observed in
true-size renders: it glares against the dark sidebar, it disappears against the
light one, and the app icon's own internal padding shrinks the blossom below
legibility under roughly 20px. The gradient tile keeps Codex's blue-violet
identity and its blossom silhouette while matching the weight of every other
chip.

`codex-fugu` renders the Codex mark plus a small cyan (`#22D3EE`) corner pip.
Fugu is a model variant on the same binary, so it earns a modifier rather than
invented branding of its own.

### Unknown harnesses fall back by construction, not by a lookup guard

The mapping is keyed by harness id and returns a neutral monogram chip for any
key it does not carry. Eight harnesses (aider, auggie, continue, crush, droid,
agy, autohand, pi) have no mark in any open icon set and use that neutral
styling today; a harness added to the daemon tomorrow gets the same treatment
without a frontend change, and without an empty slot in the row.

For an id the table has never seen, both the monogram and the accessible name
are derived from the id — initials for a multi-word id, first two letters
otherwise. That derivation is what keeps "every harness renders something" true
by construction, rather than enforced by a test that enumerates the union.

The eight known unbranded harnesses still carry an explicit monogram in the
table rather than relying on that derivation, because deriving them collides:
`auggie` and `autohand` both yield "Au". Their entries pin "Au" and "Ah". The
derivation remains the total fallback; the table only disambiguates the cases we
already know about.

### The row describes the harness; the chip is decorative

The chip is `aria-hidden` and carries only `title` for the hover case. The row
control points `aria-describedby` at a visually hidden label naming the harness.

The first attempt had this backwards — `role="img"` plus `aria-label` on the
chip — and it would have shipped an indicator that assistive technology never
announced. Every sidebar row is a control with its own `aria-label` ("Open
&lt;title&gt;"), and a control with an explicit label drops its descendants from
the accessible-name computation, so a self-labelling chip inside it is simply
not reachable. Testing-library's `getByRole` still found it, which is what made
the mistake look correct.

Describing rather than renaming keeps the row's accessible name unchanged
("Open &lt;title&gt;" still identifies what the row opens) and adds the harness
as a description, which is the semantically right slot for "which tool is
running this" and is announced after the name.

### The marker pair is an element, not a fragment

`SessionMarkers` renders a real `inline-flex` span owning a 7px internal gap,
rather than emitting the dot and the chip as bare siblings.

As a fragment the pair collected the row's own 9px gap twice, costing the name
22px instead of the chip's 13px; and in the Prime row — whose marker slot is a
plain span rather than a flex container — the dot and chip collapsed against
each other with no gap at all. Owning the spacing makes the pair render
identically wherever a row variant places it, which is also why the spec now
says the indicator is _adjacent to_ the status dot rather than _between_ the dot
and the name: the Prime row places its marker slot after the name.

### The variant pip sits inside the chip

`codex-fugu`'s pip is positioned within the chip's bounds, separated from the
mark by the chip's own tile.

The first attempt hung the pip off the chip's corner with a ring in the sidebar
surface colour, which is only correct on a resting row — on hovered and active
rows, whose backgrounds are interactive overlays on that surface, the ring
became a visible halo of the wrong colour. A pip that sits on the chip depends
on nothing outside the chip, so there is no row state for it to guess wrong.

## Risks / Trade-offs

- **[Brand marks drift as products rebrand]** → Geometry lives in one module
  with attribution; a rebrand is a single-file edit, not a dependency bump.
- **[Adding a fixed 13px slot costs the name horizontal space]** → Accepted, and
  stated plainly rather than wished away: an indicator cannot be added for free.
  The cost is bounded to the chip plus one row gap — the marker pair owns its
  internal spacing precisely so the row's gap is not paid twice — and the name
  keeps the row's only flexible slot, so it truncates rather than wrapping or
  overflowing. The spec asserts that invariant, not an "unchanged truncation"
  claim, which was never achievable.
- **[Many harnesses are black-branded, so several chips look alike at 13px]** →
  Accepted. The accessible name and hover title carry the exact identity, and
  the indicator's job is to separate the harnesses the operator actually runs
  (Claude Code, Codex, Codex Fugu), which are strongly distinct from each other.
- **[The `fake` test adapter reaches the mapping]** → It falls through to the
  neutral fallback like any other unmapped id; no special case.

## Open Questions

None. The mark set, the size, the Codex treatment, the Fugu pip, and the
fallback were reviewed at true size and approved before implementation.
