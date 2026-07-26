## 1. Harness glyph mapping

- [x] 1.1 Write failing tests for the harness → glyph resolver: a mapped harness with a brand mark resolves to that mark; an unmapped-but-known harness (e.g. `aider`) resolves to the neutral fallback; an unrecognised harness id resolves to the fallback with an accessible name derived from that id; every member of the `AgentProvider` union resolves to something renderable.
- [x] 1.2 Add the harness glyph module holding the vendored mark path data, brand tile colours, and the resolver. Include the `@lobehub/icons-static-svg` (MIT) attribution in the file header.
- [x] 1.3 Populate the 16 harnesses that carry a published brand mark, with Claude Code on its `#D97757` tile and Codex on the `#B1A7FF → #7A9DFF → #3941FF` gradient tile.
- [x] 1.4 Implement the neutral fallback so it derives its monogram and accessible name from the harness id, making "every harness renders something" true by construction.
- [x] 1.5 Add the `codex-fugu` cyan (`#22D3EE`) corner pip modifier over the Codex mark.

## 2. Sidebar rendering

- [x] 2.1 Write failing tests asserting a sidebar session row renders a harness indicator between the status dot and the name, that it carries an accessible name, and that the rendered session name string is unchanged.
- [x] 2.2 Add the `HarnessGlyph` component rendering the 13px chip with `title` and `aria-label`, leaving the row's own `aria-label` untouched.
- [x] 2.3 Render the glyph beside `SessionDot` at every sidebar row variant that shows a session.
- [x] 2.4 Confirm the fixed-width slot does not change name truncation at the narrow sidebar width.

## 3. Verification

- [x] 3.1 Run the frontend test suite and typecheck.
- [x] 3.2 Drive the running web supervisor in a browser and confirm the indicator renders in both themes and at a narrow sidebar width; capture a screenshot for the PR.
- [x] 3.3 Run `npm run ci-local` and resolve anything it reports before pushing.
