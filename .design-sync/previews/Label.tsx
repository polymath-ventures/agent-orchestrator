import { Input, Label } from "agent-orchestrator";

export function WithInput() {
	return (
		<div style={{ display: "grid", gap: 6, maxWidth: 320 }}>
			<Label htmlFor="worktree-branch">Branch</Label>
			<Input id="worktree-branch" defaultValue="claude/design-sync-2a82ed" />
		</div>
	);
}

export function RequiredField() {
	return (
		<div style={{ display: "grid", gap: 6, maxWidth: 320 }}>
			<Label htmlFor="project-repo">Project repository</Label>
			<Input id="project-repo" placeholder="owner/agent-orchestrator" />
		</div>
	);
}

export function DisabledField() {
	return (
		<div style={{ display: "grid", gap: 6, maxWidth: 320 }}>
			<Label htmlFor="session-id">Session ID</Label>
			<Input id="session-id" defaultValue="sess_9f2a1c" disabled />
		</div>
	);
}
