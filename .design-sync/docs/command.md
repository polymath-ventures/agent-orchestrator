---
category: Overlays
---

# Command

Filterable command palette (cmdk).

`Command` (or `CommandDialog` for the modal form) › `CommandInput` ·
`CommandList` (`CommandEmpty`, `CommandGroup`, `CommandItem`).

```tsx
<Command>
	<CommandInput placeholder="Type a command…" />
	<CommandList>
		<CommandEmpty>No results.</CommandEmpty>
		<CommandGroup heading="Sessions">
			<CommandItem>New session</CommandItem>
			<CommandItem>Attach to running</CommandItem>
		</CommandGroup>
	</CommandList>
</Command>
```

`CommandEmpty` renders only when filtering yields nothing. The parts must stay
inside `Command`/`CommandDialog` — they read its context.
