import { Button, Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "agent-orchestrator";

export function QuotaHint() {
	return (
		<TooltipProvider>
			<Tooltip defaultOpen>
				<TooltipTrigger asChild>
					<Button size="icon" variant="ghost" aria-label="Quota info">
						⚙
					</Button>
				</TooltipTrigger>
				<TooltipContent>Quota window resets 2026-07-25 00:00 UTC.</TooltipContent>
			</Tooltip>
		</TooltipProvider>
	);
}

export function DisabledActionReason() {
	return (
		<TooltipProvider>
			<Tooltip defaultOpen>
				<TooltipTrigger asChild>
					<Button variant="outline" disabled>
						Merge
					</Button>
				</TooltipTrigger>
				<TooltipContent side="bottom">Final review has not reached a clean verdict yet.</TooltipContent>
			</Tooltip>
		</TooltipProvider>
	);
}
