import { Skeleton } from "agent-orchestrator";

export function SessionRowLoading() {
	return (
		<div style={{ display: "flex", alignItems: "center", gap: 12, width: 320 }}>
			<Skeleton style={{ height: 36, width: 36, borderRadius: 999 }} />
			<div style={{ display: "flex", flexDirection: "column", gap: 6, flex: 1 }}>
				<Skeleton style={{ height: 12, width: "70%" }} />
				<Skeleton style={{ height: 10, width: "45%" }} />
			</div>
		</div>
	);
}

export function SessionListLoading() {
	return (
		<div style={{ display: "flex", flexDirection: "column", gap: 10, width: 320 }}>
			<Skeleton style={{ height: 14, width: "60%" }} />
			<Skeleton style={{ height: 14, width: "85%" }} />
			<Skeleton style={{ height: 14, width: "40%" }} />
			<Skeleton style={{ height: 14, width: "75%" }} />
		</div>
	);
}
