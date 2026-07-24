import { Tabs, TabsContent, TabsList, TabsTrigger } from "agent-orchestrator";

export function SessionDetail() {
	return (
		<Tabs defaultValue="logs" style={{ width: 360 }}>
			<TabsList>
				<TabsTrigger value="logs">Logs</TabsTrigger>
				<TabsTrigger value="diff">Diff</TabsTrigger>
				<TabsTrigger value="terminal">Terminal</TabsTrigger>
			</TabsList>
			<TabsContent value="logs" style={{ marginTop: 8, fontSize: 13, color: "var(--color-text-muted)" }}>
				Claimed bead ao-214, created worktree, running go test ./... before push.
			</TabsContent>
			<TabsContent value="diff" style={{ marginTop: 8, fontSize: 13, color: "var(--color-text-muted)" }}>
				backend/internal/lifecycle/reaper.go +42 -6
			</TabsContent>
			<TabsContent value="terminal" style={{ marginTop: 8, fontSize: 13, color: "var(--color-text-muted)" }}>
				$ go test -race ./... — 214 passed
			</TabsContent>
		</Tabs>
	);
}

export function ThreeStateReview() {
	return (
		<Tabs defaultValue="pending" style={{ width: 320 }}>
			<TabsList>
				<TabsTrigger value="pending">Pending review</TabsTrigger>
				<TabsTrigger value="clean">Clean</TabsTrigger>
				<TabsTrigger value="parked">Parked</TabsTrigger>
			</TabsList>
			<TabsContent value="pending" style={{ marginTop: 8, fontSize: 13, color: "var(--color-text-muted)" }}>
				PR #118 awaiting an independent reviewer from a different model family.
			</TabsContent>
			<TabsContent value="clean" style={{ marginTop: 8, fontSize: 13, color: "var(--color-text-muted)" }}>
				final-review=success on 9a3f2c1, ready for autonomous merge.
			</TabsContent>
			<TabsContent value="parked" style={{ marginTop: 8, fontSize: 13, color: "var(--color-text-muted)" }}>
				merge-park reason=human-required — touches daemon deploy target.
			</TabsContent>
		</Tabs>
	);
}
