# design-sync notes — Agent Orchestrator

Repo-specific gotchas for `/design-sync`. Read this before a re-sync.

## The shape of this repo

- **This is an app, not a design-system package.** `frontend/` is a private
  Electron/web app: no library build, no `dist/` entry, no Storybook, no
  `*.stories.*`. The sync is scoped to the reusable shadcn/ui primitives in
  `frontend/src/renderer/components/ui` (19 source files → 105 exports),
  plus the token layer.
- The synced surface is **only** `components/ui`. The ~60 app composites in
  `frontend/src/renderer/components/` are bound to daemon state, Zustand
  stores and TanStack Query, and are deliberately out of scope.

## Setup steps that are NOT in config (do these on a fresh clone)

1. `npm ci` inside `frontend/`.
2. **Create the package self-link:** `ln -sfn .. frontend/node_modules/agent-orchestrator`
   The converter resolves the package as `<node-modules>/<pkg>`, and npm never
   self-installs. Without this link the build dies with
   `ENOENT … node_modules/agent-orchestrator/package.json`. It is gitignored,
   so it must be recreated per clone.
3. Install the converter deps in `.ds-sync/` (`npm i esbuild ts-morph @types/react`)
   plus `playwright@1.60.0` — **1.60.0 specifically**, because it pins chromium
   build 1223, which is what this machine has cached. A different playwright
   fails with `browserType.launch: Executable doesn't exist`.

## Why cfg.buildCmd does three things

`buildCmd` is not just the app build. In order:

1. `npm --prefix frontend run build:web` — produces the compiled CSS.
2. `cat $(ls frontend/dist/assets/*.css | sort) .design-sync/preview-surface.css > frontend/dist/ds-compiled.css`
   - The Vite CSS asset is **content-hashed**, so its filename changes every
     build; the concat gives `cssEntry` a stable path to point at. It globs
     _every_ emitted `.css` (sorted, so the order is deterministic) rather than
     just `index-*` — a future chunk that emits its own stylesheet would
     otherwise be silently dropped from the design system.
   - `styles.css` cannot be used directly as `cssEntry`: it opens with
     `@import "tailwindcss"`, which the converter cannot resolve. Only the
     _compiled_ stylesheet works.
   - `preview-surface.css` is appended, not imported, because `cssEntry` is
     copied verbatim into `_ds_bundle.css` — a relative `@import` inside it
     would dangle after the copy.
3. `tsc -p .design-sync/tsconfig.dts.json` + `node .design-sync/emit-types-barrel.mjs`
   — see the next section. Skipping this silently destroys every prop contract.
4. `node .design-sync/validate-conventions.mjs` — see the conventions section.

## The .d.ts contracts are load-bearing and fragile

The app is `noEmit`, so there is no declaration tree. Without one the converter
resolves **every** component to `[key: string]: unknown` — the build still
succeeds and validate still exits 0, so this fails _silently_. The design agent
then has no idea `Button` takes `variant` or `Badge` takes `variant`.

Two non-obvious constraints make it work:

- Declarations are emitted to **`frontend/dist/decl`, not `dist/types`**.
  `findTypesRoot()` checks `dist/types` before falling through to the package
  root; if it returns `dist/types`, then `index.d.ts` — which the extractor
  resolves at the _package root_ — sits outside the parsed tree and nothing
  resolves. Emitting to `dist/decl` makes the types root the package dir, so
  both the barrel and the declarations are in scope.
- `emit-types-barrel.mjs` writes `frontend/index.d.ts`, the declaration entry
  the extractor looks for when `package.json` has no `types` field. **Add a
  re-export there for any new `components/ui` file** (the script derives it
  from the directory listing, so just re-run `buildCmd`).

**Verify after every build** — this one-liner asserts the file set is non-empty
first, because a bare `grep | wc -l` prints a reassuring `0` when there are no
declaration files at all, i.e. it certifies loudest exactly when the step has
failed hardest:

```bash
total=$(ls ds-bundle/components/*/*/*.d.ts | wc -l)
weak=$(grep -l 'key: string\]: unknown' ds-bundle/components/*/*/*.d.ts | wc -l)
[ "$total" -gt 0 ] && [ "$weak" -eq 0 ] && echo "ok: $total real contracts" || echo "REGRESSED: total=$total weak=$weak"
```

`tsconfig.dts.json`'s `include` is deliberately narrow (ui + `lib/utils.ts` +
`hooks/use-mobile.tsx`). Widening it to `lib/**` or `hooks/**` drags in modules
that need Vite/Electron ambient types (`import.meta.env`, `window.ao`) and the
emit fails.

`SidebarTrigger`, `SidebarInput` and `SidebarSeparator` type their props as
`React.ComponentProps<typeof Button|Input|Separator>`, which the extractor
cannot flatten — they are the only three entries in `cfg.dtsPropsFor`.

## conventions.md is prompt-injected — validate it, don't eyeball it

`conventions.md` is inlined into the design agent's system prompt, so a name in
it that does not exist is worse than silence: the agent trusts it, writes
vocabulary that never resolves, and ships silently unstyled output. Hand-checking
missed real bugs twice (`text-base`, defined in `@theme` but never emitted
because Tailwind is JIT; and `text-terminal`, which resolves to
`--color-bg-terminal` — a _background_ token, i.e. near-black text on a
near-black surface).

`node .design-sync/validate-conventions.mjs ./ds-bundle` checks every utility
and component named in the file against the built bundle, including escaped
variant selectors (`hover:`) and token-kind mismatches. `cfg.buildCmd` runs it
on every build. **Run it after editing `conventions.md`** — and note the two
gotchas it exists to catch: a class present in `@theme` is not necessarily
emitted, and a `text-*` utility can legitimately exist while pointing at the
wrong kind of token.

## Guidelines are deliberately empty

The converter's default `guidelinesGlob` picked up `frontend/docs/*.md`, whose
only file is a **desktop release runbook** — actively misleading shipped to a
design agent as "design guidelines". `cfg.guidelinesGlob` is therefore pointed
at `docs/design-guidelines/**/*.md`, which does not exist yet; drop real design
guidance there and it ships automatically.

Root `DESIGN.md` was considered and rejected as guidelines content: its useful
substance (colour is rare and meaningful, the type scale, dark-first) is already
distilled into `conventions.md`, while its opening sections call the product
"ReverbCode" and point at a local filesystem path, which would mislead a design
agent more than help it. That staleness is a docs problem worth fixing on its
own, separately from this sync.

## Docs, groups, and the docsMap enumeration

`cfg.docsMap` maps all 105 components to 19 family docs in `.design-sync/docs/`,
and each doc's `category:` frontmatter sets the component's group (7 semantic
groups: Actions, Forms, Navigation, Overlays, Data Display, Layout, Feedback).
This is a full enumeration on purpose — compound parts (`CardHeader`,
`TableRow`, …) never slug-match their family's doc, so discovery cannot bind
them. **A new component needs a `docsMap` entry or it lands in `general` with a
synthesized prompt.**

Paths in `docsMap` are `../../../.design-sync/docs/<family>.md`: they resolve
from `PKG_DIR`, which is the **symlink path** `frontend/node_modules/agent-orchestrator`,
so three `../` are needed to reach the repo root — not one.

## Rendering

- **AO is dark-first.** The preview harness injects `body{background:#fff}` in a
  `<style>` block _after_ the stylesheet, so it wins on document order.
  `preview-surface.css` uses `html body` (specificity 0,0,2) to beat it. The
  same rule ships to designs, which is correct — designs built with this DS
  belong on AO's surface.
- The harness's "preview not yet authored" floor card is emitted with inline
  light-theme colors and is illegible on the dark surface; `preview-surface.css`
  re-tones it via `[data-ds-fallback]` with `!important` (inline styles).
- **No icon library is exported.** `lucide-react` is bundled _inside_ the
  components but is not on `window.AODS`, so previews cannot import icons — use
  text or a unicode glyph.
- Radix portal components (Dialog, Sheet, DropdownMenu, Select, Tooltip) **do**
  capture correctly with `defaultOpen` + a real trigger in the tree — an earlier
  assumption that they would escape the card was wrong. They still need a
  `cardMode` override for the product's grid; see below.
- **The `cfg.overrides` cardModes are load-bearing, not cosmetic.** A preview
  can capture perfectly on its own and still present broken in the product's
  multi-story grid, which `package-validate` reports as `[GRID_OVERFLOW]`.
  Portal components (Dialog, Sheet, DropdownMenu, Select, Tooltip) are
  `cardMode: single` because portal content is positioned outside its grid cell
  and no grid layout can present it; wide ones (Command, Tabs) are
  `cardMode: column` so each story gets full card width instead of being
  cropped. **Do not drop these when adding components** — re-read the
  `[GRID_OVERFLOW]` warns after any preview change.
- `Tooltip`/`TooltipContent` throw outside `TooltipProvider`. Only four sidebar
  parts actually consume the rail context — `Sidebar`, `SidebarMenuButton`,
  `SidebarRail`, `SidebarTrigger` — and those throw outside `SidebarProvider`;
  the rest are plain layout wrappers (verified against `useSidebar()` call
  sites in `sidebar.tsx`).
- `Sidebar` is `h-svh` and overshoots the card by the harness's 24px body
  padding, clipping its own footer — the preview cancels this with `margin: -24`.
- `ResizablePanelGroup` needs an explicit height on a wrapper or it collapses,
  and the installed `react-resizable-panels@4.11.2` uses **`orientation`**, not
  the older `direction` prop.
- Radix `Select` anchors its open menu so the selected item lands on the
  trigger; without top padding the first group scrolls off the card.

## Known validate warns (triaged — anything NOT on this list is new)

The build settles at exactly **two** warnings. Both are understood; a third one
appearing means something changed.

- **`[TOKENS_MISSING]` — 45 undefined custom properties** (`--border`,
  `--accent`, `--bg`, `--bg-card`, `--accent-glow`, …). These come from
  `frontend/src/landing/**`, the marketing page, which has its own token
  vocabulary and is bundled into the same compiled stylesheet. **No component
  under `components/ui` references any of them** (verified by grep). Benign for
  the design system — do not chase it.
- **`[FONT_MISSING]` — the Nerd Font mono stack.** See the fonts section below.

Related known limitation, not a warn: `cssEntry` is the app's **whole** compiled
stylesheet (~175 KB), so designs also receive the landing page's CSS and xterm's
terminal CSS. Scoping a Tailwind build to `components/ui` alone would trim it,
but that is a separate pipeline and was judged not worth the complexity for a
one-time size cost. This is why `[TOKENS_MISSING]` exists at all.

## Fonts

`--font-family-mono` lists JetBrainsMono / FiraCode / Meslo / CaskaydiaCove /
Hack Nerd Fonts. **The repo ships no font files at all** (no `.woff2`/`.ttf`/
`.otf`, no `@font-face` in `src/`) — the app itself relies on those fonts being
installed on the developer's machine and otherwise falls through its own stack
to `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace`.

So there is nothing to wire into `cfg.extraFonts`, and `runtimeFontPrefixes`
does not apply either (no font service serves them). Mono text in the design
system renders in the system monospace fallback — the same thing the app does
on a machine without Nerd Fonts. `[FONT_MISSING]` is therefore expected and
permanent unless the repo starts shipping webfonts.

## Source bug found during the sync (not a sync defect)

`Skeleton` is styled `bg-accent`, and AO remaps `--color-accent` to the brand
blue (`--bridge-accent`), where upstream shadcn's `accent` is a muted hover
surface. Every skeleton therefore renders as solid blue bars. The previews
render this **faithfully** rather than hiding it. Fixed in GH #119 / PR #120
(`bg-accent` → `bg-muted`); once that lands, the `Skeleton` and
`SidebarMenuSkeleton` cells change appearance and must be re-graded on the
next sync.

## Re-sync risks

- **The declaration step is the thing most likely to rot silently.** A
  TypeScript upgrade, a new ambient-type dependency in `components/ui`, or a
  widened `tsconfig.dts.json` include can break the emit; the sync will still
  "succeed" with empty contracts. Run the `grep … | wc -l` check above.
- **`SidebarMenuSkeleton` uses `Math.random()`** for its bar width, so its
  render is nondeterministic. Its render hash can change with no source change;
  a cleared grade there is not necessarily a real change.
- **The chromium/playwright pin is machine state**, not repo state. A different
  machine will likely need a different playwright version — match it to whatever
  `~/.cache/ms-playwright/chromium-*` holds.
- `frontend/index.d.ts`, `frontend/dist/decl/` and `frontend/dist/ds-compiled.css`
  are **generated build outputs inside the app directory**. They are gitignored.
  If someone adds `frontend/index.d.ts` to the app's own `tsconfig` include, the
  app build will start type-checking the DS barrel.
- Preview viewports in `cfg.overrides` were hand-tuned against the current
  content. Materially changing a preview's content may need its viewport
  revisited.
- The 83 components on the floor card are a deliberate baseline, not failures.
  Authoring any of them on a later sync is incremental — previews and grades
  carry forward.
