---
category: Overlays
---

# Sheet

Edge-anchored drawer. Same shape as `Dialog`, with a `side` on `SheetContent`:
`right` (default) · `left` · `top` · `bottom`.

```tsx
<Sheet defaultOpen>
	<SheetContent side="right">
		<SheetHeader>
			<SheetTitle>New agent</SheetTitle>
			<SheetDescription>Configure and launch.</SheetDescription>
		</SheetHeader>
		…
		<SheetFooter>
			<Button>Launch</Button>
		</SheetFooter>
	</SheetContent>
</Sheet>
```

Use a Sheet for a task with several fields; use a Dialog for a short confirm.
