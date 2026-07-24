import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "agent-orchestrator";

export function SessionPalette() {
	return (
		<Command style={{ width: 380, border: "1px solid var(--color-border)" }}>
			<CommandInput placeholder="Search projects, sessions, pull requests..." />
			<CommandList>
				<CommandGroup heading="Sessions">
					<CommandItem>worker-7 — fix-daemon-restart-race</CommandItem>
					<CommandItem>worker-3 — ship-quota-window</CommandItem>
				</CommandGroup>
				<CommandGroup heading="Projects">
					<CommandItem>agent-orchestrator</CommandItem>
					<CommandItem>web-supervisor</CommandItem>
				</CommandGroup>
				<CommandGroup heading="Commands">
					<CommandItem>/final-review</CommandItem>
					<CommandItem>/cleanup-merge</CommandItem>
				</CommandGroup>
			</CommandList>
		</Command>
	);
}

export function FilteredResults() {
	return (
		<Command style={{ width: 380, border: "1px solid var(--color-border)" }}>
			<CommandInput value="quota" placeholder="Search projects, sessions, pull requests..." />
			<CommandList>
				<CommandGroup heading="Sessions">
					<CommandItem>worker-3 — ship-quota-window</CommandItem>
				</CommandGroup>
				<CommandGroup heading="Pull requests">
					<CommandItem>#113 fix: name the QUOTA window and render a dated reset</CommandItem>
				</CommandGroup>
			</CommandList>
		</Command>
	);
}

export function EmptyResults() {
	return (
		<Command style={{ width: 380, border: "1px solid var(--color-border)" }}>
			<CommandInput value="zzqx" placeholder="Search projects, sessions, pull requests..." />
			<CommandList>
				<CommandEmpty>No results found.</CommandEmpty>
			</CommandList>
		</Command>
	);
}
