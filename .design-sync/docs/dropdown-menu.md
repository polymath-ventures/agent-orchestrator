---
category: Overlays
---

# Dropdown Menu

`DropdownMenu` › `DropdownMenuTrigger` + `DropdownMenuContent`
(`DropdownMenuLabel`, `DropdownMenuGroup`, `DropdownMenuItem`,
`DropdownMenuSeparator`, `DropdownMenuShortcut`).

```tsx
<DropdownMenu defaultOpen>
	<DropdownMenuTrigger asChild>
		<Button variant="ghost" size="sm">
			Actions
		</Button>
	</DropdownMenuTrigger>
	<DropdownMenuContent>
		<DropdownMenuLabel>Session</DropdownMenuLabel>
		<DropdownMenuItem>
			Open in terminal<DropdownMenuShortcut>⌘T</DropdownMenuShortcut>
		</DropdownMenuItem>
		<DropdownMenuSeparator />
		<DropdownMenuItem>Archive</DropdownMenuItem>
	</DropdownMenuContent>
</DropdownMenu>
```

`DropdownMenuShortcut` is right-aligned muted text inside an item — it does not
bind the key itself.
