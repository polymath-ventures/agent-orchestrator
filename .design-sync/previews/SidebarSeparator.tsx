import { SidebarProvider, SidebarSeparator } from "agent-orchestrator";

// The rail's hairline divider — inset from the edges, on the sidebar surface.
export function BetweenGroups() {
	return (
		<SidebarProvider defaultOpen>
			<div style={{ width: 260, background: "var(--color-bg-sidebar)", padding: "10px 0", borderRadius: 6 }}>
				<div style={{ padding: "4px 12px", fontSize: 13 }}>Projects</div>
				<SidebarSeparator />
				<div style={{ padding: "4px 12px", fontSize: 13 }}>Fleet</div>
			</div>
		</SidebarProvider>
	);
}
