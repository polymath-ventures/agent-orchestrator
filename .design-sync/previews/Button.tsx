import { Button } from "agent-orchestrator";

export function Variants() {
	return (
		<div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
			<Button variant="primary">Create session</Button>
			<Button variant="outline">Open worktree</Button>
			<Button variant="secondary">Reassign</Button>
			<Button variant="ghost">Dismiss</Button>
		</div>
	);
}

export function Sizes() {
	return (
		<div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
			<Button size="default">Default</Button>
			<Button size="sm">Small</Button>
			<Button size="icon" aria-label="Settings">
				⚙
			</Button>
			<Button size="icon-sm" aria-label="Close">
				✕
			</Button>
		</div>
	);
}

export function Disabled() {
	return (
		<div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
			<Button disabled>Merge</Button>
			<Button variant="outline" disabled>
				Rebase
			</Button>
			<Button variant="ghost" disabled>
				Archive
			</Button>
		</div>
	);
}
