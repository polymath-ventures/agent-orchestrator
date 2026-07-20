import { Plus, Trash2 } from "lucide-react";
import type { components } from "../../api/schema";
import { RequiredAgentField } from "./CreateProjectAgentSheet";
import { Button } from "./ui/button";
import { Label } from "./ui/label";

type AgentInfo = components["schemas"]["AgentInfo"];
type WorkerMixEntry = components["schemas"]["WorkerMixEntry"];

// WorkerMixBucket is the flat, string-backed row the settings form edits. Weight
// is kept as a string so the number input stays controlled while a field is
// mid-edit or empty; it's parsed to an integer only for the live total and the
// save payload.
export type WorkerMixBucket = { agent: string; model: string; weight: string };

const REQUIRED_TOTAL = 100;

function parseWeight(weight: string): number {
	const n = Number.parseInt(weight, 10);
	return Number.isFinite(n) ? n : 0;
}

// toWorkerMixForm hydrates the flat editor rows from the persisted config array.
export function toWorkerMixForm(mix: WorkerMixEntry[] | undefined): WorkerMixBucket[] {
	return (mix ?? []).map((entry) => ({
		agent: entry.agent ?? "",
		model: entry.model ?? "",
		weight: entry.weight != null ? String(entry.weight) : "",
	}));
}

// buildWorkerMix produces the payload field, scrubbing an empty editor to
// `undefined` (omit) so an unconfigured mix serializes as absent — the feature
// stays off — rather than an empty array the daemon would persist.
export function buildWorkerMix(buckets: WorkerMixBucket[]): WorkerMixEntry[] | undefined {
	if (buckets.length === 0) return undefined;
	return buckets.map((bucket) => {
		const entry: WorkerMixEntry = { agent: bucket.agent, weight: parseWeight(bucket.weight) };
		const model = bucket.model.trim();
		if (model) entry.model = model;
		return entry;
	});
}

// workerMixTotal sums the (integer-parsed) weights across every editor row.
export function workerMixTotal(buckets: WorkerMixBucket[]): number {
	return buckets.reduce((sum, bucket) => sum + parseWeight(bucket.weight), 0);
}

// workerMixInvalid mirrors the backend's weight-sum rule for feedback only: a
// non-empty mix whose weights do not sum to exactly 100. The backend remains the
// sole authority; this cannot admit an invalid mix, it only blocks the save.
export function workerMixInvalid(buckets: WorkerMixBucket[]): boolean {
	return buckets.length > 0 && workerMixTotal(buckets) !== REQUIRED_TOTAL;
}

// parseMaxLiveWorkers turns the cap input into the payload value: 0 or blank
// means unbounded, serialized as `undefined` (omit).
export function parseMaxLiveWorkers(value: string): number | undefined {
	const n = Number.parseInt(value, 10);
	return Number.isFinite(n) && n > 0 ? n : undefined;
}

type AgentCatalog = {
	authorized?: AgentInfo[];
	installed?: AgentInfo[];
	supported?: AgentInfo[];
};

// WorkerMixFields renders the multi-row weighted-mix editor: one bucket per row
// (agent, optional model, weight, remove), an add button, and a live weight
// total. It is deliberately card-agnostic (no <Card> wrapper) so the settings
// form frames it. The concurrency cap lives alongside it in the same card but is
// owned by the parent, since it is independent of the mix rows.
export function WorkerMixFields({
	buckets,
	onChange,
	agentCatalog,
	disabled = false,
}: {
	buckets: WorkerMixBucket[];
	onChange: (next: WorkerMixBucket[]) => void;
	agentCatalog?: AgentCatalog;
	disabled?: boolean;
}) {
	const total = workerMixTotal(buckets);
	const invalid = workerMixInvalid(buckets);

	const patchBucket = (index: number, patch: Partial<WorkerMixBucket>) =>
		onChange(buckets.map((bucket, i) => (i === index ? { ...bucket, ...patch } : bucket)));
	const removeBucket = (index: number) => onChange(buckets.filter((_, i) => i !== index));
	const addBucket = () => onChange([...buckets, { agent: "", model: "", weight: "" }]);

	return (
		<div className="flex flex-col gap-4">
			<p className="text-xs leading-row text-muted-foreground">
				Distribute unpinned worker spawns across agent buckets by weight. Weights must sum to 100; an empty mix leaves
				the feature off.
			</p>
			{buckets.length === 0 ? (
				<p className="text-xs leading-row text-passive">No worker mix configured.</p>
			) : (
				<div className="flex flex-col gap-3">
					{buckets.map((bucket, index) => (
						<div
							// Buckets are an ordered, index-addressed list with no stable id; the
							// index is the identity the editor mutates against.
							key={index}
							className="flex flex-col gap-3 rounded-md border border-border p-3"
						>
							<div className="flex items-center justify-between">
								<span className="text-xs text-muted-foreground">Bucket {index + 1}</span>
								<Button
									type="button"
									variant="ghost"
									size="icon-sm"
									aria-label={`Remove bucket ${index + 1}`}
									disabled={disabled}
									onClick={() => removeBucket(index)}
								>
									<Trash2 className="size-icon-sm" aria-hidden="true" />
								</Button>
							</div>
							<RequiredAgentField
								id={`workerMixAgent-${index}`}
								label="Agent"
								placeholder="Select agent"
								value={bucket.agent}
								authorized={agentCatalog?.authorized}
								installed={agentCatalog?.installed}
								supported={agentCatalog?.supported}
								disabled={disabled}
								onChange={(value) => patchBucket(index, { agent: value })}
							/>
							<div className="grid grid-cols-2 gap-3">
								<MixField label="Model (optional)" htmlFor={`workerMixModel-${index}`}>
									<input
										id={`workerMixModel-${index}`}
										className="h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
										value={bucket.model}
										disabled={disabled}
										onChange={(e) => patchBucket(index, { model: e.target.value })}
										placeholder="(agent default)"
									/>
								</MixField>
								<MixField label="Weight" htmlFor={`workerMixWeight-${index}`}>
									<input
										id={`workerMixWeight-${index}`}
										type="number"
										min={1}
										max={100}
										className="h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
										value={bucket.weight}
										disabled={disabled}
										onChange={(e) => patchBucket(index, { weight: e.target.value })}
										placeholder="0"
									/>
								</MixField>
							</div>
						</div>
					))}
				</div>
			)}
			<div className="flex items-center justify-between gap-3">
				<Button type="button" variant="outline" size="sm" disabled={disabled} onClick={addBucket}>
					<Plus className="size-icon-sm" aria-hidden="true" />
					Add bucket
				</Button>
				{buckets.length > 0 && (
					<span className={invalid ? "text-xs font-medium text-error" : "text-xs text-muted-foreground"}>
						Total: {total} / {REQUIRED_TOTAL}
					</span>
				)}
			</div>
		</div>
	);
}

function MixField({ label, htmlFor, children }: { label: string; htmlFor?: string; children: React.ReactNode }) {
	return (
		<div className="flex flex-col gap-1.5">
			<Label htmlFor={htmlFor} className="text-xs text-muted-foreground">
				{label}
			</Label>
			{children}
		</div>
	);
}
