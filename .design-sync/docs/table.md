---
category: Data Display
---

# Table

`Table` › `TableHeader` › `TableRow` › `TableHead`; `TableBody` › `TableRow` ›
`TableCell`. Optional `TableFooter` and `TableCaption`.

```tsx
<Table>
	<TableCaption>Active sessions</TableCaption>
	<TableHeader>
		<TableRow>
			<TableHead>Session</TableHead>
			<TableHead>Status</TableHead>
		</TableRow>
	</TableHeader>
	<TableBody>
		<TableRow>
			<TableCell>design-sync</TableCell>
			<TableCell>
				<Badge variant="success">mergeable</Badge>
			</TableCell>
		</TableRow>
	</TableBody>
</Table>
```

`TableHead` is a header cell, `TableHeader` is the `<thead>` — don't swap them.
