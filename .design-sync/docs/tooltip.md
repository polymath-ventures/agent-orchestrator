---
category: Overlays
---

# Tooltip

**Wrap in `TooltipProvider`** (once, high in the tree — the app root is fine).
`TooltipContent` takes `sideOffset` (default 6).

```tsx
<TooltipProvider>
	<Tooltip defaultOpen>
		<TooltipTrigger asChild>
			<Button variant="ghost" size="icon">
				<Info />
			</Button>
		</TooltipTrigger>
		<TooltipContent>Rebuild the worktree</TooltipContent>
	</Tooltip>
</TooltipProvider>
```

Tooltips carry hints, never essential text — they are invisible on touch.
