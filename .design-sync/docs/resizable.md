---
category: Layout
---

# Resizable

Draggable split panes.

`ResizablePanelGroup` (`orientation="horizontal" | "vertical"`) › `ResizablePanel`
· `ResizableHandle`.

```tsx
<ResizablePanelGroup orientation="horizontal">
	<ResizablePanel defaultSize={30}>Sessions</ResizablePanel>
	<ResizableHandle withHandle />
	<ResizablePanel defaultSize={70}>Detail</ResizablePanel>
</ResizablePanelGroup>
```

The group needs an explicit height to render. `withHandle` shows the grip.

The axis prop is `orientation` (react-resizable-panels v4). Older docs and v1/v2
examples say `direction` — that prop does not exist here.
