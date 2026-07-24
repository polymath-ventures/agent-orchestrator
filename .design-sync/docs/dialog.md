---
category: Overlays
---

# Dialog

Modal. `Dialog` owns open state; `DialogTrigger` opens it.

`Dialog` › `DialogTrigger` + `DialogContent` (`DialogHeader` › `DialogTitle`,
`DialogDescription`; `DialogFooter`; `DialogClose`).

```tsx
<Dialog defaultOpen>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Delete session?</DialogTitle>
			<DialogDescription>This cannot be undone.</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<DialogClose asChild>
				<Button variant="outline">Cancel</Button>
			</DialogClose>
			<Button>Delete</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
```

Always give a `DialogTitle` — it is the accessible name. `DialogPortal` and
`DialogOverlay` are supplied by `DialogContent`.
