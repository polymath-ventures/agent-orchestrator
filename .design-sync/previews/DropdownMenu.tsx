import {
	Button,
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuShortcut,
	DropdownMenuTrigger,
} from "agent-orchestrator";

export function SessionActions() {
	return (
		<DropdownMenu defaultOpen>
			<DropdownMenuTrigger asChild>
				<Button variant="outline">Session actions ›</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent>
				<DropdownMenuLabel>sess_8f2a</DropdownMenuLabel>
				<DropdownMenuGroup>
					<DropdownMenuItem>
						Attach
						<DropdownMenuShortcut>⌘⏎</DropdownMenuShortcut>
					</DropdownMenuItem>
					<DropdownMenuItem>
						Open worktree
						<DropdownMenuShortcut>⌘O</DropdownMenuShortcut>
					</DropdownMenuItem>
					<DropdownMenuItem>Rename session</DropdownMenuItem>
				</DropdownMenuGroup>
				<DropdownMenuSeparator />
				<DropdownMenuItem>Stop session</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

export function ReviewerRoster() {
	return (
		<DropdownMenu defaultOpen>
			<DropdownMenuTrigger asChild>
				<Button variant="ghost">Assign reviewer ›</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent>
				<DropdownMenuLabel>Available reviewers</DropdownMenuLabel>
				<DropdownMenuItem>Codex</DropdownMenuItem>
				<DropdownMenuItem>Codex Fugu</DropdownMenuItem>
				<DropdownMenuItem disabled>GitHub Copilot (unauthenticated)</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
