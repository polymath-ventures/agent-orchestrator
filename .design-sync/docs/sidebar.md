---
category: Navigation
---

# Sidebar

The app's primary navigation rail. `Sidebar`, `SidebarMenuButton`,
`SidebarRail` and `SidebarTrigger` read the rail's open/collapsed state and
**throw outside `SidebarProvider`**; the remaining parts are plain layout
wrappers. Compose the whole rail inside the provider anyway — `defaultOpen`
on it sets the initial state.

`SidebarProvider` › `Sidebar` › `SidebarHeader` · `SidebarContent`
(`SidebarGroup` › `SidebarGroupLabel`, `SidebarGroupAction`, `SidebarGroupContent`
› `SidebarMenu` › `SidebarMenuItem` › `SidebarMenuButton`) · `SidebarFooter`.
`SidebarInset` holds the page body beside the rail; `SidebarTrigger` toggles it.

```tsx
<SidebarProvider>
	<Sidebar>
		<SidebarHeader>Agent Orchestrator</SidebarHeader>
		<SidebarContent>
			<SidebarGroup>
				<SidebarGroupLabel>Projects</SidebarGroupLabel>
				<SidebarGroupContent>
					<SidebarMenu>
						<SidebarMenuItem>
							<SidebarMenuButton isActive>agent-orchestrator</SidebarMenuButton>
						</SidebarMenuItem>
					</SidebarMenu>
				</SidebarGroupContent>
			</SidebarGroup>
		</SidebarContent>
		<SidebarFooter>…</SidebarFooter>
	</Sidebar>
	<SidebarInset>…page…</SidebarInset>
</SidebarProvider>
```

`SidebarMenuSub` / `SidebarMenuSubItem` / `SidebarMenuSubButton` nest one level
under a menu item. `SidebarMenuSkeleton` is the loading placeholder.
