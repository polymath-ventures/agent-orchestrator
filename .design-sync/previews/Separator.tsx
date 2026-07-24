import { Separator } from "agent-orchestrator";

export function Horizontal() {
	return (
		<div style={{ maxWidth: 380 }}>
			<div style={{ paddingBottom: 10, fontWeight: 600 }}>Fleet</div>
			<Separator />
			<div style={{ paddingTop: 10, fontSize: 13, opacity: 0.7 }}>7 active sessions · 2 awaiting review</div>
		</div>
	);
}

export function Vertical() {
	return (
		<div style={{ display: "flex", height: 20, alignItems: "center", gap: 12, fontSize: 13 }}>
			<span>Sessions</span>
			<Separator orientation="vertical" />
			<span>Quota</span>
			<Separator orientation="vertical" />
			<span>Settings</span>
		</div>
	);
}
