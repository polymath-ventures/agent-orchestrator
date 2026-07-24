---
category: Navigation
---

# Sidebar

The app's primary navigation rail. **Everything must be inside
`SidebarProvider`** — the child parts read open/collapsed state from its
context and throw without it. `defaultOpen` on the provider sets initial state.

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
