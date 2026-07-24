import { Badge, Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow } from "agent-orchestrator";

export function Sessions() {
	return (
		<Table>
			<TableCaption>Active sessions across the fleet.</TableCaption>
			<TableHeader>
				<TableRow>
					<TableHead>Session</TableHead>
					<TableHead>Agent</TableHead>
					<TableHead>Branch</TableHead>
					<TableHead>Status</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				<TableRow>
					<TableCell>sess_9f2a1c</TableCell>
					<TableCell>Claude</TableCell>
					<TableCell>claude/design-sync-2a82ed</TableCell>
					<TableCell>
						<Badge variant="success">running</Badge>
					</TableCell>
				</TableRow>
				<TableRow>
					<TableCell>sess_71bd44</TableCell>
					<TableCell>Codex</TableCell>
					<TableCell>codex/fix-quota-window</TableCell>
					<TableCell>
						<Badge variant="warning">awaiting input</Badge>
					</TableCell>
				</TableRow>
				<TableRow>
					<TableCell>sess_c02de8</TableCell>
					<TableCell>Gemini</TableCell>
					<TableCell>gemini/import-daemon-fix</TableCell>
					<TableCell>
						<Badge variant="error">crashed</Badge>
					</TableCell>
				</TableRow>
				<TableRow>
					<TableCell>sess_1a4f90</TableCell>
					<TableCell>Claude</TableCell>
					<TableCell>claude/cleanup-worktrees</TableCell>
					<TableCell>
						<Badge variant="neutral">stopped</Badge>
					</TableCell>
				</TableRow>
			</TableBody>
		</Table>
	);
}
