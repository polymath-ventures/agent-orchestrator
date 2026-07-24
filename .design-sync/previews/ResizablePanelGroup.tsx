import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "agent-orchestrator";

export function SidebarAndTerminal() {
	return (
		<div style={{ height: 220, border: "1px solid var(--color-border)", borderRadius: 8, overflow: "hidden" }}>
			<ResizablePanelGroup orientation="horizontal">
				<ResizablePanel defaultSize={30}>
					<div style={{ padding: 12, fontSize: 13, color: "var(--color-text-muted)" }}>
						Sessions
						<div style={{ marginTop: 8 }}>worker-7</div>
						<div>worker-3</div>
					</div>
				</ResizablePanel>
				<ResizableHandle withHandle />
				<ResizablePanel defaultSize={70}>
					<div style={{ padding: 12, fontSize: 13, color: "var(--color-text-muted)" }}>
						$ go test -race ./...
						<div>ok agent-orchestrator/backend 4.812s</div>
					</div>
				</ResizablePanel>
			</ResizablePanelGroup>
		</div>
	);
}

export function LogsAndDiffStacked() {
	return (
		<div style={{ height: 220, border: "1px solid var(--color-border)", borderRadius: 8, overflow: "hidden" }}>
			<ResizablePanelGroup orientation="vertical">
				<ResizablePanel defaultSize={55}>
					<div style={{ padding: 12, fontSize: 13, color: "var(--color-text-muted)" }}>
						Claimed bead ao-214 — running go build ./...
					</div>
				</ResizablePanel>
				<ResizableHandle withHandle />
				<ResizablePanel defaultSize={45}>
					<div style={{ padding: 12, fontSize: 13, color: "var(--color-text-muted)" }}>
						backend/internal/lifecycle/reaper.go +42 -6
					</div>
				</ResizablePanel>
			</ResizablePanelGroup>
		</div>
	);
}
