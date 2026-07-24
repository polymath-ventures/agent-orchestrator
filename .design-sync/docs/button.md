---
category: Actions
---

# Button

`variant`: `primary` (default, the blue live edge) · `outline` · `secondary` · `ghost`
`size`: `default` · `sm` · `icon` · `icon-sm`
`asChild`: render as the child element (e.g. an anchor) instead of `<button>`.

```tsx
<Button>Create session</Button>
<Button variant="outline">Cancel</Button>
<Button variant="ghost" size="sm">Dismiss</Button>
<Button size="icon" aria-label="Settings"><Settings /></Button>
```

Buttons are `font-normal` with a 6px radius. One primary per view — `primary`
marks the single live action, everything else is `outline` or `ghost`.
