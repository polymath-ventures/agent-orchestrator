import {
	Select,
	SelectContent,
	SelectGroup,
	SelectItem,
	SelectLabel,
	SelectSeparator,
	SelectTrigger,
	SelectValue,
} from "agent-orchestrator";

export function Basic() {
	return (
		<Select defaultValue="claude">
			<SelectTrigger style={{ width: 220 }}>
				<SelectValue placeholder="Select an agent" />
			</SelectTrigger>
			<SelectContent>
				<SelectItem value="claude">Claude</SelectItem>
				<SelectItem value="codex">Codex</SelectItem>
				<SelectItem value="gemini">Gemini</SelectItem>
			</SelectContent>
		</Select>
	);
}

// Open, so the grouped menu itself is visible — a closed trigger looks
// identical to every other cell and proves nothing about grouping.
export function GroupedWithLabel() {
	return (
		// Radix anchors the open menu so the selected item lands on the trigger.
		// Without headroom the earlier group scrolls off the top of the card.
		<div style={{ paddingTop: 120 }}>
			<Select defaultValue="review" defaultOpen>
				<SelectTrigger style={{ width: 220 }}>
					<SelectValue placeholder="Select a lifecycle stage" />
				</SelectTrigger>
				<SelectContent>
					<SelectGroup>
						<SelectLabel>Lifecycle</SelectLabel>
						<SelectItem value="planning">Planning</SelectItem>
						<SelectItem value="implementing">Implementing</SelectItem>
						<SelectItem value="review">Review</SelectItem>
					</SelectGroup>
					<SelectSeparator />
					<SelectGroup>
						<SelectLabel>Terminal</SelectLabel>
						<SelectItem value="merged">Merged</SelectItem>
						<SelectItem value="parked">Parked</SelectItem>
					</SelectGroup>
				</SelectContent>
			</Select>
		</div>
	);
}

export function Disabled() {
	return (
		<Select defaultValue="daemon-locked" disabled>
			<SelectTrigger style={{ width: 220 }}>
				<SelectValue />
			</SelectTrigger>
			<SelectContent>
				<SelectItem value="daemon-locked">Locked by daemon</SelectItem>
			</SelectContent>
		</Select>
	);
}
