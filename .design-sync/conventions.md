# Building with the Agent Orchestrator design system

Agent Orchestrator is a supervisor for coding-agent sessions: dense, dark-first,
keyboard-driven operator UI. Screens are information-dense panels, not marketing
pages. Colour is rare and load-bearing — it marks state, not decoration.

## Setup

No provider is needed for most components. Just import and render:

```jsx
const { Button, Card, CardHeader, CardTitle, CardContent } = window.AODS;
```

Three exceptions throw or render blank without a wrapper:

- `Sidebar`, `SidebarMenuButton`, `SidebarRail` and `SidebarTrigger` read the
  rail's open/collapsed state and must be inside `SidebarProvider`. The other
  `Sidebar*` parts are plain layout wrappers, but you are composing the rail
  anyway — put the whole thing inside the provider.
- `Tooltip` / `TooltipContent` / `TooltipTrigger` must be inside `TooltipProvider`
  (mount one high in the tree).
- `ResizablePanelGroup` needs an explicit height on an ancestor or it collapses
  to nothing.

The theme is dark by default (`:root`). A light theme exists behind
`:root[data-theme="light"]` — do not hand-build a light palette.

## Styling idiom: Tailwind utilities bound to AO tokens

Style with **Tailwind utility classes**. This system remaps Tailwind's theme to
its own tokens, so the utility names below resolve to AO's palette — use them
rather than raw hex, and never use stock Tailwind colour names like
`bg-slate-800` or `text-gray-400`, which are not part of this system.

| Family         | Real names                                                                                                                                                |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Surfaces       | `bg-background` `bg-surface` `bg-raised` `bg-card` `bg-popover` `bg-sidebar` `bg-terminal`                                                                |
| Text           | `text-foreground` `text-muted-foreground` `text-passive` `text-terminal-dim`                                                                              |
| Borders        | `border-border` `border-border-strong` `border-input`                                                                                                     |
| Accent / brand | `bg-primary` `text-primary-foreground` `text-accent` `bg-accent-weak` `border-accent-dim`                                                                 |
| State          | `text-success` `text-warning` `text-error` `text-working` `text-destructive`                                                                              |
| Interaction    | `hover:bg-interactive-hover`                                                                                                                              |
| Type scale     | `text-micro` `text-2xs` `text-caption` `text-xs` `text-sm` `text-control` `text-subtitle` `text-heading-sm` `text-heading` `text-heading-lg` `text-brand` |
| Radius         | `rounded-xs` `rounded-sm` `rounded-md` `rounded-lg` `rounded-panel` `rounded-full`                                                                        |
| Font           | `font-sans` (system UI stack) · `font-mono` (Nerd Font / SF Mono stack)                                                                                   |

Spacing, flex and grid utilities are stock Tailwind (`flex gap-2 px-3 py-2`).

The stylesheet is **pre-compiled, not generated on demand**: it contains the
~1300 utilities this product actually uses. Common layout, spacing, sizing and
the families above are all present, but an exotic or arbitrary utility
(`text-[13.5px]`, a rarely-used step) may simply not exist and will render as
nothing. If a class does not visibly apply, use a neighbouring token from the
tables above or an inline `style` — do not assume the full Tailwind surface.

House rules worth following:

- `text-control` (13px) is the default size for dense chrome — toolbars, rows,
  buttons. `text-sm` is for body copy.
- Use `font-mono` for identifiers the operator reads literally: session ids,
  branch names, paths, shortcuts, counts.
- Borders are hairline white-alpha. Prefer `border-border`; reach for
  `border-border-strong` only to separate major regions.
- One `bg-primary` action per view. Everything else is `outline` or `ghost`.

## Where the truth is

- `_ds/<folder>/styles.css` and its `@import` closure — the compiled stylesheet
  plus `tokens/tokens.css`, which lists every `--color-*`, `--font-size-*`,
  `--space-*` and `--radius-*` with comments on intent. Read it before inventing
  a value.
- `components/<group>/<Name>/<Name>.d.ts` — the real prop contract, including
  variant unions.
- `components/<group>/<Name>/<Name>.prompt.md` — per-family usage and the
  composition order for compound components.

## An idiomatic build

```jsx
const { Card, CardHeader, CardTitle, CardDescription, CardAction, CardContent, Badge, Button } = window.AODS;

<Card className="max-w-md">
	<CardHeader>
		<CardTitle>design-sync-2a82ed</CardTitle>
		<CardDescription className="font-mono text-micro">claude/design-sync — 4 commits ahead</CardDescription>
		<CardAction>
			<Badge variant="success">mergeable</Badge>
		</CardAction>
	</CardHeader>
	<CardContent>
		<div className="flex items-center gap-2 text-control text-muted-foreground">
			<span>Final review clean</span>
			<span className="text-passive">·</span>
			<span>CI green</span>
		</div>
	</CardContent>
</Card>;
```

Library components carry their own styling — pass `variant`/`size` props rather
than re-skinning them with classes. Use utilities for _your_ layout glue around
them.
