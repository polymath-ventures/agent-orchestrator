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
		<BreadcrumbPage>design-sync</BreadcrumbPage>
	</BreadcrumbList>
</Breadcrumb>
```

`BreadcrumbPage` marks the current, non-navigable leaf — it is always last.
