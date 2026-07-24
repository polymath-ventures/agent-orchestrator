import {
	Button,
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "agent-orchestrator";

export function ConfirmDestructive() {
	return (
		<Dialog defaultOpen>
			<DialogTrigger asChild>
				<Button variant="outline">Archive worktree</Button>
			</DialogTrigger>
			<DialogContent style={{ maxWidth: 420 }}>
				<DialogHeader>
					<DialogTitle>Archive worktree?</DialogTitle>
					<DialogDescription>
						This removes .claude/worktrees/design-sync-2a82ed and deletes the branch claude/design-sync-2a82ed. Any
						uncommitted changes are lost.
					</DialogDescription>
				</DialogHeader>
				<DialogFooter style={{ display: "flex", flexDirection: "row", justifyContent: "flex-end", gap: 8 }}>
					<DialogClose asChild>
						<Button variant="outline">Cancel</Button>
					</DialogClose>
					<Button variant="primary">Archive</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

export function NoCloseButton() {
	return (
		<Dialog defaultOpen>
			<DialogTrigger asChild>
				<Button variant="outline">Rebase onto main</Button>
			</DialogTrigger>
			<DialogContent showCloseButton={false} style={{ maxWidth: 420 }}>
				<DialogHeader>
					<DialogTitle>Rebasing session-2a82ed</DialogTitle>
					<DialogDescription>
						Fetching origin/main and replaying 4 commits. This session is locked until the rebase completes.
					</DialogDescription>
				</DialogHeader>
				<DialogFooter>
					<Button variant="ghost" disabled>
						Cancel
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
