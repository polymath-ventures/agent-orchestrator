import { Plus, Trash2 } from "lucide-react";
import type { components } from "../../api/schema";
import type { AgentModelAvailabilityResponse } from "../hooks/useModelAvailabilityQuery";
import { RequiredAgentField } from "./CreateProjectAgentSheet";
import { ModelAvailabilityField, type ModelSelection } from "./ModelAvailabilityField";
import { Button } from "./ui/button";
import { Label } from "./ui/label";

type AgentInfo = components["schemas"]["AgentInfo"];
type WorkerMixEntry = components["schemas"]["WorkerMixEntry"];

type WorkerMixModelPair = { model: string; effort: string };

// WorkerMixBucket is the string-backed editor row. id is editor-only stable
// identity, and selectionsByAgent remembers native model/effort pairs while the
// operator switches harnesses. Neither field is serialized by buildWorkerMix.
// Weight remains a string while mid-edit and is parsed only for totals/payloads.
export type WorkerMixBucket = {
	id: string;
	agent: string;
	model: string;
	effort: string;
	weight: string;
	selectionsByAgent: Record<string, WorkerMixModelPair>;
};

const REQUIRED_TOTAL = 100;
let nextWorkerMixBucketID = 0;

function newWorkerMixBucketID() {
	nextWorkerMixBucketID += 1;
	return `bucket-${nextWorkerMixBucketID}`;
}

// parseWeight reads the numeric value of a weight field. Number(), not
// parseInt: a number input can legally hold exponent notation like "5e1", and
// parseInt stops at the "e" and silently reads 5 instead of 50 — corrupting a
// mix that looks valid in the field. Non-integer or non-finite input counts as 0.
function parseWeight(weight: string): number {
	const trimmed = weight.trim();
	if (trimmed === "") return 0;
	const n = Number(trimmed);
	return Number.isInteger(n) ? n : 0;
}

// toWorkerMixForm hydrates the flat editor rows from the persisted config array.
export function toWorkerMixForm(mix: WorkerMixEntry[] | undefined): WorkerMixBucket[] {
	return (mix ?? []).map((entry) => {
		const agent = entry.agent ?? "";
		const model = entry.model ?? "";
		const effort = entry.effort ?? "";
		return {
			id: newWorkerMixBucketID(),
			agent,
			model,
			effort,
			weight: entry.weight != null ? String(entry.weight) : "",
			selectionsByAgent: agent ? { [agent]: { model, effort } } : {},
		};
	});
}

// buildWorkerMix produces the payload field, scrubbing an empty editor to
// `undefined` so an unconfigured mix is omitted from the PUT rather than sent as
// an empty array — keeping the stored config free of an empty `workerMix` key and
// the feature unambiguously off.
export function buildWorkerMix(buckets: WorkerMixBucket[]): WorkerMixEntry[] | undefined {
	if (buckets.length === 0) return undefined;
	return buckets.map((bucket) => {
		const entry: WorkerMixEntry = { agent: bucket.agent, weight: parseWeight(bucket.weight) };
		const model = bucket.model.trim();
		if (model) entry.model = model;
		const effort = bucket.effort.trim();
		if (effort) entry.effort = effort;
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

export function workerMixRowError(buckets: WorkerMixBucket[]): string | null {
	for (let i = 0; i < buckets.length; i += 1) {
		const bucket = buckets[i];
		if (!bucket.agent.trim()) {
			return `Worker mix bucket ${i + 1} requires an agent.`;
		}
		const weight = parseWeight(bucket.weight);
		if (weight < 1 || weight > 100) {
			return `Worker mix bucket ${i + 1} weight must be a whole number from 1 to 100.`;
		}
	}
	return null;
}

// parseMaxLiveWorkers turns the cap input into the payload value: 0 or blank
// means unbounded, serialized as `undefined` (omit). Number(), not parseInt, so
// exponent notation such as "1e2" reads as 100 rather than being truncated to 1;
// a non-integer or non-positive value is treated as unbounded.
export function parseMaxLiveWorkers(value: string): number | undefined {
	const trimmed = value.trim();
	if (trimmed === "") return undefined;
	const n = Number(trimmed);
	return Number.isSafeInteger(n) && n > 0 ? n : undefined;
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
	availability,
	disabled = false,
	isRefreshing = false,
	onRefresh,
}: {
	buckets: WorkerMixBucket[];
	onChange: (next: WorkerMixBucket[]) => void;
	agentCatalog?: AgentCatalog;
	availability?: AgentModelAvailabilityResponse;
	disabled?: boolean;
	isRefreshing?: boolean;
	onRefresh?: () => void | Promise<unknown>;
}) {
	const total = workerMixTotal(buckets);
	const invalid = workerMixInvalid(buckets);

	const patchBucket = (index: number, patch: Partial<WorkerMixBucket>) =>
		onChange(buckets.map((bucket, i) => (i === index ? { ...bucket, ...patch } : bucket)));
	const removeBucket = (index: number) => onChange(buckets.filter((_, i) => i !== index));
	const addBucket = () =>
		onChange([
			...buckets,
			{ id: newWorkerMixBucketID(), agent: "", model: "", effort: "", weight: "", selectionsByAgent: {} },
		]);
	const rememberedSelections = (bucket: WorkerMixBucket) => {
		const selections = { ...bucket.selectionsByAgent };
		if (bucket.agent) selections[bucket.agent] = { model: bucket.model, effort: bucket.effort };
		return selections;
	};
	const changeAgent = (index: number, agent: string) => {
		const bucket = buckets[index];
		const selectionsByAgent = rememberedSelections(bucket);
		const remembered = selectionsByAgent[agent] ?? { model: "", effort: "" };
		patchBucket(index, { agent, ...remembered, selectionsByAgent });
	};
	const changeModelSelection = (index: number, selection: ModelSelection) => {
		const bucket = buckets[index];
		const selectionsByAgent = {
			...rememberedSelections(bucket),
			[selection.harness]: { model: selection.model, effort: selection.effort },
		};
		patchBucket(index, { model: selection.model, effort: selection.effort, selectionsByAgent });
	};

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
						<div key={bucket.id} className="flex flex-col gap-3 rounded-md border border-border p-3">
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
								id={`workerMix-${bucket.id}-agent`}
								label="Agent"
								placeholder="Select agent"
								value={bucket.agent}
								authorized={agentCatalog?.authorized}
								installed={agentCatalog?.installed}
								supported={agentCatalog?.supported}
								disabled={disabled}
								onChange={(value) => changeAgent(index, value)}
							/>
							<ModelAvailabilityField
								id={`workerMix-${bucket.id}`}
								label="Model and effort"
								value={{ harness: bucket.agent, model: bucket.model, effort: bucket.effort }}
								onChange={(selection) => changeModelSelection(index, selection)}
								availability={availability}
								configuredPins={Object.entries(rememberedSelections(bucket)).map(([harness, pair]) => ({
									harness,
									...pair,
								}))}
								disabled={disabled || !bucket.agent}
								isRefreshing={isRefreshing}
								onRefresh={onRefresh}
								showHarness={false}
								statusVisibility="actionable"
								emptyLabel="Default / inherit"
								showManualModelNotice
							/>
							<p className="text-[11px] text-passive">
								Blank model uses the agent default; blank effort inherits worker configuration.
							</p>
							<div className="max-w-40">
								<MixField label="Weight" htmlFor={`workerMix-${bucket.id}-weight`}>
									<input
										id={`workerMix-${bucket.id}-weight`}
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
