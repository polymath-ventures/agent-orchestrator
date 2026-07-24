---
category: Navigation
---

# Tabs

`Tabs` › `TabsList` › `TabsTrigger`, then `TabsContent` as a **sibling of**
`TabsList` (not inside it), paired to a trigger by `value`.

```tsx
<Tabs defaultValue="diff">
	<TabsList>
		<TabsTrigger value="diff">Diff</TabsTrigger>
		<TabsTrigger value="files">Files</TabsTrigger>
	</TabsList>
	<TabsContent value="diff">…</TabsContent>
	<TabsContent value="files">…</TabsContent>
</Tabs>
```

Every `TabsTrigger` needs a `TabsContent` with the same `value`.
