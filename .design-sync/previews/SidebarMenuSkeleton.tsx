import { SidebarMenu, SidebarMenuItem, SidebarMenuSkeleton, SidebarProvider } from "agent-orchestrator";

// Loading placeholder for rail menu rows, sized to match SidebarMenuButton.
export function Loading() {
	return (
		<SidebarProvider defaultOpen>
			<div style={{ width: 260, background: "var(--color-bg-sidebar)", padding: 8, borderRadius: 6 }}>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuSkeleton showIcon />
					</SidebarMenuItem>
					<SidebarMenuItem>
						<SidebarMenuSkeleton showIcon />
					</SidebarMenuItem>
					<SidebarMenuItem>
						<SidebarMenuSkeleton showIcon />
					</SidebarMenuItem>
				</SidebarMenu>
			</div>
		</SidebarProvider>
	);
}
