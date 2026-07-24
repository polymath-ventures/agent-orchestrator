import { Label, Switch } from "agent-orchestrator";

export function States() {
	return (
		<div style={{ display: "flex", gap: 16, alignItems: "center", flexWrap: "wrap" }}>
			<div style={{ display: "flex", alignItems: "center", gap: 8 }}>
				<Switch id="switch-off" />
				<Label htmlFor="switch-off">Off</Label>
			</div>
			<div style={{ display: "flex", alignItems: "center", gap: 8 }}>
				<Switch id="switch-on" defaultChecked />
				<Label htmlFor="switch-on">On</Label>
			</div>
			<div style={{ display: "flex", alignItems: "center", gap: 8 }}>
				<Switch id="switch-disabled" disabled />
				<Label htmlFor="switch-disabled">Disabled</Label>
			</div>
		</div>
	);
}

export function SessionSettings() {
	return (
		<div style={{ display: "grid", gap: 12, maxWidth: 320 }}>
			<div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
				<Label htmlFor="auto-merge">Autonomous merge</Label>
				<Switch id="auto-merge" defaultChecked />
			</div>
			<div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
				<Label htmlFor="lan-listener">LAN listener</Label>
				<Switch id="lan-listener" />
			</div>
		</div>
	);
}
