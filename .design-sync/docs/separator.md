---
category: Layout
---

# Separator

Hairline rule. `orientation`: `horizontal` (default) · `vertical`.
A vertical separator needs a parent with a height.

```tsx
<Separator />
<div className="flex h-5 items-center gap-3">
  <span>Fleet</span><Separator orientation="vertical" /><span>Sessions</span>
</div>
```
