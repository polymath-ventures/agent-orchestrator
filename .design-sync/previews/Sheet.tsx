import {
	Button,
	Sheet,
	SheetContent,
	SheetDescription,
	SheetFooter,
	SheetHeader,
	SheetTitle,
	SheetTrigger,
} from "agent-orchestrator";

export function SessionDetail() {
	return (
		<Sheet defaultOpen>
			<SheetTrigger asChild>
				<Button variant="outline">View session</Button>
			</SheetTrigger>
			<SheetContent side="right">
				<SheetHeader>
					<SheetTitle>fix-review-race</SheetTitle>
					<SheetDescription>Session sess_8f2a on claude/design-sync-2a82ed, launched from ship-task.</SheetDescription>
				</SheetHeader>
				<SheetFooter style={{ display: "flex", flexDirection: "row", gap: 8 }}>
					<Button size="sm">Attach</Button>
					<Button size="sm" variant="outline">
						Stop session
					</Button>
				</SheetFooter>
			</SheetContent>
		</Sheet>
	);
}

export function ProjectSettingsLeft() {
	return (
		<Sheet defaultOpen>
			<SheetTrigger asChild>
				<Button variant="outline">Project settings</Button>
			</SheetTrigger>
			<SheetContent side="left">
				<SheetHeader>
					<SheetTitle>agent-orchestrator</SheetTitle>
					<SheetDescription>Default runtime, worktree root, and quota policy for new sessions.</SheetDescription>
				</SheetHeader>
				<SheetFooter>
					<Button size="sm" variant="primary">
						Save changes
					</Button>
				</SheetFooter>
			</SheetContent>
		</Sheet>
	);
}
