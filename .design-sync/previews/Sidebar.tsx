import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarInset,
	SidebarMenu,
	SidebarMenuBadge,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarProvider,
} from "agent-orchestrator";

// Every Sidebar part reads open/collapsed state from SidebarProvider, so the
// only true render of any of them is the full composition.
//
// Sidebar is h-svh, so it overshoots the preview viewport by the harness's
// 24px body padding and clips its own footer. The negative margin cancels that
// padding so the full rail — header through footer — is visible.
export function Navigation() {
	return (
		<div style={{ margin: -24, height: "100svh", display: "flex" }}>
			<SidebarProvider defaultOpen>
				<Sidebar>
					<SidebarHeader>
						<div style={{ padding: "4px 8px", fontWeight: 600 }}>Agent Orchestrator</div>
					</SidebarHeader>
					<SidebarContent>
						<SidebarGroup>
							<SidebarGroupLabel>Projects</SidebarGroupLabel>
							<SidebarGroupContent>
								<SidebarMenu>
									<SidebarMenuItem>
										<SidebarMenuButton isActive>agent-orchestrator</SidebarMenuButton>
										<SidebarMenuBadge>7</SidebarMenuBadge>
									</SidebarMenuItem>
									<SidebarMenuItem>
										<SidebarMenuButton>polypowers</SidebarMenuButton>
										<SidebarMenuBadge>2</SidebarMenuBadge>
									</SidebarMenuItem>
									<SidebarMenuItem>
										<SidebarMenuButton>mirrorborn</SidebarMenuButton>
									</SidebarMenuItem>
								</SidebarMenu>
							</SidebarGroupContent>
						</SidebarGroup>
						<SidebarGroup>
							<SidebarGroupLabel>Fleet</SidebarGroupLabel>
							<SidebarGroupContent>
								<SidebarMenu>
									<SidebarMenuItem>
										<SidebarMenuButton>Sessions</SidebarMenuButton>
									</SidebarMenuItem>
									<SidebarMenuItem>
										<SidebarMenuButton>Quota</SidebarMenuButton>
									</SidebarMenuItem>
								</SidebarMenu>
							</SidebarGroupContent>
						</SidebarGroup>
					</SidebarContent>
					<SidebarFooter>
						<div style={{ padding: "4px 8px", fontSize: 12, opacity: 0.7 }}>daemon · connected</div>
					</SidebarFooter>
				</Sidebar>
				<SidebarInset>
					<div style={{ padding: "20px 20px 20px 44px" }}>
						<div style={{ fontWeight: 600, marginBottom: 6 }}>Sessions</div>
						<div style={{ fontSize: 13, opacity: 0.7 }}>7 active · 2 awaiting review</div>
					</div>
				</SidebarInset>
			</SidebarProvider>
		</div>
	);
}
