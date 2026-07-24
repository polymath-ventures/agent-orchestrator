---
category: Navigation
---

# Breadcrumb

Path trail for nested views.

`Breadcrumb` › `BreadcrumbList` › `BreadcrumbItem` · `BreadcrumbSeparator` · `BreadcrumbPage`

```tsx
<Breadcrumb>
	<BreadcrumbList>
		<BreadcrumbItem>agent-orchestrator</BreadcrumbItem>
		<BreadcrumbSeparator />
		<BreadcrumbItem>sessions</BreadcrumbItem>
		<BreadcrumbSeparator />
		<BreadcrumbItem>
			<BreadcrumbPage>design-sync</BreadcrumbPage>
		</BreadcrumbItem>
	</BreadcrumbList>
</Breadcrumb>
```

`BreadcrumbPage` marks the current, non-navigable leaf — always last, and it
renders a `<span>`, so wrap it in a `BreadcrumbItem` rather than dropping it
straight into the list.
