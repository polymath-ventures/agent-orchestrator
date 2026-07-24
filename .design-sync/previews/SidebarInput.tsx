import { SidebarGroup, SidebarGroupContent, SidebarInput, SidebarProvider } from "agent-orchestrator";

// SidebarInput is the Input primitive restyled for the rail — shown in the
// group it normally sits in.
export function InRail() {
	return (
		<SidebarProvider defaultOpen>
			<div style={{ width: 260, background: "var(--color-bg-sidebar)", padding: 8, borderRadius: 6 }}>
				<SidebarGroup>
					<SidebarGroupContent>
						<SidebarInput placeholder="Filter projects…" />
					</SidebarGroupContent>
				</SidebarGroup>
			</div>
		</SidebarProvider>
	);
}
