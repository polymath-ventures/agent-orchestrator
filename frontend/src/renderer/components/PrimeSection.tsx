import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import type { components } from "../../api/schema";
import { agentsQueryOptions } from "../hooks/useAgentsQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { useModelAvailabilityQuery, useRefreshModelAvailability } from "../hooks/useModelAvailabilityQuery";
import {
	effectiveDisplayHarness,
	filterModelAvailabilityToSelectableAgents,
	modelAvailabilityFromAgentInventory,
} from "../lib/agent-selection";
import { workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { primeSettingsQueryKey, primeSettingsQueryOptions } from "../hooks/usePrimeSettingsQuery";
import { ModelAvailabilityField, type ModelSelection } from "./ModelAvailabilityField";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
import { Switch } from "./ui/switch";

type PrimeSettings = components["schemas"]["DomainPrimeSettings"];

const emptySettings: PrimeSettings = {
	enabled: false,
	displayName: "AO Prime",
	agent: "",
	agentConfig: {},
	rules: "",
	rulesFile: "",
	wakeInterval: "15m",
};

const PERMISSION_MODE_OPTIONS = [
	{ value: "default", label: "Default" },
	{ value: "accept-edits", label: "Accept edits" },
	{ value: "auto", label: "Auto" },
	{ value: "bypass-permissions", label: "Bypass permissions" },
] as const;

export function PrimeSection() {
	const queryClient = useQueryClient();
	const agentsQuery = useQuery(agentsQueryOptions);
	const modelAvailabilityQuery = useModelAvailabilityQuery();
	const { refresh: refreshModels, isRefreshing: isRefreshingModels } = useRefreshModelAvailability();
	const query = useQuery(primeSettingsQueryOptions);
	const [form, setForm] = useState<PrimeSettings>(emptySettings);
	const [wakeMinutes, setWakeMinutes] = useState("15");
	const [validationError, setValidationError] = useState<string | null>(null);

	useEffect(() => {
		if (query.data?.settings) {
			const settings = normalizeSettings(query.data.settings);
			setForm(settings);
			setWakeMinutes(durationToMinutes(settings.wakeInterval ?? "15m"));
		}
	}, [query.data]);

	const mutation = useMutation({
		mutationFn: async () => {
			const minutes = parseWakeMinutes(wakeMinutes);
			if (minutes === null) {
				throw new PrimeValidationError("Wake interval must be between 1 and 360 minutes.");
			}
			const { data, error } = await apiClient.PUT("/api/v1/prime/settings", {
				body: { settings: normalizeSettings({ ...form, wakeInterval: `${minutes}m` }) },
			});
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Prime settings were not returned.");
			return data;
		},
		onSuccess: (next) => {
			queryClient.setQueryData(primeSettingsQueryKey, next);
			void queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
		},
		onError: (error) => {
			setValidationError(error instanceof PrimeValidationError ? error.message : null);
		},
	});

	const busy = query.isLoading || mutation.isPending;
	// Display-only: if the saved Prime harness has dropped out of the polled model
	// catalog (the daemon omits a harness whose binary is missing), show Claude
	// Code instead of the stale harness. Keyed on the raw polled availability, not
	// the inventory — a missing harness lingers in the inventory but is absent from
	// the catalog. `form.agent` (the saved value) is untouched here; only an
	// explicit user selection updates it.
	const modelSelection: ModelSelection = {
		harness: effectiveDisplayHarness(form.agent ?? "", modelAvailabilityQuery.data),
		model: form.agentConfig?.model ?? "",
		effort: form.agentConfig?.effort ?? "",
	};
	const inventoryModelAvailability = modelAvailabilityFromAgentInventory(agentsQuery.data, {
		current: modelSelection.harness,
		requireAuthorized: true,
	});
	const effectiveModelAvailability = modelAvailabilityQuery.data ?? inventoryModelAvailability;
	const modelAvailability = filterModelAvailabilityToSelectableAgents(effectiveModelAvailability, agentsQuery.data, {
		current: modelSelection.harness,
		requireAuthorized: true,
	});
	const updateModelSelection = (selection: ModelSelection) =>
		setForm((f) => ({
			...f,
			agent: selection.harness,
			agentConfig: { ...f.agentConfig, model: selection.model, effort: selection.effort },
		}));

	return (
		<Card>
			<CardHeader>
				<CardTitle className="text-control">Prime</CardTitle>
			</CardHeader>
			<CardContent className="flex flex-col gap-4">
				<div className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2">
					<div className="min-w-0">
						<label htmlFor="prime-enabled" className="block text-control text-foreground">
							Enable Prime
						</label>
						<p className="mt-0.5 text-xs leading-row text-muted-foreground">
							{form.enabled ? "Prime supervises the fleet globally." : "Prime is disabled globally."}
						</p>
					</div>
					<Switch
						id="prime-enabled"
						checked={form.enabled}
						disabled={busy}
						onCheckedChange={(enabled) => setForm((f) => ({ ...f, enabled }))}
					/>
				</div>

				<div className="grid gap-3 sm:grid-cols-2">
					<PrimeInput
						label="Display name"
						value={form.displayName ?? ""}
						onChange={(displayName) => setForm((f) => ({ ...f, displayName }))}
					/>
					<PrimeInput
						label="Wake interval minutes"
						type="number"
						min={1}
						max={360}
						value={wakeMinutes}
						onChange={setWakeMinutes}
					/>
				</div>

				<ModelAvailabilityField
					id="prime-model"
					label="Prime model and effort"
					value={modelSelection}
					onChange={updateModelSelection}
					availability={modelAvailability}
					configuredPins={[modelSelection]}
					disabled={busy}
					isRefreshing={isRefreshingModels || modelAvailabilityQuery.isFetching}
					onRefresh={refreshModels}
					harnessEmptyLabel="Select harness"
					modelEmptyLabel="Select model"
					effortEmptyLabel="Select effort"
					showManualModelNotice
				/>
				<label className="flex flex-col gap-1 text-control text-foreground">
					<span>Permission mode</span>
					<PermissionModeSelect
						value={form.agentConfig?.permissions ?? ""}
						onChange={(permissions) =>
							setForm((f) => ({ ...f, agentConfig: { ...(f.agentConfig ?? {}), permissions } }))
						}
					/>
				</label>
				{modelAvailabilityQuery.isError && (
					<p className="text-xs leading-row text-warning">
						Model catalogs are unavailable; saved pins remain editable.
					</p>
				)}

				<label className="flex flex-col gap-1 text-control text-foreground">
					<span>Inline instructions</span>
					<textarea
						className="min-h-24 rounded-md border border-input bg-transparent px-2.5 py-2 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
						value={form.rules ?? ""}
						onChange={(event) => setForm((f) => ({ ...f, rules: event.target.value }))}
					/>
				</label>
				<PrimeInput
					label="Instructions file path"
					value={form.rulesFile ?? ""}
					onChange={(rulesFile) => setForm((f) => ({ ...f, rulesFile }))}
				/>
				<p className="text-xs leading-row text-muted-foreground">
					Inline instructions are loaded first. File content is appended after it; the file does not override inline
					instructions. Use an absolute path for fleet Prime.
				</p>

				{query.isError && (
					<p className="text-xs leading-row text-error">
						{query.error instanceof Error ? query.error.message : "Could not load Prime settings."}
					</p>
				)}
				{validationError && <p className="text-xs leading-row text-error">{validationError}</p>}
				{mutation.isError && !(mutation.error instanceof PrimeValidationError) && (
					<p className="text-xs leading-row text-error">
						{mutation.error instanceof Error ? mutation.error.message : "Could not save Prime settings."}
					</p>
				)}

				<div>
					<Button
						type="button"
						variant="primary"
						onClick={() => {
							setValidationError(null);
							mutation.mutate();
						}}
						disabled={busy}
					>
						{mutation.isPending && <Loader2 className="mr-2 size-icon-base animate-spin" />}
						Save Prime
					</Button>
				</div>
			</CardContent>
		</Card>
	);
}

function PrimeInput({
	label,
	value,
	onChange,
	type = "text",
	min,
	max,
}: {
	label: string;
	value: string;
	onChange: (value: string) => void;
	type?: string;
	min?: number;
	max?: number;
}) {
	return (
		<label className="flex flex-col gap-1 text-control text-foreground">
			<span>{label}</span>
			<input
				type={type}
				min={min}
				max={max}
				className="h-control-form rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
				value={value}
				onChange={(event) => onChange(event.target.value)}
			/>
		</label>
	);
}

function PermissionModeSelect({ value, onChange }: { value: string; onChange: (value: string) => void }) {
	return (
		<Select value={value || "__default__"} onValueChange={(v) => onChange(v === "__default__" ? "" : v)}>
			<SelectTrigger className="h-control-form w-full text-control" aria-label="Permission mode">
				<SelectValue />
			</SelectTrigger>
			<SelectContent>
				<SelectItem value="__default__">Prime default</SelectItem>
				{PERMISSION_MODE_OPTIONS.map((opt) => (
					<SelectItem key={opt.value} value={opt.value}>
						{opt.label}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}

function normalizeSettings(settings: PrimeSettings): PrimeSettings {
	return {
		...emptySettings,
		...settings,
		agentConfig: { ...(settings.agentConfig ?? {}) },
	};
}

function durationToMinutes(value: string): string {
	const minutes = parseDurationMinutes(value);
	return minutes === null ? "" : String(minutes);
}

function parseWakeMinutes(value: string): number | null {
	if (!/^\d+$/.test(value.trim())) return null;
	const minutes = Number(value);
	return Number.isInteger(minutes) && minutes >= 1 && minutes <= 360 ? minutes : null;
}

function parseDurationMinutes(value: string): number | null {
	const raw = value.trim();
	if (!raw) return null;
	const tokenPattern = /(\d+(?:\.\d+)?)(h|m|s)/g;
	let totalSeconds = 0;
	let matched = false;
	let position = 0;
	for (const match of raw.matchAll(tokenPattern)) {
		if (match.index !== position) return null;
		position += match[0].length;
		matched = true;
		const amount = Number(match[1]);
		if (match[2] === "h") totalSeconds += amount * 60 * 60;
		if (match[2] === "m") totalSeconds += amount * 60;
		if (match[2] === "s") totalSeconds += amount;
	}
	const seconds = Math.round(totalSeconds);
	if (!matched || position !== raw.length || Math.abs(totalSeconds - seconds) > Number.EPSILON || seconds % 60 !== 0) {
		return null;
	}
	const minutes = seconds / 60;
	return minutes >= 1 && minutes <= 360 ? minutes : null;
}

class PrimeValidationError extends Error {}
