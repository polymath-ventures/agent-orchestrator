import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import * as Dialog from "@radix-ui/react-dialog";
import { TriangleAlert, X, type LucideIcon } from "lucide-react";
import { memo, useEffect, useMemo, useState } from "react";
import type { components } from "../../api/schema";
import { agentsQueryKey, agentsQueryOptions, refreshAgents } from "../hooks/useAgentsQuery";
import {
	type AgentModelAvailabilityResponse,
	useModelAvailabilityQuery,
	useRefreshModelAvailability,
} from "../hooks/useModelAvailabilityQuery";
import {
	agentDisplayLabel,
	type AgentInfo,
	type AgentInventory,
	modelAvailabilityFromAgentInventory,
	selectableAgentCatalog,
} from "../lib/agent-selection";
import { cn } from "../lib/utils";
import { AgentAvatar } from "./AgentAvatar";
import { FieldDefaultHint } from "./FieldDefaultHint";
import { buildIntake, type IntakeForm, IntakeFields, intakeNeedsRule } from "./IntakeFields";
import { AgentSelectMenuItem } from "./settings/AgentSelectMenuItem";
import { SettingsRow } from "./settings/SettingsRow";
import { SettingsOptionMenu } from "./settings/SettingsOptionMenu";
import type { ProjectKind } from "../types/workspace";
import { ModelAvailabilityField } from "./ModelAvailabilityField";
import { PermissionModeSelect } from "./PermissionModeSelect";
import { Button } from "./ui/button";
import { Label } from "./ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

type TrackerIntakeConfig = components["schemas"]["TrackerIntakeConfig"];

export type CreateProjectAgentSelection = {
	workerAgent: string;
	orchestratorAgent: string;
	reviewerAgent: string;
	permissions: string;
	modelDefaults: Record<string, { model: string; effort: string }>;
	trackerIntake?: TrackerIntakeConfig;
};

const EMPTY_INTAKE: IntakeForm = { enabled: false, repo: "", assignee: "" };
const AUTOMATIC_REVIEWER = "__automatic_independent_reviewer__";
const DEFAULT_AGENT_PRIORITY = ["claude-code", "codex", "cursor", "opencode", "aider"] as const;
const DEFAULT_AGENT_PRIORITY_RANK = new Map<string, number>(
	DEFAULT_AGENT_PRIORITY.map((agent, index) => [agent, index]),
);

function agentLabelCompare(a: AgentInfo, b: AgentInfo): number {
	return a.label.localeCompare(b.label) || a.id.localeCompare(b.id);
}

type CreateProjectAgentSheetProps = {
	error?: string | null;
	isCreating: boolean;
	isInitializing?: boolean;
	kind: ProjectKind;
	onOpenChange: (open: boolean) => void;
	onSubmit: (selection: CreateProjectAgentSelection) => Promise<void>;
	open: boolean;
	path: string | null;
	repositorySetupNeeded?: boolean;
	repositorySetupWarning?: string | null;
};

type SheetError = {
	title: string;
	message: string;
	tone: "warning" | "error";
};

function projectSheetError(error: string): SheetError {
	const setupMessage = error.replace(/^Setup failed:\s*/i, "").trim();
	const codeMatch = setupMessage.match(/\(([A-Z0-9_]+)\)\s*$/);
	const code = codeMatch?.[1];
	const message = codeMatch ? setupMessage.slice(0, codeMatch.index).trim() : setupMessage;

	switch (code) {
		case "PROJECT_PATH_NOT_REPO_ROOT":
			return {
				title: "Select the repository root",
				message: "This folder is inside another Git repository. Choose the top-level folder and try again.",
				tone: "warning",
			};
		case "PROJECT_BARE_REPOSITORY":
			return {
				title: "Choose a normal checkout",
				message: "AO needs a regular working folder, not a bare Git repository.",
				tone: "warning",
			};
		case "UNSUPPORTED_GIT_REPO":
			return {
				title: "Choose a valid Git folder",
				message: "AO could not read the Git metadata here. Repair the repository or choose a plain folder.",
				tone: "warning",
			};
		default:
			return {
				title: error.toLowerCase().startsWith("setup failed:") ? "Repository setup failed" : "Could not create project",
				message: message || "Try again, or choose a different folder.",
				tone: "error",
			};
	}
}

export function CreateProjectAgentSheet({
	error,
	isCreating,
	isInitializing = false,
	kind,
	onOpenChange,
	onSubmit,
	open,
	path,
	repositorySetupNeeded = false,
	repositorySetupWarning = null,
}: CreateProjectAgentSheetProps) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const agentsQuery = useQuery({
		...agentsQueryOptions,
		enabled: open,
	});
	const modelAvailabilityQuery = useModelAvailabilityQuery(open);
	const { refresh: refreshModels, isRefreshing: isRefreshingModels } = useRefreshModelAvailability();
	const refreshAgentsMutation = useMutation({
		mutationFn: refreshAgents,
		onSuccess: (next) => queryClient.setQueryData(agentsQueryKey, next),
	});
	const agents = agentsQuery.data;
	const installedAgents = agents?.installed ?? [];
	const agentOptions = agents?.authorized ?? [];
	const supportedAgents = agents?.supported ?? [];
	const isLoadingAgents = agents === undefined && agentsQuery.isFetching;
	const agentsError = agentsQuery.isError
		? agentsQuery.error instanceof Error
			? agentsQuery.error.message
			: "Could not load harness catalog."
		: null;
	const displayError = refreshAgentsMutation.isError
		? refreshAgentsMutation.error instanceof Error
			? refreshAgentsMutation.error.message
			: "Could not refresh harness catalog."
		: agentsError;
	const [workerAgent, setWorkerAgent] = useState("");
	const [orchestratorAgent, setOrchestratorAgent] = useState("");
	const [reviewerAgent, setReviewerAgent] = useState("");
	const [permissions, setPermissions] = useState("");
	const [workerAgentTouched, setWorkerAgentTouched] = useState(false);
	const [orchestratorAgentTouched, setOrchestratorAgentTouched] = useState(false);
	const [modelDefaults, setModelDefaults] = useState<Record<string, { model: string; effort: string }>>({});
	const isBusy = isCreating || isInitializing;
	const [intake, setIntake] = useState<IntakeForm>(EMPTY_INTAKE);
	const intakeIncomplete = intakeNeedsRule(intake);
	const canSubmit = workerAgent !== "" && orchestratorAgent !== "" && !intakeIncomplete && !isBusy && !isLoadingAgents;
	const sheetError = error ? projectSheetError(error) : null;
	const inventoryModelAvailability = modelAvailabilityFromAgentInventory(agents);
	const effectiveModelAvailability = modelAvailabilityQuery.data ?? inventoryModelAvailability;
	const usingInventoryModelFallback =
		modelAvailabilityQuery.isError && !modelAvailabilityQuery.data && inventoryModelAvailability !== undefined;
	const reviewerCatalog = useMemo(
		() => reviewerAgentCatalog(agents, effectiveModelAvailability, reviewerAgent),
		[agents, effectiveModelAvailability, reviewerAgent],
	);
	const selectedHarnesses = useMemo(
		() => uniqueSelectedHarnesses([workerAgent, orchestratorAgent, reviewerAgent]),
		[workerAgent, orchestratorAgent, reviewerAgent],
	);
	const configuredModelPins = useMemo(
		() => Object.entries(modelDefaults).map(([harness, pair]) => ({ harness, ...pair })),
		[modelDefaults],
	);

	useEffect(() => {
		if (!open) return;
		const defaultAgent = defaultAuthorizedAgent(agentOptions);
		if (!workerAgentTouched) setWorkerAgent(defaultAgent);
		if (!orchestratorAgentTouched) setOrchestratorAgent(defaultAgent);
	}, [agentOptions, open, orchestratorAgentTouched, workerAgentTouched]);

	useEffect(() => {
		if (!open) {
			setWorkerAgent("");
			setOrchestratorAgent("");
			setReviewerAgent("");
			setPermissions("");
			setWorkerAgentTouched(false);
			setOrchestratorAgentTouched(false);
			setModelDefaults({});
			setIntake(EMPTY_INTAKE);
		}
	}, [open, path]);

	return (
		<Dialog.Root open={open} onOpenChange={(next) => !isBusy && onOpenChange(next)}>
			<Dialog.Portal>
				<Dialog.Overlay className="dialog-overlay data-[state=open]:animate-overlay-in" />
				<Dialog.Content className="fixed left-1/2 top-1/2 z-overlay max-h-[calc(100vh-32px)] w-[min(480px,calc(100vw-32px))] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-agents-sheet border border-[var(--color-border-agents-sheet)] bg-[var(--color-bg-agents-sheet)] p-0 text-[var(--color-text-agents-sheet-title)] shadow-[var(--shadow-import-modal)] data-[state=open]:animate-modal-in">
					<div className="flex items-start justify-between gap-4 border-b border-[var(--color-border-agents-sheet)] px-6 py-5">
						<div className="min-w-0">
							<Dialog.Title className="text-subtitle font-semibold text-[var(--color-text-agents-sheet-title)]">
								{kind === "workspace" ? t("createProject.workspaceAgents") : t("createProject.projectAgents")}
							</Dialog.Title>
							<Dialog.Description className="mt-1 break-all text-xs text-[var(--color-text-agents-sheet-description)]">
								{path ?? ""}
							</Dialog.Description>
						</div>
						<Dialog.Close asChild>
							<button
								type="button"
								className="grid size-7 shrink-0 place-items-center rounded-md text-[var(--color-text-agents-sheet-description)] transition hover:bg-interactive-hover hover:text-[var(--color-text-agents-sheet-title)] disabled:pointer-events-none disabled:opacity-50"
								aria-label="Close project harnesses dialog"
								disabled={isBusy}
							>
								<X className="size-icon-base" aria-hidden="true" />
							</button>
						</Dialog.Close>
					</div>
					<form
						className="space-y-5 px-6 py-5"
						onSubmit={(event) => {
							event.preventDefault();
							if (!canSubmit) return;
							void onSubmit({
								workerAgent,
								orchestratorAgent,
								reviewerAgent,
								permissions,
								modelDefaults: selectedModelDefaults(selectedHarnesses, modelDefaults),
								trackerIntake: buildIntake(intake),
							});
						}}
					>
						<div className="grid gap-4 sm:grid-cols-2">
							<RequiredAgentField
								id="newProjectWorkerAgent"
								label={t("createProject.workerAgent")}
								placeholder={t("createProject.selectWorker")}
								value={workerAgent}
								authorized={agentOptions}
								installed={installedAgents}
								supported={supportedAgents}
								disabled={isLoadingAgents}
								labelClassName="agents-sheet-label"
								triggerClassName="agents-sheet-control"
								contentClassName="agents-sheet-menu"
								onChange={(value) => {
									setWorkerAgent(value);
									setWorkerAgentTouched(true);
								}}
							/>
							<RequiredAgentField
								id="newProjectOrchestratorAgent"
								label={t("createProject.orchestratorAgent")}
								placeholder={t("createProject.selectOrchestrator")}
								value={orchestratorAgent}
								authorized={agentOptions}
								installed={installedAgents}
								supported={supportedAgents}
								disabled={isLoadingAgents}
								labelClassName="agents-sheet-label"
								triggerClassName="agents-sheet-control"
								contentClassName="agents-sheet-menu"
								onChange={(value) => {
									setOrchestratorAgent(value);
									setOrchestratorAgentTouched(true);
								}}
							/>
							<RequiredAgentField
								id="newProjectReviewerAgent"
								label="Reviewer harness"
								placeholder="Select reviewer harness"
								value={reviewerAgent || AUTOMATIC_REVIEWER}
								authorized={reviewerCatalog.authorized}
								installed={reviewerCatalog.installed}
								supported={reviewerCatalog.supported}
								disabled={isLoadingAgents}
								labelClassName="agents-sheet-label"
								triggerClassName="agents-sheet-control"
								contentClassName="agents-sheet-menu"
								onChange={(value) => setReviewerAgent(value === AUTOMATIC_REVIEWER ? "" : value)}
							/>
							<div className="flex flex-col gap-1.5">
								<Label htmlFor="newProjectPermissionMode" className="agents-sheet-label">
									Permission mode
								</Label>
								<PermissionModeSelect
									id="newProjectPermissionMode"
									value={permissions}
									onChange={setPermissions}
									defaultLabel="Project default"
									disabled={isBusy}
									size="sm"
									className="w-full text-control agents-sheet-control"
								/>
							</div>
						</div>

						<div className="space-y-3 border-t border-border pt-4">
							{selectedHarnesses.map((harness) => {
								const label = harnessDisplayLabel(agents, effectiveModelAvailability, harness);
								const value = { harness, ...(modelDefaults[harness] ?? { model: "", effort: "" }) };
								return (
									<div key={harness} className="rounded-md border border-border px-3 py-3">
										<ModelAvailabilityField
											id={`newProjectModel-${harness}`}
											label={`${label} default model`}
											value={value}
											onChange={(next) =>
												setModelDefaults((current) => ({
													...current,
													[harness]: { model: next.model, effort: next.effort },
												}))
											}
											availability={effectiveModelAvailability}
											configuredPins={configuredModelPins}
											disabled={isBusy}
											isRefreshing={isRefreshingModels || modelAvailabilityQuery.isFetching}
											onRefresh={() => refreshModels().catch(() => undefined)}
											showHarness={false}
											emptyLabel="Harness default"
											showManualModelNotice
										/>
									</div>
								);
							})}
							{usingInventoryModelFallback && (
								<p className="mt-2 text-xs leading-row text-warning">
									Model catalogs are unavailable. Enter a model ID manually for the selected harness default.
								</p>
							)}
						</div>

						{isLoadingAgents && (
							<p className="text-xs leading-row text-[var(--color-text-agents-sheet-description)]">
								Loading harnesses...
							</p>
						)}

						<div className="flex items-center justify-between gap-3 text-xs leading-row text-[var(--color-text-agents-sheet-description)]">
							<span>Harness availability is cached.</span>
							<button
								type="button"
								className="shrink-0 rounded text-[var(--color-text-agents-sheet-title)] underline-offset-2 hover:underline disabled:pointer-events-none disabled:opacity-50"
								disabled={refreshAgentsMutation.isPending}
								onClick={() => refreshAgentsMutation.mutate()}
							>
								{refreshAgentsMutation.isPending ? "Refreshing..." : "Refresh harnesses"}
							</button>
						</div>

						{displayError && (
							<div className="flex items-center justify-between gap-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs leading-row text-destructive">
								<span>{displayError}</span>
								<button
									type="button"
									className="shrink-0 rounded text-[var(--color-text-agents-sheet-title)] underline-offset-2 hover:underline disabled:pointer-events-none disabled:opacity-50"
									disabled={refreshAgentsMutation.isPending}
									onClick={() => refreshAgentsMutation.mutate()}
								>
									Retry
								</button>
							</div>
						)}

						<div className="border-t border-[var(--color-border-agents-sheet)] pt-5">
							<IntakeFields
								form={intake}
								onChange={(patch) => setIntake((f) => ({ ...f, ...patch }))}
								compact
								controlClassName="agents-sheet-control"
								labelClassName="agents-sheet-label"
							/>
						</div>

						{repositorySetupNeeded && (
							<div className="rounded-lg border border-[var(--color-border-agents-sheet)] bg-[var(--color-bg-agents-sheet-control)]/80 px-3 py-2.5 text-xs leading-body-md text-[var(--color-text-agents-sheet-description)]">
								<p>
									If this folder needs Git setup, AO will initialize it and create the first commit before starting.
								</p>
								{repositorySetupWarning && <p className="mt-2 text-warning">{repositorySetupWarning}</p>}
							</div>
						)}

						{sheetError && (
							<div
								role="alert"
								className={
									sheetError.tone === "warning"
										? "flex gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2.5 text-xs leading-body-md"
										: "flex gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2.5 text-xs leading-body-md"
								}
							>
								<TriangleAlert
									className={
										sheetError.tone === "warning"
											? "mt-0.5 size-icon-sm shrink-0 text-warning"
											: "mt-0.5 size-icon-sm shrink-0 text-destructive"
									}
									aria-hidden="true"
								/>
								<div className="min-w-0 space-y-0.5">
									<p
										className={
											sheetError.tone === "warning"
												? "font-medium text-[var(--color-text-agents-sheet-title)]"
												: "font-medium text-destructive"
										}
									>
										{sheetError.title}
									</p>
									<p className="text-[var(--color-text-agents-sheet-description)]">{sheetError.message}</p>
								</div>
							</div>
						)}

						<div className="flex items-center justify-end gap-2 pt-1">
							<Button
								type="button"
								variant="outline"
								disabled={isBusy}
								className="rounded-lg border-[var(--color-border-agents-sheet)] bg-transparent text-[var(--color-text-agents-sheet-title)] hover:bg-interactive-hover"
								onClick={() => onOpenChange(false)}
							>
								Cancel
							</Button>
							<Button type="submit" variant="primary" className="rounded-lg" disabled={!canSubmit}>
								{isInitializing
									? "Setting up..."
									: isCreating
										? "Creating..."
										: kind === "workspace"
											? "Create workspace and start"
											: "Create and start"}
							</Button>
						</div>
					</form>
				</Dialog.Content>
			</Dialog.Portal>
		</Dialog.Root>
	);
}

export const RequiredAgentField = memo(function RequiredAgentField({
	authorized,
	disabled = false,
	hint,
	icon,
	id,
	invalid = false,
	installed,
	label,
	onChange,
	placeholder,
	supported,
	triggerClassName,
	labelClassName,
	contentClassName,
	fieldGapClassName = "gap-1.5",
	value,
	variant = "stacked",
}: {
	authorized?: AgentInfo[];
	disabled?: boolean;
	hint?: string;
	icon?: LucideIcon;
	id: string;
	invalid?: boolean;
	installed?: AgentInfo[];
	label: string;
	onChange: (value: string) => void;
	placeholder: string;
	supported?: AgentInfo[];
	triggerClassName?: string;
	labelClassName?: string;
	contentClassName?: string;
	fieldGapClassName?: string;
	value: string;
	variant?: "stacked" | "settings-row" | "chip";
}) {
	const catalog = selectableAgentCatalog({ authorized, installed, supported }, { current: value });
	const supportedAgents = catalog.supported ?? [];
	const installedAgents = catalog.installed ?? [];
	const authorizedAgents = catalog.authorized ?? [];
	const authorizedIds = new Set(authorizedAgents.map((agent) => agent.id));
	const installedById = new Map(installedAgents.map((agent) => [agent.id, agent]));
	const options = supportedAgents
		.map((agent) => {
			const installedAgent = installedById.get(agent.id);
			const authStatus = installedAgent?.authStatus;
			const isAuthorized = authorizedIds.has(agent.id) || authStatus === "authorized";
			const isAuthUnknown = Boolean(installedAgent) && !isAuthorized && authStatus !== "unauthorized";
			const isSelectable = isAuthorized || isAuthUnknown;
			const rank = isAuthorized ? 0 : isAuthUnknown ? 1 : installedAgent ? 2 : 3;
			return {
				...agent,
				disabled: !isSelectable,
				priorityRank: DEFAULT_AGENT_PRIORITY_RANK.get(agent.id) ?? Number.MAX_SAFE_INTEGER,
				rank,
				reason: !installedAgent ? "Needs install" : isAuthUnknown ? "Auth unknown" : !isAuthorized ? "Needs auth" : "",
				warning: isAuthUnknown,
			};
		})
		.sort((a, b) => a.rank - b.rank || a.priorityRank - b.priorityRank || agentLabelCompare(a, b));
	const selectedOption = options.find((agent) => agent.id === value);

	if (variant === "settings-row") {
		return (
			<SettingsRow icon={icon} label={label}>
				<SettingsOptionMenu
					aria-label={label}
					value={value}
					placeholder={placeholder}
					options={options.map((agent) => ({ value: agent.id, label: agent.label, disabled: agent.disabled }))}
					disabled={disabled}
					onChange={onChange}
					triggerClassName={invalid ? "text-error" : undefined}
					menuClassName="settings-agent-menu-surface"
					menuItemClassName="settings-agent-menu-item"
					renderTrigger={(selected, triggerPlaceholder) => (
						<>
							{selected ? <AgentAvatar provider={selected.value} className="size-icon-lg" /> : null}
							<span className="min-w-0 truncate">{selected?.label ?? triggerPlaceholder}</span>
						</>
					)}
					renderMenuItem={(option, selected) => {
						const agent = options.find((entry) => entry.id === option.value);
						if (!agent) return option.label;
						return (
							<AgentSelectMenuItem
								agentId={agent.id}
								label={agent.label}
								selected={selected}
								status={agent.reason}
								statusTone={agent.warning ? "warning" : agent.reason ? "muted" : "success"}
								disabled={agent.disabled}
							/>
						);
					}}
				/>
			</SettingsRow>
		);
	}

	if (variant === "chip") {
		return (
			<Select value={value} onValueChange={onChange} disabled={disabled}>
				<SelectTrigger
					id={id}
					size="sm"
					className={cn(
						"composer-chip h-control-md! bg-(--color-bg-composer-chip)! px-2! text-control! [&_svg]:size-icon-sm",
						invalid && "text-error",
						triggerClassName,
					)}
					aria-label={label}
					aria-invalid={invalid || undefined}
				>
					<SelectValue placeholder={placeholder}>
						{selectedOption ? (
							<span className="flex min-w-0 items-center gap-2">
								<AgentAvatar provider={selectedOption.id} className="size-icon-base" decorative />
								<span className="min-w-0 truncate">{selectedOption.label}</span>
							</span>
						) : null}
					</SelectValue>
				</SelectTrigger>
				<SelectContent position="popper" side="bottom" align="start" sideOffset={6} className={contentClassName}>
					{options.map((agent) => (
						<SelectItem key={agent.id} value={agent.id} disabled={agent.disabled}>
							<AgentSelectMenuItem
								agentId={agent.id}
								label={agent.label}
								selected={value === agent.id}
								status={agent.reason}
								statusTone={agent.warning ? "warning" : agent.reason ? "muted" : "success"}
								disabled={agent.disabled}
							/>
						</SelectItem>
					))}
				</SelectContent>
			</Select>
		);
	}

	return (
		<div className={cn("flex flex-col", fieldGapClassName)}>
			<div className="flex min-w-0 items-baseline gap-1.5">
				<Label htmlFor={id} className={cn("text-xs font-medium text-muted-foreground", labelClassName)}>
					{label}
				</Label>
				{hint ? <FieldDefaultHint text={hint} /> : null}
			</div>
			<Select value={value} onValueChange={onChange} disabled={disabled}>
				<SelectTrigger
					id={id}
					size="sm"
					className={cn("w-full text-control", triggerClassName)}
					aria-invalid={invalid || undefined}
				>
					<SelectValue placeholder={placeholder} />
				</SelectTrigger>
				<SelectContent
					position="popper"
					side="bottom"
					align="start"
					sideOffset={4}
					className={cn("max-h-select-menu-max!", contentClassName)}
				>
					{options.map((agent) => (
						<SelectItem
							key={agent.id}
							value={agent.id}
							disabled={agent.disabled}
							className="[&>span:last-child]:w-full"
						>
							<span className="flex min-w-0 w-full items-center justify-between gap-4">
								<span className="truncate">{agent.label}</span>
								{agent.reason && (
									<span className="inline-flex shrink-0 items-center gap-1 text-caption text-muted-foreground">
										{agent.warning && <TriangleAlert className="size-3 text-warning" aria-hidden="true" />}
										{agent.reason}
									</span>
								)}
							</span>
						</SelectItem>
					))}
				</SelectContent>
			</Select>
		</div>
	);
});

export function defaultAuthorizedAgent(authorizedAgents: AgentInfo[]): string {
	const authorizedIds = new Set(authorizedAgents.map((agent) => agent.id));
	const prioritized = DEFAULT_AGENT_PRIORITY.find((agent) => authorizedIds.has(agent));
	if (prioritized) return prioritized;
	return [...authorizedAgents].sort(agentLabelCompare)[0]?.id ?? "";
}

function uniqueSelectedHarnesses(harnesses: string[]): string[] {
	const seen = new Set<string>();
	const out: string[] = [];
	for (const harness of harnesses) {
		if (!harness || seen.has(harness)) continue;
		seen.add(harness);
		out.push(harness);
	}
	return out;
}

function selectedModelDefaults(
	harnesses: string[],
	modelDefaults: Record<string, { model: string; effort: string }>,
): Record<string, { model: string; effort: string }> {
	const out: Record<string, { model: string; effort: string }> = {};
	for (const harness of harnesses) {
		const pair = modelDefaults[harness];
		if (pair) out[harness] = pair;
	}
	return out;
}

function harnessDisplayLabel(
	catalog: AgentInventory | undefined,
	availability: AgentModelAvailabilityResponse | undefined,
	harness: string,
): string {
	return agentDisplayLabel(catalog, availability, harness);
}

function reviewerAgentCatalog(
	catalog: AgentInventory | undefined,
	availability: AgentModelAvailabilityResponse | undefined,
	currentHarness: string,
): AgentInventory {
	const reviewerIDs = new Set<string>();
	for (const harness of availability?.harnesses ?? []) {
		if (harness.reviewerCapable) reviewerIDs.add(harness.id);
	}
	for (const agents of [catalog?.supported, catalog?.installed, catalog?.authorized]) {
		for (const agent of agents ?? []) {
			if (agent.reviewerCapable) reviewerIDs.add(agent.id);
		}
	}
	if (currentHarness) reviewerIDs.add(currentHarness);
	const automatic: AgentInfo = {
		id: AUTOMATIC_REVIEWER,
		label: "Automatic independent reviewer",
		authStatus: "authorized",
		reviewerCapable: true,
	};
	const filteredCatalog = {
		supported: catalog?.supported?.filter((agent) => reviewerIDs.has(agent.id)),
		installed: catalog?.installed?.filter((agent) => reviewerIDs.has(agent.id)),
		authorized: catalog?.authorized?.filter((agent) => reviewerIDs.has(agent.id)),
	};
	return selectableAgentCatalog(filteredCatalog, {
		current: currentHarness,
		currentLabel: currentHarness ? agentDisplayLabel(catalog, availability, currentHarness) : undefined,
		includeDefault: automatic,
		reviewerOnly: true,
	});
}
