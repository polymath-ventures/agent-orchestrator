---
category: Data Display
---

# Badge

Mono-type pill for short status or metadata. In Agent Orchestrator colour is
rare and meaningful — reach for `neutral` first and use a colour variant only
when the state actually matters.

`variant`: `neutral` (default) · `outline` · `accent` · `success` · `warning` · `error`

```tsx
<Badge>queued</Badge>
<Badge variant="success">mergeable</Badge>
<Badge variant="warning">needs you</Badge>
<Badge variant="error">failed</Badge>
```

Badges render `font-mono` at micro size — they are chrome, not headings. Keep
the label to one or two lowercase words.
