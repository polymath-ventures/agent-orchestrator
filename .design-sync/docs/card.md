---
category: Layout
---

# Card

Bordered surface on the raised background. Compose from the parts; `Card` alone
is just an empty panel.

`Card` › `CardHeader` (`CardTitle`, `CardDescription`, `CardAction`) · `CardContent` · `CardFooter`

```tsx
<Card>
	<CardHeader>
		<CardTitle>Session limits</CardTitle>
		<CardDescription>Applies to every agent in this project.</CardDescription>
		<CardAction>
			<Button variant="ghost" size="sm">
				Edit
			</Button>
		</CardAction>
	</CardHeader>
	<CardContent>…</CardContent>
	<CardFooter>
		<Button>Save</Button>
	</CardFooter>
</Card>
```

`CardHeader` switches to a two-column grid automatically when a `CardAction` is
present — put the action inside the header, not floated.
