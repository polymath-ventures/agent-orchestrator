import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Switch } from "./ui/switch";

type PrimeSettings = components["schemas"]["DomainPrimeSettings"];
type PrimeSettingsView = components["schemas"]["PrimeSettingsView"];

export const primeSettingsQueryKey = ["prime-settings"] as const;

const emptySettings: PrimeSettings = {
	enabled: false,
	displayName: "AO Prime",
	agent: "",
	agentConfig: {},
	rules: "",
	rulesFile: "",
	wakeInterval: "15m",
};

async function fetchPrimeSettings(): Promise<PrimeSettingsView> {
	const { data, error } = await apiClient.GET("/api/v1/prime/settings");
	if (error) throw new Error(apiErrorMessage(error));
	if (!data) throw new Error("Prime settings are unavailable.");
	return data;
}

export function PrimeSection() {
	const queryClient = useQueryClient();
	const query = useQuery({
		queryKey: primeSettingsQueryKey,
		queryFn: fetchPrimeSettings,
		refetchInterval: 15_000,
	});
	const [form, setForm] = useState<PrimeSettings>(emptySettings);

	useEffect(() => {
		if (query.data?.settings) setForm(normalizeSettings(query.data.settings));
	}, [query.data]);

	const mutation = useMutation({
		mutationFn: async () => {
			const { data, error } = await apiClient.PUT("/api/v1/prime/settings", { body: { settings: normalizeSettings(form) } });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Prime settings were not returned.");
			return data;
		},
		onSuccess: (next) => {
			queryClient.setQueryData(primeSettingsQueryKey, next);
			void queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
		},
	});

	const legacy = query.data?.legacyEnvironment;
	const busy = query.isLoading || mutation.isPending;

	return (
		<Card>
			<CardHeader>
				<CardTitle className="text-control">Prime</CardTitle>
			</CardHeader>
			<CardContent className="flex flex-col gap-4">
				<div className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2">
					<div className="min-w-0">
						<label htmlFor="prime-enabled" className="block text-control text-foreground">
							Enable fleet Prime
						</label>
						<p className="mt-0.5 text-xs leading-row text-muted-foreground">
							{form.enabled ? "Prime will be supervised globally." : "Prime is disabled globally."}
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
					<PrimeInput label="Display name" value={form.displayName ?? ""} onChange={(displayName) => setForm((f) => ({ ...f, displayName }))} />
					<PrimeInput label="Agent" value={form.agent ?? ""} onChange={(agent) => setForm((f) => ({ ...f, agent }))} />
					<PrimeInput
						label="Model"
						value={form.agentConfig?.model ?? ""}
						onChange={(model) => setForm((f) => ({ ...f, agentConfig: { ...f.agentConfig, model } }))}
					/>
					<PrimeInput
						label="Effort"
						value={form.agentConfig?.effort ?? ""}
						onChange={(effort) => setForm((f) => ({ ...f, agentConfig: { ...f.agentConfig, effort } }))}
					/>
					<PrimeInput
						label="Wake interval"
						value={form.wakeInterval ?? ""}
						onChange={(wakeInterval) => setForm((f) => ({ ...f, wakeInterval }))}
					/>
					<PrimeInput
						label="Rules file"
						value={form.rulesFile ?? ""}
						onChange={(rulesFile) => setForm((f) => ({ ...f, rulesFile }))}
					/>
				</div>

				<label className="flex flex-col gap-1 text-control text-foreground">
					<span>Rules</span>
					<textarea
						className="min-h-24 rounded-md border border-input bg-transparent px-2.5 py-2 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
						value={form.rules ?? ""}
						onChange={(event) => setForm((f) => ({ ...f, rules: event.target.value }))}
					/>
				</label>

				{legacy?.configured && (
					<p className="text-xs leading-row text-warning">
						Legacy Prime environment is configured{legacy.projectId ? ` for ${legacy.projectId}` : ""}.
					</p>
				)}
				{query.isError && (
					<p className="text-xs leading-row text-error">
						{query.error instanceof Error ? query.error.message : "Could not load Prime settings."}
					</p>
				)}
				{mutation.isError && (
					<p className="text-xs leading-row text-error">
						{mutation.error instanceof Error ? mutation.error.message : "Could not save Prime settings."}
					</p>
				)}

				<div>
					<Button type="button" variant="primary" onClick={() => mutation.mutate()} disabled={busy}>
						{mutation.isPending && <Loader2 className="mr-2 size-icon-base animate-spin" />}
						Save Prime
					</Button>
				</div>
			</CardContent>
		</Card>
	);
}

function PrimeInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
	return (
		<label className="flex flex-col gap-1 text-control text-foreground">
			<span>{label}</span>
			<input
				className="h-control-form rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
				value={value}
				onChange={(event) => onChange(event.target.value)}
			/>
		</label>
	);
}

function normalizeSettings(settings: PrimeSettings): PrimeSettings {
	return {
		...emptySettings,
		...settings,
		agentConfig: { ...(settings.agentConfig ?? {}) },
	};
}
