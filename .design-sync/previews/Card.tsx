import {
	Badge,
	Button,
	Card,
	CardAction,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardTitle,
} from "agent-orchestrator";

export function Basic() {
	return (
		<Card style={{ maxWidth: 420 }}>
			<CardHeader>
				<CardTitle>Session limits</CardTitle>
				<CardDescription>Applies to every agent launched in this project.</CardDescription>
			</CardHeader>
			<CardContent>
				<p style={{ margin: 0 }}>
					Concurrent sessions are capped per project so a single fleet cannot exhaust the daemon&rsquo;s worker pool.
				</p>
			</CardContent>
		</Card>
	);
}

export function WithAction() {
	return (
		<Card style={{ maxWidth: 420 }}>
			<CardHeader>
				<CardTitle>design-sync-2a82ed</CardTitle>
				<CardDescription>claude/design-sync — 4 commits ahead of main</CardDescription>
				<CardAction>
					<Badge variant="success">mergeable</Badge>
				</CardAction>
			</CardHeader>
			<CardContent>
				<p style={{ margin: 0 }}>Final review passed. CI green on the current head.</p>
			</CardContent>
			<CardFooter style={{ display: "flex", gap: 8 }}>
				<Button size="sm">Merge</Button>
				<Button size="sm" variant="outline">
					View diff
				</Button>
			</CardFooter>
		</Card>
	);
}
