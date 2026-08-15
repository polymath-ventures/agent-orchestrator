import { Info, RefreshCw, TriangleAlert } from "lucide-react";
import { useMemo } from "react";
import type {
	AgentHarnessModels,
	AgentModelAvailability,
	AgentModelAvailabilityResponse,
} from "../hooks/useModelAvailabilityQuery";
import { Button } from "./ui/button";
import { Label } from "./ui/label";

export type ModelSelection = {
	harness: string;
	model: string;
	effort: string;
};

export type ConfiguredModelPin = ModelSelection;

export type ModelCatalogOption = AgentModelAvailability & {
	catalogEffortCount?: number;
	synthetic?: boolean;
};

export type HarnessCatalogOption = Omit<AgentHarnessModels, "models"> & {
	models: ModelCatalogOption[];
};

export type ModelAvailabilityFieldProps = {
	id: string;
	label: string;
	value: ModelSelection;
	onChange: (selection: ModelSelection) => void;
	availability?: AgentModelAvailabilityResponse;
	configuredPins?: ConfiguredModelPin[];
	disabled?: boolean;
	isRefreshing?: boolean;
	onRefresh?: () => void | Promise<unknown>;
	showHarness?: boolean;
	showEffort?: boolean;
	allowEmpty?: boolean;
	emptyLabel?: string;
	fieldLabelsVisible?: boolean;
	harnessEmptyLabel?: string;
	modelEmptyLabel?: string;
	effortEmptyLabel?: string;
	showManualModelNotice?: boolean;
};

const selectClassName =
	"h-control-form w-full rounded-md border border-border bg-transparent px-2.5 text-control text-foreground outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-focus disabled:cursor-not-allowed disabled:opacity-50";

export function ModelAvailabilityField({
	id,
	label,
	value,
	onChange,
	availability,
	configuredPins = [],
	disabled = false,
	isRefreshing = false,
	onRefresh,
	showHarness = true,
	showEffort = true,
	allowEmpty = true,
	emptyLabel = "Agent default",
	fieldLabelsVisible = true,
	harnessEmptyLabel = emptyLabel,
	modelEmptyLabel = emptyLabel,
	effortEmptyLabel = emptyLabel,
	showManualModelNotice = false,
}: ModelAvailabilityFieldProps) {
	const harnesses = useMemo(
		() => buildModelCatalogView(availability, value, configuredPins),
		[availability, configuredPins, value],
	);
	const harness = harnesses.find((option) => option.id === value.harness);
	const model = harness?.models.find((option) => option.model === value.model);
	const manualEffort = shouldUseManualEffort(model);
	const efforts = buildEffortOptions(
		model?.synthetic
			? [...(model.efforts ?? []), ...harnessEfforts(harness)]
			: model
				? (model.efforts ?? [])
				: harnessEfforts(harness),
	);
	const provenance = harness ? catalogProvenanceLabel(harness) : "";
	// Only an actionable status is rendered. A non-actionable one — "not probed;
	// only configured pins are live-validated" — is noise a user cannot act on.
	// This used to be a caller-supplied `statusVisibility` prop defaulting to
	// "all", so the component shipped the wrong behavior and every one of its
	// four callers had to remember to override it. The rule belongs here, in the
	// component that owns the status, not in a copy each caller re-derives.
	const showModelStatus = model && model.status === "unreachable";
	const columnClass = showHarness && showEffort ? "sm:grid-cols-3" : showHarness || showEffort ? "sm:grid-cols-2" : "";

	const selectHarness = (nextHarnessID: string) => {
		if (nextHarnessID === "") {
			onChange({ harness: "", model: "", effort: "" });
			return;
		}
		const configuredPin = configuredPins.find((pin) => pin.harness.trim() === nextHarnessID);
		onChange({
			harness: nextHarnessID,
			model: configuredPin?.model.trim() ?? "",
			effort: configuredPin?.effort.trim() ?? "",
		});
	};

	const selectModel = (nextModelID: string) => {
		const nextModel = harness?.models.find((option) => option.model === nextModelID);
		const nextUsesManualEffort = shouldUseManualEffort(nextModel);
		const keepEffort = nextModel?.efforts?.includes(value.effort);
		const keepManualEffort = nextUsesManualEffort && !nextModel?.synthetic && value.effort.trim() !== "";
		onChange({
			...value,
			model: nextModelID,
			effort: nextModelID === "" ? "" : keepEffort || keepManualEffort ? value.effort : preferredEffort(nextModel),
		});
	};
	const refresh = async () => {
		try {
			await onRefresh?.();
		} catch {
			// The query keeps the last successful catalog. Callers render query
			// state separately, so a refresh failure must not escape the click
			// handler as an unhandled rejection.
		}
	};

	return (
		<fieldset className="flex min-w-0 flex-col gap-2" disabled={disabled}>
			<div className="flex items-center justify-between gap-2">
				<legend className="text-[12px] font-medium text-muted-foreground">{label}</legend>
				{onRefresh && (
					<Button
						type="button"
						variant="ghost"
						className="h-7 px-2"
						disabled={disabled || isRefreshing}
						onClick={() => void refresh()}
					>
						<RefreshCw className={`size-3.5 ${isRefreshing ? "animate-spin" : ""}`} aria-hidden="true" />
						<span className="sr-only">Refresh model catalog</span>
					</Button>
				)}
			</div>

			<div className={`grid gap-2 ${columnClass}`}>
				{showHarness && (
					<div className="flex min-w-0 flex-col gap-1">
						<Label
							htmlFor={`${id}-harness`}
							className={fieldLabelsVisible ? "text-[11px] text-muted-foreground" : "sr-only"}
						>
							Harness
						</Label>
						<select
							id={`${id}-harness`}
							className={selectClassName}
							value={value.harness}
							onChange={(event) => selectHarness(event.target.value)}
						>
							{allowEmpty && <option value="">{harnessEmptyLabel}</option>}
							{harnesses.map((option) => (
								<option key={option.id} value={option.id}>
									{option.label}
								</option>
							))}
						</select>
					</div>
				)}

				<div className="flex min-w-0 flex-col gap-1">
					<Label
						htmlFor={`${id}-model`}
						className={fieldLabelsVisible ? "text-[11px] text-muted-foreground" : "sr-only"}
					>
						Model
					</Label>
					<input
						id={`${id}-model`}
						type="text"
						className={selectClassName}
						value={value.model}
						list={`${id}-model-options`}
						placeholder={allowEmpty ? modelEmptyLabel : undefined}
						required={!allowEmpty}
						onChange={(event) => selectModel(event.target.value)}
					/>
					<datalist id={`${id}-model-options`}>
						{harness?.models.map((option) => (
							<option key={option.model} value={option.model}>
								{option.label}
							</option>
						))}
					</datalist>
				</div>

				{showEffort && (
					<div className="flex min-w-0 flex-col gap-1">
						<Label
							htmlFor={`${id}-effort`}
							className={fieldLabelsVisible ? "text-[11px] text-muted-foreground" : "sr-only"}
						>
							Effort
						</Label>
						{manualEffort ? (
							<>
								<input
									id={`${id}-effort`}
									type="text"
									className={selectClassName}
									value={value.effort}
									list={`${id}-effort-options`}
									placeholder={allowEmpty ? effortEmptyLabel : undefined}
									required={!allowEmpty}
									onChange={(event) => onChange({ ...value, effort: event.target.value })}
								/>
								<datalist id={`${id}-effort-options`}>
									{efforts.map((effort) => (
										<option key={effort} value={effort} />
									))}
								</datalist>
							</>
						) : (
							<select
								id={`${id}-effort`}
								className={selectClassName}
								value={value.effort}
								onChange={(event) => onChange({ ...value, effort: event.target.value })}
							>
								{allowEmpty && <option value="">{effortEmptyLabel}</option>}
								{efforts.map((effort) => (
									<option key={effort} value={effort}>
										{effort}
									</option>
								))}
							</select>
						)}
					</div>
				)}
			</div>

			{showManualModelNotice && (
				<p className="flex items-start gap-1.5 text-[12px] text-muted-foreground">
					<TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-warning" aria-hidden="true" />
					<span>
						Manual model IDs are allowed; launch may fail if the harness rejects the model.
						{showEffort && manualEffort
							? " Manual effort values are allowed; AO may reject the effort when saving."
							: ""}
					</span>
				</p>
			)}

			{showModelStatus && (
				<p
					className={`flex items-start gap-1.5 text-[12px] ${model.status === "unreachable" ? "text-warning" : "text-muted-foreground"}`}
				>
					{model.status === "unreachable" ? (
						<TriangleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
					) : (
						<Info className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
					)}
					<span>
						Status: {model.status}
						{model.reason ? ` · ${model.reason}` : ""}
					</span>
				</p>
			)}

			{provenance && (
				<p className="flex items-start gap-1.5 text-[12px] text-muted-foreground">
					<Info className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
					<span>{provenance}</span>
				</p>
			)}

			{availability?.checkedAt && (
				<time dateTime={availability.checkedAt} className="text-[11px] text-passive">
					Checked {formatCheckedAt(availability.checkedAt)}
				</time>
			)}
		</fieldset>
	);
}

export function buildModelCatalogView(
	availability: AgentModelAvailabilityResponse | undefined,
	current: ModelSelection,
	configuredPins: ConfiguredModelPin[] = [],
): HarnessCatalogOption[] {
	const harnesses = new Map<string, HarnessCatalogOption>();
	for (const harness of availability?.harnesses ?? []) {
		harnesses.set(harness.id, {
			...harness,
			models: harness.models.map((model) => ({ ...model, catalogEffortCount: model.efforts?.length ?? 0 })),
		});
	}

	const pins = [...configuredPins];
	if (current.harness && current.model) pins.push(current);
	for (const pin of pins) {
		const harnessID = pin.harness.trim();
		const modelID = pin.model.trim();
		if (!harnessID || !modelID) continue;
		let harness = harnesses.get(harnessID);
		if (!harness) {
			harness = {
				id: harnessID,
				label: harnessID,
				reviewerCapable: false,
				catalogSource: "configured-pins",
				catalogVerified: false,
				catalogReason: "Harness is present only in configured model pins.",
				models: [],
			};
			harnesses.set(harnessID, harness);
		}
		let model = harness.models.find((option) => option.model === modelID);
		if (!model) {
			model = {
				model: modelID,
				label: modelID,
				efforts: pin.effort ? [pin.effort] : [],
				verified: false,
				status: "unknown",
				reason: "Configured pin is not present in the current catalog.",
				reasonCode: "not-probed",
				catalogEffortCount: 0,
				synthetic: true,
			};
			harness.models.push(model);
		} else if (pin.effort && !model.efforts?.includes(pin.effort)) {
			model.efforts = [...(model.efforts ?? []), pin.effort];
		}
	}

	for (const harness of harnesses.values()) {
		harness.models.sort((a, b) => a.label.localeCompare(b.label) || a.model.localeCompare(b.model));
	}
	return [...harnesses.values()].sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));
}

export function catalogProvenanceLabel(
	harness: Pick<AgentHarnessModels, "label" | "catalogSource" | "catalogReason" | "catalogVerified">,
): string {
	const reason = harness.catalogReason ? `: ${harness.catalogReason}` : "";
	switch (harness.catalogSource) {
		case "adapter":
			return harness.catalogVerified ? "" : `Using an unverified ${harness.label} adapter catalog${reason}`;
		case "cached-adapter":
			return `Using the last successful cached ${harness.label} catalog${reason}`;
		case "known-set":
			return `Using a known fallback catalog for ${harness.label}${reason}`;
		case "configured-pins":
			return `Showing configured pins only for ${harness.label}${reason}`;
		case "none":
			return `No model catalog is available for ${harness.label}${reason}`;
	}
}

export function preferredEffort(model?: Pick<AgentModelAvailability, "defaultEffort" | "efforts">): string {
	return model?.defaultEffort ?? model?.efforts?.[0] ?? "";
}

export function shouldUseManualEffort(
	model: Pick<ModelCatalogOption, "catalogEffortCount" | "efforts" | "synthetic"> | undefined,
): boolean {
	if (!model) return true;
	if (model.synthetic) return true;
	return (model.catalogEffortCount ?? model.efforts?.length ?? 0) === 0;
}

export function harnessEfforts(harness: HarnessCatalogOption | undefined): string[] {
	const efforts = new Set<string>();
	for (const model of harness?.models ?? []) {
		for (const effort of model.efforts ?? []) {
			efforts.add(effort);
		}
	}
	return [...efforts];
}

export function buildEffortOptions(efforts: string[] | undefined): string[] {
	const options = new Set<string>();
	for (const effort of efforts ?? []) {
		const trimmed = effort.trim();
		if (!trimmed) continue;
		options.add(trimmed);
	}
	return [...options];
}

function formatCheckedAt(value: string): string {
	const checked = new Date(value);
	return Number.isNaN(checked.getTime()) ? value : checked.toLocaleString();
}
