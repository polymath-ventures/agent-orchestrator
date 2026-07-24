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
2. `cat frontend/dist/assets/index-*.css .design-sync/preview-surface.css > frontend/dist/ds-compiled.css`
   - The Vite CSS asset is **content-hashed**, so its filename changes every
     build; the concat gives `cssEntry` a stable path to point at.
   - `styles.css` cannot be used directly as `cssEntry`: it opens with
     `@import "tailwindcss"`, which the converter cannot resolve. Only the
     _compiled_ stylesheet works.
   - `preview-surface.css` is appended, not imported, because `cssEntry` is
     copied verbatim into `_ds_bundle.css` — a relative `@import` inside it
     would dangle after the copy.
3. `tsc -p .design-sync/tsconfig.dts.json` + `node .design-sync/emit-types-barrel.mjs`
   — see the next section. Skipping this silently destroys every prop contract.

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

**Verify after every build:**
`grep -l 'key: string\]: unknown' ds-bundle/components/*/*/*.d.ts | wc -l`
should print `0`. Anything above 0 means the declaration step regressed.

`tsconfig.dts.json`'s `include` is deliberately narrow (ui + `lib/utils.ts` +
`hooks/use-mobile.ts`). Widening it to `lib/**` or `hooks/**` drags in modules
that need Vite/Electron ambient types (`import.meta.env`, `window.ao`) and the
emit fails.

`SidebarTrigger`, `SidebarInput` and `SidebarSeparator` type their props as
`React.ComponentProps<typeof Button|Input|Separator>`, which the extractor
cannot flatten — they are the only three entries in `cfg.dtsPropsFor`.

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
  capture correctly with `defaultOpen` + a real trigger in the tree; no
  `cardMode`/`viewport` override was needed. An earlier assumption that portals
  would escape the card was wrong.
- `Tooltip`/`TooltipContent` throw outside `TooltipProvider`. Every `Sidebar*`
  part requires `SidebarProvider`.
- `Sidebar` is `h-svh` and overshoots the card by the harness's 24px body
  padding, clipping its own footer — the preview cancels this with `margin: -24`.
- `ResizablePanelGroup` needs an explicit height on a wrapper or it collapses,
  and the installed `react-resizable-panels@4.11.2` uses **`orientation`**, not
  the older `direction` prop.
- Radix `Select` anchors its open menu so the selected item lands on the
  trigger; without top padding the first group scrolls off the card.

## Known render warns (triaged, expected — not new)

- `variants render identically` on single-look components (`Label`, `Switch`)
  — they genuinely have one appearance per state.
- `[RENDER_THIN]` on hairline components (`Separator`, `SidebarSeparator`) —
  they really are ~1px tall.

## Source bug found during the sync (not a sync defect)

`Skeleton` is styled `bg-accent`, and AO remaps `--color-accent` to the brand
blue (`--bridge-accent`), where upstream shadcn's `accent` is a muted hover
surface. Every skeleton therefore renders as solid blue bars. The previews
render this **faithfully** rather than hiding it. Filed separately; if it is
fixed upstream of a re-sync, the `Skeleton` and `SidebarMenuSkeleton` cells
will change appearance and should be re-graded.

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
