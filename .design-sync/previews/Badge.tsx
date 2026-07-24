import { Badge } from "agent-orchestrator";

export function Variants() {
	return (
		<div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
			<Badge variant="neutral">queued</Badge>
			<Badge variant="outline">draft</Badge>
			<Badge variant="accent">claude</Badge>
			<Badge variant="success">mergeable</Badge>
			<Badge variant="warning">needs review</Badge>
			<Badge variant="error">blocked</Badge>
		</div>
	);
}

export function SessionStatus() {
	return (
		<div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
			<Badge variant="success">running</Badge>
			<Badge variant="warning">awaiting input</Badge>
			<Badge variant="error">crashed</Badge>
			<Badge variant="neutral">stopped</Badge>
		</div>
	);
}
