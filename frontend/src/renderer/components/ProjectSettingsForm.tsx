import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { components } from "../../api/schema";
import { agentsQueryKey, agentsQueryOptions, refreshAgents } from "../hooks/useAgentsQuery";
import {
	modelAvailabilityQueryKey,
	type AgentModelAvailabilityResponse,
	useModelAvailabilityQuery,
	useRefreshModelAvailability,
} from "../hooks/useModelAvailabilityQuery";
import { useWorkspaceQuery, workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { captureRendererEvent } from "../lib/telemetry";
import { spawnOrchestrator } from "../lib/spawn-orchestrator";
import { newestActiveOrchestrator } from "../types/workspace";
import { modelAvailabilityFromAgentInventory, RequiredAgentField } from "./CreateProjectAgentSheet";
import { DashboardSubhead } from "./DashboardSubhead";
import { buildIntake, deriveGitHubRepo, IntakeFields, type IntakeForm, intakeNeedsRule } from "./IntakeFields";
import { type ConfiguredModelPin, ModelAvailabilityField, type ModelSelection } from "./ModelAvailabilityField";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Label } from "./ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
import {
	buildWorkerMix,
	parseMaxLiveWorkers,
	toWorkerMixForm,
	WorkerMixFields,
	workerMixInvalid,
	workerMixRowError,
	workerMixTotal,
} from "./WorkerMixFields";

type Project = components["schemas"]["Project"];
type ProjectConfig = components["schemas"]["ProjectConfig"];
type AgentConfig = components["schemas"]["AgentConfig"];
type AgentInfo = components["schemas"]["AgentInfo"];
type TrackerIntakeConfig = components["schemas"]["TrackerIntakeConfig"];

const PERMISSION_MODE_OPTIONS = [
	{ value: "default", label: "Default" },
	{ value: "accept-edits", label: "Accept edits" },
	{ value: "auto", label: "Auto" },
	{ value: "bypass-permissions", label: "Bypass permissions" },
] as const;

const ROLE_PROMPT_OPTIONS = ["worker", "orchestrator", "reviewer"] as const;
type RolePromptRole = (typeof ROLE_PROMPT_OPTIONS)[number];

const projectQueryKey = (id: string) => ["project", id] as const;
const rolePromptQueryKey = (id: string, role: string) => ["project", id, "role-prompt", role] as const;

export function ProjectSettingsForm({ projectId }: { projectId: string }) {
	const queryClient = useQueryClient();

	const query = useQuery({
		queryKey: projectQueryKey(projectId),
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/projects/{id}", {
				params: { path: { id: projectId } },
			});
			if (error) throw new Error(apiErrorMessage(error));
			if (data?.status !== "ok") throw new Error("Project config is unavailable (degraded).");
			return data.project as Project;
		},
	});

	if (query.isLoading) {
		return <CenteredNote>Loading project settings…</CenteredNote>;
	}
	if (query.isError || !query.data) {
		return (
			<CenteredNote>{query.error instanceof Error ? query.error.message : "Could not load project."}</CenteredNote>
		);
	}

	return (
		<div className="flex h-full min-h-0 flex-col bg-background text-foreground">
			<DashboardSubhead title="Settings" subtitle={query.data.path} />
			<div className="min-h-0 flex-1 overflow-y-auto p-4.5">
				<SettingsBody
					key={projectId}
					project={query.data}
					onSaved={() => queryClient.invalidateQueries({ queryKey: workspaceQueryKey })}
					projectId={projectId}
				/>
			</div>
		</div>
	);
}

function SettingsBody({ project, projectId, onSaved }: { project: Project; projectId: string; onSaved: () => void }) {
	const queryClient = useQueryClient();
	const workspaceQuery = useWorkspaceQuery();
	const config = project.config ?? {};
	const workspace = workspaceQuery.data?.find((item) => item.id === projectId);
	const activeOrchestrator = newestActiveOrchestrator(workspace?.sessions ?? []);
	const intake: TrackerIntakeConfig = config.trackerIntake ?? {};
	const firstReviewer = config.reviewers?.[0];
	const initialProjectHarness = firstConfiguredHarness(config.agentConfig);
	const initialWorkerAgent = config.worker?.agent ?? "";
	const initialOrchestratorAgent = config.orchestrator?.agent ?? "";
	const initialReviewerHarness = firstReviewer?.harness ?? "";
	const [form, setForm] = useState({
		defaultBranch: config.defaultBranch ?? project.defaultBranch ?? "",
		sessionPrefix: config.sessionPrefix ?? "",
		projectHarness: initialProjectHarness,
		model: config.agentConfig?.model ?? "",
		effort: config.agentConfig?.effort ?? "",
		modelByHarness: toHarnessModelForm(config.agentConfig?.modelByHarness),
		workerAgent: initialWorkerAgent,
		workerModels: toRoleHarnessModelForm(config.worker?.agentConfig, initialWorkerAgent),
		orchestratorAgent: initialOrchestratorAgent,
		orchestratorModels: toRoleHarnessModelForm(config.orchestrator?.agentConfig, initialOrchestratorAgent),
		permissions: config.agentConfig?.permissions ?? "",
		reviewerHarness: initialReviewerHarness,
		reviewerModels: toRoleHarnessModelForm(firstReviewer?.agentConfig, initialReviewerHarness),
		agentRules: config.agentRules ?? "",
		agentRulesFile: config.agentRulesFile ?? "",
		orchestratorRules: config.orchestratorRules ?? "",
		orchestratorRulesFile: config.orchestratorRulesFile ?? "",
		reviewerRules: config.reviewerRules ?? "",
		reviewerRulesFile: config.reviewerRulesFile ?? "",
		intakeEnabled: intake.enabled ?? false,
		intakeRepo: intake.repo ?? "",
		intakeAssignee: intake.assignee ?? "",
		workerMix: toWorkerMixForm(config.workerMix),
		maxLiveWorkers: config.maxLiveWorkers ? String(config.maxLiveWorkers) : "",
	});
	const [savedAt, setSavedAt] = useState<number | null>(null);
	const [replacementError, setReplacementError] = useState<string | null>(null);
	const [validationError, setValidationError] = useState<string | null>(null);
	const missingRequiredAgent = form.workerAgent === "" || form.orchestratorAgent === "";
	const agentsQuery = useQuery(agentsQueryOptions);
	const modelAvailabilityQuery = useModelAvailabilityQuery();
	const { refresh: refreshModels, isRefreshing: isRefreshingModels } = useRefreshModelAvailability();
	const agentCatalog = agentsQuery.data;
	const effectiveModelAvailability = modelAvailabilityQuery.data ?? modelAvailabilityFromAgentInventory(agentCatalog);
	const refreshAgentsMutation = useMutation({
		mutationFn: refreshAgents,
		onSuccess: (next) => queryClient.setQueryData(agentsQueryKey, next),
	});

	// The Electron app only registers git projects today, so the daemon always has a usable
	// git origin to derive owner/repo from (trackerRepo() in observer.go) when
	// trackerIntake.repo is unset — there's no manual override input here. This mirrors that
	// same derivation client-side purely for display (a link to the repo being polled).
	const intakeForm: IntakeForm = {
		enabled: form.intakeEnabled,
		repo: form.intakeRepo,
		assignee: form.intakeAssignee,
	};
	const patchIntake = (patch: Partial<IntakeForm>) =>
		setForm((f) => ({
			...f,
			intakeEnabled: patch.enabled ?? f.intakeEnabled,
			intakeRepo: patch.repo ?? f.intakeRepo,
			intakeAssignee: patch.assignee ?? f.intakeAssignee,
		}));
	const effectiveIntakeRepo = form.intakeRepo.trim() || deriveGitHubRepo(project.repo);
	const intakeIncomplete = intakeNeedsRule(intakeForm);

	const mutation = useMutation({
		mutationFn: async () => {
			void captureRendererEvent("ao.renderer.settings_save_requested", { project_id: projectId });
			// PUT replaces the whole config; merge the edited fields over what loaded
			// so we don't drop env/symlinks/postCreate the form doesn't expose.
			const next: ProjectConfig = {
				...config,
				defaultBranch: form.defaultBranch || undefined,
				sessionPrefix: form.sessionPrefix || undefined,
				worker: {
					...config.worker,
					agent: form.workerAgent,
					agentConfig: buildRoleAgentConfig(
						config.worker?.agentConfig,
						initialWorkerAgent,
						form.workerAgent,
						form.workerModels,
					),
				},
				orchestrator: {
					...config.orchestrator,
					agent: form.orchestratorAgent,
					agentConfig: buildRoleAgentConfig(
						config.orchestrator?.agentConfig,
						initialOrchestratorAgent,
						form.orchestratorAgent,
						form.orchestratorModels,
					),
				},
				agentConfig: buildAgentConfig(
					config.agentConfig,
					form.model,
					form.effort,
					form.permissions,
					form.modelByHarness,
				),
				reviewers: buildReviewerConfig(
					config.reviewers,
					form.reviewerHarness,
					initialReviewerHarness,
					form.reviewerModels,
				),
				agentRules: form.agentRules.trim() || undefined,
				agentRulesFile: form.agentRulesFile.trim() || undefined,
				orchestratorRules: form.orchestratorRules.trim() || undefined,
				orchestratorRulesFile: form.orchestratorRulesFile.trim() || undefined,
				reviewerRules: form.reviewerRules.trim() || undefined,
				reviewerRulesFile: form.reviewerRulesFile.trim() || undefined,
				trackerIntake: buildIntake(intakeForm),
				workerMix: buildWorkerMix(form.workerMix),
				maxLiveWorkers: parseMaxLiveWorkers(form.maxLiveWorkers),
			};
			const { error } = await apiClient.PUT("/api/v1/projects/{id}/config", {
				params: { path: { id: projectId } },
				body: { config: next },
			});
			if (error) throw new Error(apiErrorMessage(error));
			if (
				form.orchestratorAgent !== initialOrchestratorAgent ||
				(activeOrchestrator && activeOrchestrator.provider !== form.orchestratorAgent)
			) {
				try {
					await spawnOrchestrator(projectId, "settings", true);
				} catch (error) {
					return {
						replacementError: error instanceof Error ? error.message : "Could not replace orchestrator",
					};
				}
			}
			return { replacementError: null };
		},
		onSuccess: (result) => {
			void captureRendererEvent("ao.renderer.settings_save_succeeded", { project_id: projectId });
			setSavedAt(Date.now());
			setReplacementError(result.replacementError);
			setValidationError(null);
			void queryClient.invalidateQueries({ queryKey: ["project", projectId] });
			void queryClient.invalidateQueries({ queryKey: modelAvailabilityQueryKey });
			onSaved();
		},
		onError: () => {
			void captureRendererEvent("ao.renderer.settings_save_failed", { project_id: projectId });
		},
	});

	return (
		<form
			noValidate
			className="mx-auto flex max-w-2xl flex-col gap-4"
			onSubmit={(event) => {
				event.preventDefault();
				setSavedAt(null);
				setReplacementError(null);
				if (missingRequiredAgent) {
					setValidationError("Worker and orchestrator agents are required.");
					return;
				}
				if (intakeIncomplete) {
					setValidationError("Enabling intake requires an assignee.");
					return;
				}
				const rowError = workerMixRowError(form.workerMix);
				if (rowError) {
					setValidationError(rowError);
					return;
				}
				if (workerMixInvalid(form.workerMix)) {
					setValidationError(`Worker mix weights must sum to 100 (currently ${workerMixTotal(form.workerMix)}).`);
					return;
				}
				setValidationError(null);
				mutation.mutate();
			}}
		>
			<Card>
				<CardHeader>
					<CardTitle className="text-control">Identity</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-2 font-mono text-xs text-muted-foreground">
					<ReadonlyRow label="id" value={project.id} />
					<ReadonlyRow label="kind" value={project.kind === "workspace" ? "workspace" : "single repo"} />
					<ReadonlyRow label="path" value={project.path} />
					<ReadonlyRow label="repo" value={project.repo || "—"} />
				</CardContent>
			</Card>

			{project.kind === "workspace" && (
				<Card>
					<CardHeader>
						<CardTitle className="text-[13px]">Workspace repos</CardTitle>
					</CardHeader>
					<CardContent className="flex flex-col gap-2">
						{project.workspaceRepos?.length ? (
							project.workspaceRepos.map((repo) => (
								<div
									key={repo.name}
									className="grid grid-cols-[minmax(0,120px)_minmax(0,1fr)] gap-3 rounded-md border border-border px-3 py-2 font-mono text-[12px]"
								>
									<span className="truncate text-foreground">{repo.name}</span>
									<span className="min-w-0 truncate text-muted-foreground">
										{repo.relativePath}
										{repo.repo ? ` · ${repo.repo}` : ""}
									</span>
								</div>
							))
						) : (
							<p className="text-[12px] text-muted-foreground">No child repositories are registered.</p>
						)}
					</CardContent>
				</Card>
			)}

			<Card>
				<CardHeader>
					<CardTitle className="text-control">Worktrees</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<Field label="Default branch" htmlFor="defaultBranch">
						<input
							id="defaultBranch"
							className="h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
							value={form.defaultBranch}
							onChange={(e) => setForm((f) => ({ ...f, defaultBranch: e.target.value }))}
							placeholder="main"
						/>
					</Field>
					<Field label="Session prefix" htmlFor="sessionPrefix">
						<input
							id="sessionPrefix"
							className="h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
							value={form.sessionPrefix}
							onChange={(e) => setForm((f) => ({ ...f, sessionPrefix: e.target.value }))}
							placeholder="ao"
						/>
					</Field>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="text-control">Harnesses and models</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<p className="text-xs leading-row text-muted-foreground">
						Each role keeps an independent harness-native model and effort pair. Switching harnesses restores that
						harness's saved pair or clears the fields when none exists.
					</p>
					<HarnessModelRow
						id="project-model"
						harnessLabel="Project model harness"
						modelLabel="Project model and effort"
						selection={projectSelection(form.projectHarness, form.model, form.effort, form.modelByHarness)}
						configuredPins={[
							{ harness: "", model: form.model, effort: form.effort },
							...modelPins(form.modelByHarness),
						]}
						agentCatalog={agentCatalog}
						availability={effectiveModelAvailability}
						allowDefaultHarness
						allowScalar
						defaultHarnessLabel="Scalar fallback"
						isRefreshingModels={isRefreshingModels || modelAvailabilityQuery.isFetching}
						onRefreshModels={refreshModels}
						onChange={(selection) =>
							setForm((f) =>
								selection.harness === ""
									? { ...f, projectHarness: "", model: selection.model, effort: selection.effort }
									: {
											...f,
											projectHarness: selection.harness,
											modelByHarness: patchHarnessSelection(f.modelByHarness, selection),
										},
							)
						}
					/>
					<HarnessModelRow
						id="worker-model"
						harnessLabel="Default worker agent"
						modelLabel="Worker model and effort"
						selection={roleSelection(form.workerAgent, form.workerModels)}
						configuredPins={modelPins(form.workerModels)}
						agentCatalog={agentCatalog}
						availability={effectiveModelAvailability}
						invalidHarness={validationError !== null && form.workerAgent === ""}
						isRefreshingModels={isRefreshingModels || modelAvailabilityQuery.isFetching}
						onRefreshModels={refreshModels}
						onChange={(selection) =>
							setForm((f) => ({
								...f,
								workerAgent: selection.harness,
								workerModels: patchHarnessSelection(f.workerModels, selection),
							}))
						}
					/>
					<HarnessModelRow
						id="orchestrator-model"
						harnessLabel="Default orchestrator agent"
						modelLabel="Orchestrator model and effort"
						selection={roleSelection(form.orchestratorAgent, form.orchestratorModels)}
						configuredPins={modelPins(form.orchestratorModels)}
						agentCatalog={agentCatalog}
						availability={effectiveModelAvailability}
						invalidHarness={validationError !== null && form.orchestratorAgent === ""}
						isRefreshingModels={isRefreshingModels || modelAvailabilityQuery.isFetching}
						onRefreshModels={refreshModels}
						onChange={(selection) =>
							setForm((f) => ({
								...f,
								orchestratorAgent: selection.harness,
								orchestratorModels: patchHarnessSelection(f.orchestratorModels, selection),
							}))
						}
					/>
					<HarnessModelRow
						id="reviewer-model"
						harnessLabel="Default reviewer agent"
						modelLabel="Reviewer model and effort"
						selection={roleSelection(form.reviewerHarness, form.reviewerModels)}
						configuredPins={modelPins(form.reviewerModels)}
						agentCatalog={agentCatalog}
						availability={effectiveModelAvailability}
						allowDefaultHarness
						defaultHarnessLabel="Automatic independent reviewer"
						reviewerOnly
						isRefreshingModels={isRefreshingModels || modelAvailabilityQuery.isFetching}
						onRefreshModels={refreshModels}
						onChange={(selection) =>
							setForm((f) => ({
								...f,
								reviewerHarness: selection.harness,
								reviewerModels: patchHarnessSelection(f.reviewerModels, selection),
							}))
						}
					/>
					<div className="flex items-center justify-between gap-3 text-xs leading-row text-muted-foreground">
						<span>Agent availability is cached.</span>
						<button
							type="button"
							className="shrink-0 rounded text-foreground underline-offset-2 hover:underline disabled:pointer-events-none disabled:opacity-50"
							disabled={refreshAgentsMutation.isPending}
							onClick={() => refreshAgentsMutation.mutate()}
						>
							{refreshAgentsMutation.isPending ? "Refreshing..." : "Refresh agents"}
						</button>
					</div>
					{refreshAgentsMutation.isError && (
						<p className="text-xs leading-row text-error">
							{refreshAgentsMutation.error instanceof Error
								? refreshAgentsMutation.error.message
								: "Could not refresh agent catalog."}
						</p>
					)}
					{missingRequiredAgent && (
						<p className="text-xs leading-row text-error">Worker and orchestrator agents are required.</p>
					)}
					{modelAvailabilityQuery.isError && (
						<p className="text-xs leading-row text-warning">
							Model catalogs are unavailable; saved pins and the agent inventory remain usable.
						</p>
					)}
					<Field label="Permission mode" htmlFor="permissionMode">
						<PermissionModeSelect
							id="permissionMode"
							value={form.permissions}
							onChange={(v) => setForm((f) => ({ ...f, permissions: v }))}
						/>
					</Field>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="text-control">Role instructions</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<p className="text-xs leading-row text-muted-foreground">
						Operator-controlled standing instructions injected into each role's prompt on the next spawn,
						content-preserving (only surrounding whitespace is normalized). Inline content is loaded first, then file
						content is appended after it. A configured repo-relative or absolute file path that is missing, empty, or
						oversized fails the spawn loudly rather than silently dropping the instructions.
					</p>
					<RulesField
						label="Worker instructions"
						fileLabel="Worker instructions file path (repo-relative or absolute)"
						idPrefix="agentRules"
						rules={form.agentRules}
						file={form.agentRulesFile}
						onRules={(v) => setForm((f) => ({ ...f, agentRules: v }))}
						onFile={(v) => setForm((f) => ({ ...f, agentRulesFile: v }))}
					/>
					<RulesField
						label="Orchestrator instructions"
						fileLabel="Orchestrator instructions file path (repo-relative or absolute)"
						idPrefix="orchestratorRules"
						rules={form.orchestratorRules}
						file={form.orchestratorRulesFile}
						onRules={(v) => setForm((f) => ({ ...f, orchestratorRules: v }))}
						onFile={(v) => setForm((f) => ({ ...f, orchestratorRulesFile: v }))}
					/>
					<RulesField
						label="Reviewer instructions"
						fileLabel="Reviewer instructions file path (repo-relative or absolute)"
						idPrefix="reviewerRules"
						rules={form.reviewerRules}
						file={form.reviewerRulesFile}
						onRules={(v) => setForm((f) => ({ ...f, reviewerRules: v }))}
						onFile={(v) => setForm((f) => ({ ...f, reviewerRulesFile: v }))}
					/>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="text-control">Prompt inspector</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-3">
					<p className="text-xs leading-row text-muted-foreground">
						The exact, fully-assembled system prompt a role receives for this project — base scaffold plus every
						injected instruction source. Reflects saved config; save your changes above to see them here.
					</p>
					<RolePromptInspector projectId={projectId} />
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="text-control">Worker mix</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<WorkerMixFields
						buckets={form.workerMix}
						onChange={(next) => setForm((f) => ({ ...f, workerMix: next }))}
						agentCatalog={agentCatalog}
						availability={effectiveModelAvailability}
						isRefreshing={isRefreshingModels || modelAvailabilityQuery.isFetching}
						onRefresh={refreshModels}
					/>
					<Field label="Max live workers (0 = unlimited)" htmlFor="maxLiveWorkers">
						<input
							id="maxLiveWorkers"
							type="number"
							min={0}
							className="h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
							value={form.maxLiveWorkers}
							onChange={(e) => setForm((f) => ({ ...f, maxLiveWorkers: e.target.value }))}
							placeholder="0"
						/>
					</Field>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="text-control">Tracker intake</CardTitle>
				</CardHeader>
				<CardContent>
					<IntakeFields form={intakeForm} onChange={patchIntake} repoPreview={{ value: effectiveIntakeRepo }} />
				</CardContent>
			</Card>

			<div className="flex items-center gap-3">
				<Button type="submit" variant="primary" disabled={mutation.isPending}>
					{mutation.isPending ? "Saving…" : "Save changes"}
				</Button>
				{validationError && <span className="text-xs text-error">{validationError}</span>}
				{mutation.isError && (
					<span className="text-xs text-error">
						{mutation.error instanceof Error ? mutation.error.message : "Save failed"}
					</span>
				)}
				{savedAt && !mutation.isPending && !mutation.isError && <span className="text-xs text-success">Saved.</span>}
				{replacementError && !mutation.isPending && !mutation.isError && (
					<span className="text-xs text-warning">Orchestrator restart failed: {replacementError}</span>
				)}
			</div>
		</form>
	);
}

function PermissionModeSelect({
	id,
	value,
	onChange,
}: {
	id: string;
	value: string;
	onChange: (value: string) => void;
}) {
	return (
		<Select value={value || "__default__"} onValueChange={(v) => onChange(v === "__default__" ? "" : v)}>
			<SelectTrigger id={id} className="h-control-form w-full text-control">
				<SelectValue />
			</SelectTrigger>
			<SelectContent>
				<SelectItem value="__default__">Project default</SelectItem>
				{PERMISSION_MODE_OPTIONS.map((opt) => (
					<SelectItem key={opt.value} value={opt.value}>
						{opt.label}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}

const DEFAULT_HARNESS_ID = "__project_default__";

function HarnessModelRow({
	id,
	harnessLabel,
	modelLabel,
	selection,
	configuredPins,
	agentCatalog,
	availability,
	allowDefaultHarness = false,
	allowScalar = false,
	defaultHarnessLabel = "Project default",
	reviewerOnly = false,
	invalidHarness = false,
	isRefreshingModels,
	onRefreshModels,
	onChange,
}: {
	id: string;
	harnessLabel: string;
	modelLabel: string;
	selection: ModelSelection;
	configuredPins: ConfiguredModelPin[];
	agentCatalog: { authorized?: AgentInfo[]; installed?: AgentInfo[]; supported?: AgentInfo[] } | undefined;
	availability?: AgentModelAvailabilityResponse;
	allowDefaultHarness?: boolean;
	allowScalar?: boolean;
	defaultHarnessLabel?: string;
	reviewerOnly?: boolean;
	invalidHarness?: boolean;
	isRefreshingModels: boolean;
	onRefreshModels: () => void | Promise<unknown>;
	onChange: (selection: ModelSelection) => void;
}) {
	const scopedAvailability = reviewerOnly ? reviewerModelAvailability(availability, selection.harness) : availability;
	const scopedCatalog = modelHarnessCatalog(
		agentCatalog,
		availability,
		selection.harness,
		reviewerOnly,
		allowDefaultHarness,
		defaultHarnessLabel,
	);
	const harnessValue = allowDefaultHarness && selection.harness === "" ? DEFAULT_HARNESS_ID : selection.harness;
	const changeHarness = (rawHarness: string) => {
		const harness = rawHarness === DEFAULT_HARNESS_ID ? "" : rawHarness;
		const saved = configuredPins.find((pin) => pin.harness === harness);
		onChange({ harness, model: saved?.model ?? "", effort: saved?.effort ?? "" });
	};

	return (
		<div className="grid gap-3 rounded-md border border-border px-3 py-3 md:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
			<RequiredAgentField
				id={`${id}-harness`}
				value={harnessValue}
				placeholder={`Select ${harnessLabel.toLowerCase()}`}
				label={harnessLabel}
				authorized={scopedCatalog.authorized}
				installed={scopedCatalog.installed}
				supported={scopedCatalog.supported}
				invalid={invalidHarness}
				onChange={changeHarness}
			/>
			<ModelAvailabilityField
				id={id}
				label={modelLabel}
				value={selection}
				onChange={onChange}
				availability={scopedAvailability}
				configuredPins={configuredPins}
				disabled={selection.harness === "" && !allowScalar}
				isRefreshing={isRefreshingModels}
				onRefresh={onRefreshModels}
				showHarness={false}
				emptyLabel="Inherit default"
			/>
		</div>
	);
}

const controlInputClass =
	"h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak";

function RulesField({
	label,
	fileLabel,
	idPrefix,
	rules,
	file,
	onRules,
	onFile,
}: {
	label: string;
	fileLabel: string;
	idPrefix: string;
	rules: string;
	file: string;
	onRules: (value: string) => void;
	onFile: (value: string) => void;
}) {
	return (
		<div className="flex flex-col gap-2 rounded-md border border-border p-3">
			<Field label={label} htmlFor={`${idPrefix}Inline`}>
				<textarea
					id={`${idPrefix}Inline`}
					className="min-h-[64px] w-full resize-y rounded-md border border-input bg-transparent px-2.5 py-1.5 font-mono text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
					value={rules}
					onChange={(e) => onRules(e.target.value)}
					placeholder="(none)"
				/>
			</Field>
			<Field label={fileLabel} htmlFor={`${idPrefix}File`}>
				<input
					id={`${idPrefix}File`}
					className={controlInputClass}
					value={file}
					onChange={(e) => onFile(e.target.value)}
					placeholder="docs/role-rules.md"
				/>
			</Field>
		</div>
	);
}

function RolePromptInspector({ projectId }: { projectId: string }) {
	const [role, setRole] = useState<RolePromptRole>("worker");
	const query = useQuery({
		queryKey: rolePromptQueryKey(projectId, role),
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/projects/{id}/roles/{role}/prompt", {
				params: { path: { id: projectId, role } },
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not load the assembled prompt."));
			return data.prompt;
		},
	});
	return (
		<div className="flex flex-col gap-2">
			<Field label="Role" htmlFor="rolePromptRole">
				<Select value={role} onValueChange={(v) => setRole(v as RolePromptRole)}>
					<SelectTrigger id="rolePromptRole" className="h-control-form w-full text-control">
						<SelectValue />
					</SelectTrigger>
					<SelectContent>
						{ROLE_PROMPT_OPTIONS.map((opt) => (
							<SelectItem key={opt} value={opt}>
								{opt}
							</SelectItem>
						))}
					</SelectContent>
				</Select>
			</Field>
			{query.isPending ? (
				<p className="text-xs text-muted-foreground">Loading…</p>
			) : query.isError ? (
				<p className="text-xs text-error">
					{query.error instanceof Error ? query.error.message : "Could not load the assembled prompt."}
				</p>
			) : (
				<pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-md bg-terminal px-3 py-3 font-mono text-xs leading-row text-terminal-foreground">
					{query.data}
				</pre>
			)}
		</div>
	);
}

function Field({ label, htmlFor, children }: { label: string; htmlFor?: string; children: React.ReactNode }) {
	return (
		<div className="flex flex-col gap-1.5">
			<Label htmlFor={htmlFor} className="text-xs text-muted-foreground">
				{label}
			</Label>
			{children}
		</div>
	);
}

function ReadonlyRow({ label, value }: { label: string; value: string }) {
	return (
		<div className="flex items-center gap-3">
			<span className="w-12 shrink-0 text-passive">{label}</span>
			<span className="min-w-0 flex-1 truncate text-foreground">{value}</span>
		</div>
	);
}

function CenteredNote({ children }: { children: React.ReactNode }) {
	return (
		<div className="grid h-full place-items-center bg-background p-6 text-center text-xs text-passive">{children}</div>
	);
}

// Drop an object whose every value is undefined so we send `undefined` (omit)
// rather than an empty {} the daemon would persist.
function blankToUndefined<T extends object>(obj: T): T | undefined {
	return Object.values(obj).some((v) => v !== undefined) ? obj : undefined;
}

type HarnessModelForm = Record<string, { model: string; effort: string }>;

function firstConfiguredHarness(config: AgentConfig | undefined) {
	return Object.keys(config?.modelByHarness ?? {}).sort()[0] ?? "";
}

function toHarnessModelForm(modelByHarness: AgentConfig["modelByHarness"] | undefined) {
	const out: HarnessModelForm = {};
	for (const [harness, value] of Object.entries(modelByHarness ?? {})) {
		out[harness] = { model: value?.model ?? "", effort: value?.effort ?? "" };
	}
	return out;
}

function toRoleHarnessModelForm(config: AgentConfig | undefined, harness: string) {
	const out = toHarnessModelForm(config?.modelByHarness);
	if (harness) {
		const current = out[harness] ?? { model: "", effort: "" };
		out[harness] = {
			model: current.model || config?.model || "",
			effort: current.effort || config?.effort || "",
		};
	}
	return out;
}

function projectSelection(harness: string, model: string, effort: string, form: HarnessModelForm): ModelSelection {
	if (!harness) return { harness: "", model, effort };
	return roleSelection(harness, form);
}

function roleSelection(harness: string, form: HarnessModelForm): ModelSelection {
	const pair = form[harness];
	return { harness, model: pair?.model ?? "", effort: pair?.effort ?? "" };
}

function patchHarnessSelection(form: HarnessModelForm, selection: ModelSelection) {
	if (!selection.harness) return form;
	return {
		...form,
		[selection.harness]: { model: selection.model, effort: selection.effort },
	};
}

function modelPins(form: HarnessModelForm): ConfiguredModelPin[] {
	return Object.entries(form).map(([harness, pair]) => ({ harness, ...pair }));
}

function buildHarnessModelConfig(form: HarnessModelForm) {
	const entries = Object.entries(form)
		.map(([harness, value]) => {
			const model = value.model.trim();
			// An entry is kept if it pins a model OR carries an effort: the
			// daemon accepts effort-only overrides, so an empty model field
			// must not delete a persisted effort the form cannot display.
			const out: { model?: string; effort?: string } = {};
			if (model) out.model = model;
			const effort = value.effort.trim();
			if (effort) out.effort = effort;
			return [harness, out] as const;
		})
		.filter(([, value]) => Object.keys(value).length > 0);
	return entries.length > 0 ? Object.fromEntries(entries) : undefined;
}

function buildAgentConfig(
	current: ProjectConfig["agentConfig"],
	model: string,
	effort: string,
	permissions: string,
	modelByHarnessForm: HarnessModelForm,
) {
	const next = {
		...current,
		model: model.trim() || undefined,
		effort: effort.trim() || undefined,
		permissions: permissions || undefined,
	};
	const modelByHarness = buildHarnessModelConfig(modelByHarnessForm);
	if (modelByHarness) {
		return blankToUndefined({ ...next, modelByHarness });
	}
	const withoutHarnessMap = { ...next } as components["schemas"]["AgentConfig"];
	delete withoutHarnessMap.modelByHarness;
	return blankToUndefined(withoutHarnessMap);
}

function buildRoleAgentConfig(
	current: AgentConfig | undefined,
	initialHarness: string,
	selectedHarness: string,
	form: HarnessModelForm,
) {
	const next: AgentConfig = { ...(current ?? {}) };
	// A legacy scalar role pair belongs to the harness that was configured when
	// the form loaded. The form seeds it into that harness's map. Once a concrete
	// role harness is involved, remove the scalar so it cannot leak when the role
	// later switches providers.
	if (initialHarness || selectedHarness) {
		delete next.model;
		delete next.effort;
	}
	const modelByHarness = buildHarnessModelConfig(form);
	if (modelByHarness) next.modelByHarness = modelByHarness;
	else delete next.modelByHarness;
	return blankToUndefined(next);
}

function buildReviewerConfig(
	current: ProjectConfig["reviewers"],
	harness: string,
	initialHarness: string,
	form: HarnessModelForm,
) {
	const rest = current?.slice(1) ?? [];
	if (!harness) return rest.length > 0 ? rest : undefined;
	const first = current?.[0];
	return [
		{
			...(first ?? {}),
			harness,
			agentConfig: buildRoleAgentConfig(first?.agentConfig, initialHarness, harness, form),
		},
		...rest,
	];
}

function reviewerModelAvailability(
	availability: AgentModelAvailabilityResponse | undefined,
	currentHarness: string,
): AgentModelAvailabilityResponse | undefined {
	if (!availability) return undefined;
	return {
		...availability,
		harnesses: (availability.harnesses ?? []).filter(
			(harness) => harness.reviewerCapable || harness.id === currentHarness,
		),
	};
}

function modelHarnessCatalog(
	catalog: { authorized?: AgentInfo[]; installed?: AgentInfo[]; supported?: AgentInfo[] } | undefined,
	availability: AgentModelAvailabilityResponse | undefined,
	currentHarness: string,
	reviewerOnly: boolean,
	includeDefault: boolean,
	defaultLabel: string,
) {
	const availabilityByID = new Map((availability?.harnesses ?? []).map((harness) => [harness.id, harness]));
	const reviewerIDs = new Set(
		(availability?.harnesses ?? []).filter((harness) => harness.reviewerCapable).map((harness) => harness.id),
	);
	for (const agents of [catalog?.supported, catalog?.installed, catalog?.authorized]) {
		for (const agent of agents ?? []) {
			if (agent.reviewerCapable) reviewerIDs.add(agent.id);
		}
	}
	const allowed = (id: string) => !reviewerOnly || reviewerIDs.has(id) || id === currentHarness;
	const supportedByID = new Map<string, AgentInfo>();
	for (const agent of catalog?.supported ?? []) {
		if (allowed(agent.id)) supportedByID.set(agent.id, agent);
	}
	for (const harness of availability?.harnesses ?? []) {
		if (allowed(harness.id) && !supportedByID.has(harness.id)) {
			supportedByID.set(harness.id, {
				id: harness.id,
				label: harness.label,
				reviewerCapable: harness.reviewerCapable,
			});
		}
	}
	if (currentHarness && !supportedByID.has(currentHarness)) {
		supportedByID.set(currentHarness, {
			id: currentHarness,
			label: availabilityByID.get(currentHarness)?.label ?? currentHarness,
			reviewerCapable: availabilityByID.get(currentHarness)?.reviewerCapable ?? false,
		});
	}
	const defaultAgent: AgentInfo = {
		id: DEFAULT_HARNESS_ID,
		label: defaultLabel,
		authStatus: "authorized",
		reviewerCapable: false,
	};
	if (includeDefault) supportedByID.set(defaultAgent.id, defaultAgent);
	const filter = (agents: AgentInfo[] | undefined) => {
		if (!agents) return undefined;
		const out = agents.filter((agent) => allowed(agent.id));
		if (includeDefault) out.push(defaultAgent);
		return out;
	};
	return {
		supported: [...supportedByID.values()],
		installed: filter(catalog?.installed),
		authorized: filter(catalog?.authorized),
	};
}
