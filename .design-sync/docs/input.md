---
category: Forms
---

# Input

Single-line text field. Pair with `Label` and wire them with `htmlFor`/`id`.

```tsx
<div className="grid gap-1.5">
	<Label htmlFor="branch">Branch</Label>
	<Input id="branch" placeholder="main" />
</div>
```

Accepts all native `<input>` props (`type`, `disabled`, `value`, `onChange`).
