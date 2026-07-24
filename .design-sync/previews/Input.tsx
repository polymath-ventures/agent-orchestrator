import { Input, Label } from "agent-orchestrator";

export function WithLabel() {
	return (
		<div style={{ display: "grid", gap: 6, maxWidth: 320 }}>
			<Label htmlFor="branch">Branch</Label>
			<Input id="branch" defaultValue="claude/design-sync" />
		</div>
	);
}

export function States() {
	return (
		<div style={{ display: "grid", gap: 12, maxWidth: 320 }}>
			<Input placeholder="Search sessions…" />
			<Input defaultValue="agent-orchestrator" />
			<Input defaultValue="locked-by-daemon" disabled />
		</div>
	);
}
