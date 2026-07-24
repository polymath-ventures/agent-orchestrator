---
category: Layout
---

# Resizable

Draggable split panes.

`ResizablePanelGroup` (`direction="horizontal" | "vertical"`) › `ResizablePanel`
· `ResizableHandle`.

```tsx
<ResizablePanelGroup direction="horizontal">
	<ResizablePanel defaultSize={30}>Sessions</ResizablePanel>
	<ResizableHandle withHandle />
	<ResizablePanel defaultSize={70}>Detail</ResizablePanel>
</ResizablePanelGroup>
```

The group needs an explicit height to render. `withHandle` shows the grip.
