import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	ProjectAgentsSettingsView,
	ProjectGeneralSettingsView,
	ProjectSettingsFormView,
	ProjectSettingsSection,
	ProjectWorkflowSettingsView,
	validateProjectSettings,
} from "@aoagents/product-ui";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useEffect, useMemo, useState } from "react";
import { Pencil, RefreshCw } from "lucide-react";
import type { components } from "../../api/schema";
import {
	agentModelsQueryKey,
	agentModelsQueryOptions,
	refreshAgentModels,
	revalidateAgentModels,
	type AgentModelCatalog,
} from "../hooks/useAgentModelsQuery";
import { agentsQueryKey, agentsQueryOptions, refreshAgents } from "../hooks/useAgentsQuery";
import {
	type AgentModelAvailabilityResponse,
	useModelAvailabilityQuery,
	useRefreshModelAvailability,
} from "../hooks/useModelAvailabilityQuery";
import { useWorkspaceQuery, workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { modelAvailabilityFromAgentInventory } from "../lib/agent-selection";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { captureRendererEvent } from "../lib/telemetry";
import { spawnOrchestrator } from "../lib/spawn-orchestrator";
import { cn } from "../lib/utils";
import { newestActiveOrchestrator } from "../types/workspace";
import { RequiredAgentField } from "./CreateProjectAgentSheet";
import { buildIntake, deriveIntakeRepo, IntakeFields, type IntakeForm } from "./IntakeFields";
import {
	buildEffortOptions,
	buildModelCatalogView,
	harnessEfforts,
	shouldUseManualEffort,
} from "./ModelAvailabilityField";
import { ProductExternalLink } from "./ProductExternalLink";
import { ReviewerSelect, reviewerTrustWarning } from "./ReviewerSelect";
import { AgentModelCombobox } from "./settings/AgentModelCombobox";
import { SettingsOptionMenu } from "./settings/SettingsOptionMenu";
import { SettingsRow } from "./settings/SettingsRow";
import {
	buildWorkerMix,
	parseMaxLiveWorkers,
	toWorkerMixForm,
	type WorkerMixBucket,
	WorkerMixFields,
	workerMixInvalid,
	workerMixRowError,
	workerMixTotal,
} from "./WorkerMixFields";

type Project = components["schemas"]["Project"];
type ProjectConfig = components["schemas"]["ProjectConfig"];
type TrackerIntakeConfig = components["schemas"]["TrackerIntakeConfig"];
type AgentConfig = components["schemas"]["AgentConfig"];
type RoleOverride = components["schemas"]["RoleOverride"];
type ReviewerConfig = components["schemas"]["DomainReviewerConfig"];
type WorkerMixEntry = components["schemas"]["WorkerMixEntry"];

const PERMISSION_MODE_VALUES = ["default", "accept-edits", "auto", "bypass-permissions"] as const;

const projectQueryKey = (id: string) => ["project", id] as const;

export type ProjectSettingsSection = "general" | "agents" | "workflow" | "instructions" | "workers" | "intake";
export interface ProjectSettingsSaveState {
	isPending: boolean;
	showSaving: boolean;
	validationError: string | null;
	mutationError: string | null;
	saved: boolean;
	replacementError: string | null;
}

export function ProjectSettingsForm({
	projectId,
	section = "general",
	onSaveState,
}: {
	projectId: string;
	section?: ProjectSettingsSection;
	onSaveState?: (state: ProjectSettingsSaveState) => void;
}) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();

	const query = useQuery({
		queryKey: projectQueryKey(projectId),
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/projects/{id}", {
				params: { path: { id: projectId } },
			});
			if (error) throw new Error(apiErrorMessage(error));
			if (data?.status !== "ok") throw new Error(t("settings.project.degraded"));
			return data.project as Project;
		},
	});

	return (
		<>
			{query.isLoading ? (
				<p className="text-sm text-settings-muted">{t("settings.project.loading")}</p>
			) : query.isError || !query.data ? (
				<p className="text-sm text-error">
					{query.error instanceof Error ? query.error.message : t("settings.project.loadFailed")}
				</p>
			) : (
				<SettingsBody
					key={projectId}
					project={query.data}
					onSaved={() => queryClient.invalidateQueries({ queryKey: workspaceQueryKey })}
					projectId={projectId}
					section={section}
					onSaveState={onSaveState}
				/>
			)}
		</>
	);
}

function SettingsBody({
	project,
	projectId,
	onSaved,
	section = "general",
	onSaveState,
}: {
	project: Project;
	projectId: string;
	onSaved: () => void;
	section?: ProjectSettingsSection;
	onSaveState?: (state: ProjectSettingsSaveState) => void;
}) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const workspaceQuery = useWorkspaceQuery();
	const config = project.config ?? {};
	const isScratchProject = project.kind === "scratch";
	const workspace = workspaceQuery.data?.find((item) => item.id === projectId);
	const activeOrchestrator = newestActiveOrchestrator(workspace?.sessions ?? []);
	const intake: TrackerIntakeConfig = config.trackerIntake ?? {};
	const firstReviewer = config.reviewers?.[0];
	const workerModelSelection = roleModelSelection(
		config.worker?.agentConfig,
		config.agentConfig,
		config.worker?.agent ?? "",
	);
	const orchestratorModelSelection = roleModelSelection(
		config.orchestrator?.agentConfig,
		config.agentConfig,
		config.orchestrator?.agent ?? "",
	);
	const [initialWorkerMix] = useState(() => toWorkerMixForm(config.workerMix));
	const [form, setForm] = useState(() => ({
		displayName: project.name,
		defaultBranch: config.defaultBranch ?? project.defaultBranch ?? "",
		sessionPrefix: config.sessionPrefix ?? "",
		workerAgent: config.worker?.agent ?? "",
		orchestratorAgent: config.orchestrator?.agent ?? "",
		workerModel: workerModelSelection.model,
		workerEffort: workerModelSelection.effort,
		orchestratorModel: orchestratorModelSelection.model,
		orchestratorEffort: orchestratorModelSelection.effort,
		workerMode: config.worker?.agentConfig?.mode || config.agentConfig?.mode || "",
		orchestratorMode: config.orchestrator?.agentConfig?.mode || config.agentConfig?.mode || "",
		permissions: config.agentConfig?.permissions ?? "",
		reviewerHarness: firstReviewer?.harness ?? "",
		workerTaskPrompt: config.workerTaskPrompt ?? "",
		agentRules: config.agentRules ?? "",
		agentRulesFile: config.agentRulesFile ?? "",
		orchestratorRules: config.orchestratorRules ?? "",
		orchestratorRulesFile: config.orchestratorRulesFile ?? "",
		reviewerRules: config.reviewerRules ?? "",
		reviewerRulesFile: config.reviewerRulesFile ?? "",
		workerMix: initialWorkerMix,
		maxLiveWorkers: config.maxLiveWorkers ? String(config.maxLiveWorkers) : "",
		intakeEnabled: intake.enabled ?? false,
		intakeProvider: intake.provider ?? "",
		intakeRepo: intake.repo ?? "",
		intakeAssignee: intake.assignee ?? "",
		intakeOptOutLabel: intake.optOutLabel ?? "",
	}));
	const [savedAt, setSavedAt] = useState<number | null>(null);
	const [showSaving, setShowSaving] = useState(false);
	const [replacementError, setReplacementError] = useState<string | null>(null);
	const [validationError, setValidationError] = useState<string | null>(null);
	const initialOrchestratorAgent = config.orchestrator?.agent ?? "";
	const missingRequiredAgent = form.orchestratorAgent === "";
	const agentsQuery = useQuery(agentsQueryOptions);
	const agentCatalog = agentsQuery.data;
	const modelAvailabilityQuery = useModelAvailabilityQuery();
	const { refresh: refreshModels, isRefreshing: isRefreshingModels } = useRefreshModelAvailability();
	const effectiveModelAvailability = modelAvailabilityQuery.data ?? modelAvailabilityFromAgentInventory(agentCatalog);
	const refreshAgentsMutation = useMutation({
		mutationFn: refreshAgents,
		onSuccess: (next) => queryClient.setQueryData(agentsQueryKey, next),
	});

	const intakeForm: IntakeForm = {
		enabled: form.intakeEnabled,
		provider: form.intakeProvider,
		repo: form.intakeRepo,
		assignee: form.intakeAssignee,
		optOutLabel: form.intakeOptOutLabel,
	};
	const patchIntake = (patch: Partial<IntakeForm>) =>
		setForm((f) => ({
			...f,
			intakeEnabled: patch.enabled ?? f.intakeEnabled,
			intakeProvider: patch.provider ?? f.intakeProvider,
			intakeRepo: patch.repo ?? f.intakeRepo,
			intakeAssignee: patch.assignee ?? f.intakeAssignee,
			intakeOptOutLabel: patch.optOutLabel ?? f.intakeOptOutLabel,
		}));
	const derivedIntakeRepo = deriveIntakeRepo(project.repo, form.intakeProvider);
	const effectiveIntakeRepo = form.intakeRepo.trim() || derivedIntakeRepo?.path;
	const reviewerWarning = reviewerTrustWarning(form.reviewerHarness);
	const resetWorkerAgent = (workerAgent: string) =>
		setForm((f) => ({
			...f,
			workerAgent,
			workerModel: roleModelValue(config.worker?.agentConfig, config.agentConfig, workerAgent, "model"),
			workerEffort: roleModelValue(config.worker?.agentConfig, config.agentConfig, workerAgent, "effort"),
			workerMode: config.worker?.agentConfig?.mode || config.agentConfig?.mode || "",
		}));
	const resetOrchestratorAgent = (orchestratorAgent: string) =>
		setForm((f) => ({
			...f,
			orchestratorAgent,
			orchestratorModel: roleModelValue(
				config.orchestrator?.agentConfig,
				config.agentConfig,
				orchestratorAgent,
				"model",
			),
			orchestratorEffort: roleModelValue(
				config.orchestrator?.agentConfig,
				config.agentConfig,
				orchestratorAgent,
				"effort",
			),
			orchestratorMode: config.orchestrator?.agentConfig?.mode || config.agentConfig?.mode || "",
		}));

	const mutation = useMutation({
		mutationFn: async () => {
			void captureRendererEvent("ao.renderer.settings_save_requested", { project_id: projectId });
			const displayName = form.displayName.trim();
			const sharedAgentConfig = buildSharedAgentConfig(config.agentConfig, [
				{
					agentId: form.workerAgent,
					model: form.workerModel,
					effort: form.workerEffort,
					mode: form.workerMode,
					roleConfig: config.worker?.agentConfig,
				},
				{
					agentId: form.orchestratorAgent,
					model: form.orchestratorModel,
					effort: form.orchestratorEffort,
					mode: form.orchestratorMode,
					roleConfig: config.orchestrator?.agentConfig,
				},
			]);
			const reviewers =
				!isScratchProject && form.reviewerHarness
					? config.reviewers?.length
						? config.reviewers.map((reviewer, index) =>
								index === 0 ? { ...reviewer, harness: form.reviewerHarness } : reviewer,
							)
						: [{ harness: form.reviewerHarness }]
					: undefined;
			const preservedConsumers = preserveSharedAgentConsumers(config, sharedAgentConfig.removals, reviewers);
			const workerMix = preserveWorkerMixSharedConsumers(
				buildWorkerMix(form.workerMix),
				form.workerMix,
				initialWorkerMix,
				config.agentConfig,
				sharedAgentConfig.removals,
			);
			const next: ProjectConfig = isScratchProject
				? {
						...scratchSupportedConfig(config),
						worker: {
							...config.worker,
							agent: form.workerAgent,
							agentConfig: buildRoleAgentConfig(
								config.worker?.agentConfig,
								config.agentConfig,
								config.worker?.agent ?? "",
								form.workerAgent,
								form.workerModel,
								form.workerMode,
								form.workerEffort,
								sharedAgentConfig.removals,
							),
						},
						orchestrator: {
							...config.orchestrator,
							agent: form.orchestratorAgent,
							agentConfig: buildRoleAgentConfig(
								config.orchestrator?.agentConfig,
								config.agentConfig,
								config.orchestrator?.agent ?? "",
								form.orchestratorAgent,
								form.orchestratorModel,
								form.orchestratorMode,
								form.orchestratorEffort,
								sharedAgentConfig.removals,
							),
						},
						agentConfig: blankToUndefined({
							...sharedAgentConfig.config,
							permissions: form.permissions || undefined,
						}),
						prime: preservedConsumers.prime,
						workerTaskPrompt: form.workerTaskPrompt || undefined,
						agentRules: form.agentRules.trim() || undefined,
						agentRulesFile: form.agentRulesFile.trim() || undefined,
						orchestratorRules: form.orchestratorRules.trim() || undefined,
						orchestratorRulesFile: form.orchestratorRulesFile.trim() || undefined,
						reviewerRules: form.reviewerRules.trim() || undefined,
						reviewerRulesFile: form.reviewerRulesFile.trim() || undefined,
						workerMix,
						maxLiveWorkers: parseMaxLiveWorkers(form.maxLiveWorkers),
					}
				: {
						...config,
						defaultBranch: form.defaultBranch || undefined,
						sessionPrefix: form.sessionPrefix || undefined,
						worker: {
							...config.worker,
							agent: form.workerAgent,
							agentConfig: buildRoleAgentConfig(
								config.worker?.agentConfig,
								config.agentConfig,
								config.worker?.agent ?? "",
								form.workerAgent,
								form.workerModel,
								form.workerMode,
								form.workerEffort,
								sharedAgentConfig.removals,
							),
						},
						orchestrator: {
							...config.orchestrator,
							agent: form.orchestratorAgent,
							agentConfig: buildRoleAgentConfig(
								config.orchestrator?.agentConfig,
								config.agentConfig,
								config.orchestrator?.agent ?? "",
								form.orchestratorAgent,
								form.orchestratorModel,
								form.orchestratorMode,
								form.orchestratorEffort,
								sharedAgentConfig.removals,
							),
						},
						agentConfig: blankToUndefined({
							...sharedAgentConfig.config,
							permissions: form.permissions || undefined,
						}),
						prime: preservedConsumers.prime,
						workerTaskPrompt: form.workerTaskPrompt || undefined,
						agentRules: form.agentRules.trim() || undefined,
						agentRulesFile: form.agentRulesFile.trim() || undefined,
						orchestratorRules: form.orchestratorRules.trim() || undefined,
						orchestratorRulesFile: form.orchestratorRulesFile.trim() || undefined,
						reviewerRules: form.reviewerRules.trim() || undefined,
						reviewerRulesFile: form.reviewerRulesFile.trim() || undefined,
						workerMix,
						maxLiveWorkers: parseMaxLiveWorkers(form.maxLiveWorkers),
						reviewers: preservedConsumers.reviewers,
						trackerIntake: buildIntake(intakeForm),
					};
			const { error } = await apiClient.PUT("/api/v1/projects/{id}", {
				params: { path: { id: projectId } },
				body: { displayName, config: next },
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
						replacementError: error instanceof Error ? error.message : t("settings.project.replaceOrchestratorFailed"),
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
			onSaved();
		},
		onError: () => {
			void captureRendererEvent("ao.renderer.settings_save_failed", { project_id: projectId });
		},
	});

	useEffect(() => {
		if (!mutation.isPending) {
			setShowSaving(false);
			return;
		}
		const timeout = window.setTimeout(() => setShowSaving(true), 200);
		return () => window.clearTimeout(timeout);
	}, [mutation.isPending]);

	useEffect(() => {
		onSaveState?.({
			isPending: mutation.isPending,
			showSaving,
			validationError,
			mutationError: mutation.isError
				? mutation.error instanceof Error
					? mutation.error.message
					: t("settings.project.saveFailed")
				: null,
			saved: savedAt !== null && !mutation.isPending && !mutation.isError,
			replacementError: replacementError && !mutation.isPending && !mutation.isError ? replacementError : null,
		});
	}, [
		mutation.error,
		mutation.isError,
		mutation.isPending,
		onSaveState,
		replacementError,
		savedAt,
		showSaving,
		t,
		validationError,
	]);

	useEffect(() => {
		if (savedAt === null) return;
		const timeout = window.setTimeout(() => setSavedAt(null), 1800);
		return () => window.clearTimeout(timeout);
	}, [savedAt]);

	return (
		<ProjectSettingsFormView
			id="project-settings-form"
			onSubmit={() => {
				setSavedAt(null);
				setReplacementError(null);
				const validation = validateProjectSettings(form, { validateIntake: !isScratchProject });
				if (validation) {
					setValidationError(
						validation === "agents_required"
							? t("settings.project.agentsRequired")
							: validation === "name_required"
								? t("settings.project.nameRequired")
								: t("settings.project.intakeAssigneeRequired"),
					);
					return;
				}
				const rowError = workerMixRowError(form.workerMix);
				if (rowError) {
					setValidationError(rowError);
					return;
				}
				if (workerMixInvalid(form.workerMix)) {
					setValidationError(t("settings.project.workerMixInvalid", { total: workerMixTotal(form.workerMix) }));
					return;
				}
				setValidationError(null);
				mutation.mutate();
			}}
		>
			{section === "general" && (
				<>
					<ProjectGeneralSettingsView
						displayName={form.displayName}
						externalLink={ProductExternalLink}
						icons={{
							edit: <Pencil className="settings-inline-edit-icon" aria-hidden="true" />,
						}}
						onDisplayNameChange={(displayName) => setForm((f) => ({ ...f, displayName }))}
						labels={{
							title: t("settings.project.identity"),
							name: t("settings.project.name"),
							id: t("settings.project.id"),
							kind: t("settings.project.kind"),
							path: t("settings.project.path"),
							repo: t("settings.project.repo"),
							workspaceRepos: t("settings.project.workspaceRepos"),
							workspaceReposEmpty: t("settings.project.childReposEmpty"),
							editName: t("settings.field.edit", { label: t("settings.project.name") }),
						}}
						project={{
							id: project.id,
							kindLabel: projectKindLabel(project.kind, t),
							path: project.path,
							pathHref: `file://${encodeURI(project.path)}`,
							repo: project.repo,
							repoHref: project.repo ? repositoryHref(project.repo) : undefined,
							workspaceRepos: project.kind === "workspace" ? (project.workspaceRepos ?? []) : undefined,
						}}
					/>
				</>
			)}

			{section === "agents" && (
				<>
					<ProjectAgentsSettingsView
						title={t("settings.project.agents")}
						workerArea={
							<RequiredAgentField
								id="workerAgent"
								variant="settings-row"
								value={form.workerAgent}
								placeholder={t("settings.project.selectWorker")}
								label={t("settings.project.defaultWorker")}
								emptyOptionLabel={t("settings.project.defaultWorkerEvenSplit")}
								authorized={agentCatalog?.authorized}
								installed={agentCatalog?.installed}
								supported={agentCatalog?.supported}
								disabled={agentsQuery.isFetching && agentCatalog === undefined}
								onChange={resetWorkerAgent}
							/>
						}
						workerModelArea={
							<AgentModelField
								role="worker"
								agentId={form.workerAgent}
								projectId={projectId}
								model={form.workerModel}
								effort={form.workerEffort}
								mode={form.workerMode}
								availability={effectiveModelAvailability}
								onModelChange={(workerModel) => setForm((f) => ({ ...f, workerModel }))}
								onEffortChange={(workerEffort) => setForm((f) => ({ ...f, workerEffort }))}
								onModeChange={(workerMode) => setForm((f) => ({ ...f, workerMode }))}
							/>
						}
						orchestratorArea={
							<RequiredAgentField
								id="orchestratorAgent"
								variant="settings-row"
								value={form.orchestratorAgent}
								placeholder={t("settings.project.selectOrchestrator")}
								label={t("settings.project.defaultOrchestrator")}
								authorized={agentCatalog?.authorized}
								installed={agentCatalog?.installed}
								supported={agentCatalog?.supported}
								disabled={agentsQuery.isFetching && agentCatalog === undefined}
								invalid={validationError !== null && form.orchestratorAgent === ""}
								onChange={resetOrchestratorAgent}
							/>
						}
						orchestratorModelArea={
							<AgentModelField
								role="orchestrator"
								agentId={form.orchestratorAgent}
								projectId={projectId}
								model={form.orchestratorModel}
								effort={form.orchestratorEffort}
								mode={form.orchestratorMode}
								availability={effectiveModelAvailability}
								onModelChange={(orchestratorModel) => setForm((f) => ({ ...f, orchestratorModel }))}
								onEffortChange={(orchestratorEffort) => setForm((f) => ({ ...f, orchestratorEffort }))}
								onModeChange={(orchestratorMode) => setForm((f) => ({ ...f, orchestratorMode }))}
							/>
						}
						permissions={{
							control: (
								<PermissionModeSelect
									value={form.permissions}
									onChange={(v) => setForm((f) => ({ ...f, permissions: v }))}
								/>
							),
							label: t("settings.project.permissionMode"),
						}}
						refresh={{
							actionIcon: (
								<RefreshCw
									className={cn("size-icon-base", refreshAgentsMutation.isPending && "animate-spin")}
									aria-hidden="true"
								/>
							),
							disabled: refreshAgentsMutation.isPending,
							label: t("settings.project.refreshAgents"),
							onClick: () => refreshAgentsMutation.mutate(),
							value: refreshAgentsMutation.isPending ? t("settings.project.refreshing") : t("settings.project.refresh"),
						}}
						error={
							refreshAgentsMutation.isError
								? refreshAgentsMutation.error instanceof Error
									? refreshAgentsMutation.error.message
									: t("settings.project.refreshFailed")
								: null
						}
						missingRequiredMessage={missingRequiredAgent ? t("settings.project.agentsRequired") : null}
					/>
				</>
			)}

			{section === "workflow" && (
				<>
					{!isScratchProject ? (
						<>
							<ProjectWorkflowSettingsView
								branch={form.defaultBranch}
								icons={{
									edit: <Pencil className="settings-inline-edit-icon" aria-hidden="true" />,
								}}
								prefix={form.sessionPrefix}
								onBranchChange={(defaultBranch) => setForm((f) => ({ ...f, defaultBranch }))}
								onPrefixChange={(sessionPrefix) => setForm((f) => ({ ...f, sessionPrefix }))}
								labels={{
									worktrees: t("settings.project.worktrees"),
									defaultBranch: t("settings.project.defaultBranch"),
									sessionPrefix: t("settings.project.sessionPrefix"),
									reviewers: t("settings.project.reviewers"),
									defaultReviewer: t("settings.project.defaultReviewer"),
									editDefaultBranch: t("settings.field.edit", {
										label: t("settings.project.defaultBranch"),
									}),
									editSessionPrefix: t("settings.field.edit", {
										label: t("settings.project.sessionPrefix"),
									}),
								}}
								reviewerControl={
									<ReviewerSelect
										value={form.reviewerHarness}
										onChange={(v) => setForm((f) => ({ ...f, reviewerHarness: v }))}
										ariaLabel={t("settings.project.defaultReviewer")}
										authorized={agentCatalog?.authorized}
										defaultOptionLabel={t("settings.project.default")}
										defaultTriggerLabel={t("settings.project.default")}
										installed={agentCatalog?.installed}
										supported={agentCatalog?.supported}
										disabled={agentsQuery.isFetching && agentCatalog === undefined}
									/>
								}
								reviewerWarning={reviewerWarning}
							/>
						</>
					) : (
						<p className="px-1 text-xs text-settings-muted">{t("settings.project.workflow")}</p>
					)}
				</>
			)}

			{section === "instructions" && (
				<>
					<ProjectSettingsSection title={t("settings.project.instructions")} grouped>
						<SettingsTextareaRow
							label={t("settings.project.workerTaskPrompt")}
							id="workerTaskPrompt"
							value={form.workerTaskPrompt}
							onChange={(workerTaskPrompt) => setForm((f) => ({ ...f, workerTaskPrompt }))}
							placeholder={t("settings.project.workerTaskPromptPlaceholder")}
						/>
						<RulesFields
							label={t("settings.project.workerInstructions")}
							fileLabel={t("settings.project.workerInstructionsFile")}
							idPrefix="agentRules"
							rules={form.agentRules}
							file={form.agentRulesFile}
							onRules={(agentRules) => setForm((f) => ({ ...f, agentRules }))}
							onFile={(agentRulesFile) => setForm((f) => ({ ...f, agentRulesFile }))}
						/>
						<RulesFields
							label={t("settings.project.orchestratorInstructions")}
							fileLabel={t("settings.project.orchestratorInstructionsFile")}
							idPrefix="orchestratorRules"
							rules={form.orchestratorRules}
							file={form.orchestratorRulesFile}
							onRules={(orchestratorRules) => setForm((f) => ({ ...f, orchestratorRules }))}
							onFile={(orchestratorRulesFile) => setForm((f) => ({ ...f, orchestratorRulesFile }))}
						/>
						<RulesFields
							label={t("settings.project.reviewerInstructions")}
							fileLabel={t("settings.project.reviewerInstructionsFile")}
							idPrefix="reviewerRules"
							rules={form.reviewerRules}
							file={form.reviewerRulesFile}
							onRules={(reviewerRules) => setForm((f) => ({ ...f, reviewerRules }))}
							onFile={(reviewerRulesFile) => setForm((f) => ({ ...f, reviewerRulesFile }))}
						/>
					</ProjectSettingsSection>
					<ProjectSettingsSection title={t("settings.project.promptInspector")} grouped>
						<RolePromptInspector projectId={projectId} />
					</ProjectSettingsSection>
				</>
			)}

			{section === "workers" && (
				<ProjectSettingsSection title={t("settings.project.workers")} grouped>
					<WorkerMixFields
						buckets={form.workerMix}
						onChange={(workerMix) => setForm((f) => ({ ...f, workerMix }))}
						agentCatalog={agentCatalog}
						availability={effectiveModelAvailability}
						isRefreshing={isRefreshingModels || modelAvailabilityQuery.isFetching}
						onRefresh={refreshModels}
					/>
					<SettingsInputRow
						label={t("settings.project.maxLiveWorkers")}
						id="maxLiveWorkers"
						type="number"
						value={form.maxLiveWorkers}
						onChange={(maxLiveWorkers) => setForm((f) => ({ ...f, maxLiveWorkers }))}
						placeholder="0"
					/>
					{modelAvailabilityQuery.isError && (
						<p className="px-1 text-xs leading-row text-warning">{t("settings.models.loadFailed")}</p>
					)}
				</ProjectSettingsSection>
			)}

			{section === "intake" && (
				<>
					{!isScratchProject ? (
						<ProjectSettingsSection title={t("settings.project.trackerIntake")} grouped>
							<IntakeFields
								variant="settings"
								form={intakeForm}
								onChange={patchIntake}
								repoPreview={{
									value: effectiveIntakeRepo,
									href: effectiveIntakeRepo === derivedIntakeRepo?.path ? derivedIntakeRepo?.url : undefined,
								}}
							/>
						</ProjectSettingsSection>
					) : (
						<p className="px-1 text-xs text-settings-muted">{t("settings.project.trackerIntake")}</p>
					)}
				</>
			)}
		</ProjectSettingsFormView>
	);
}

function AgentModelField({
	role,
	agentId,
	projectId,
	model,
	effort,
	mode,
	availability,
	onModelChange,
	onEffortChange,
	onModeChange,
}: {
	role: "worker" | "orchestrator";
	agentId: string;
	projectId: string;
	model: string;
	effort: string;
	mode: string;
	availability?: AgentModelAvailabilityResponse;
	onModelChange: (value: string) => void;
	onEffortChange: (value: string) => void;
	onModeChange: (value: string) => void;
}) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [customAgentId, setCustomAgentId] = useState<string | null>(null);
	const query = useQuery(agentModelsQueryOptions(agentId, projectId));
	const catalog: AgentModelCatalog | undefined = query.data;
	const revalidationQuery = useQuery({
		queryKey: ["agent-model-revalidation", agentId, projectId, catalog?.validatedAt ?? ""],
		queryFn: () => revalidateAgentModels(agentId, projectId),
		enabled: agentId !== "" && catalog?.refreshRecommended === true,
		staleTime: Number.POSITIVE_INFINITY,
		retry: false,
	});
	useEffect(() => {
		if (revalidationQuery.data) {
			queryClient.setQueryData(agentModelsQueryKey(agentId, projectId), revalidationQuery.data);
		}
	}, [agentId, projectId, queryClient, revalidationQuery.data]);
	const refreshMutation = useMutation({
		mutationFn: () => refreshAgentModels(agentId, projectId),
		onSuccess: (catalog) => queryClient.setQueryData(agentModelsQueryKey(agentId, projectId), catalog),
	});
	const isMode = catalog?.selectionMode === "mode";
	const label = t(`settings.models.${role}${isMode ? "Mode" : "Model"}`);
	const datalistID = `${role}-model-options`;
	const warning =
		(refreshMutation.isError
			? refreshMutation.error instanceof Error
				? refreshMutation.error.message
				: t("settings.models.refreshFailed")
			: undefined) ??
		(revalidationQuery.isError
			? revalidationQuery.error instanceof Error
				? revalidationQuery.error.message
				: t("settings.models.validateFailed")
			: undefined) ??
		catalog?.warning ??
		(query.isError
			? query.error instanceof Error
				? query.error.message
				: t("settings.models.loadFailed")
			: undefined);

	if (isMode) {
		const options = [
			{ value: "__default__", label: t("settings.models.agentDefault") },
			...(catalog.models ?? []).map((item) => ({ value: item.id, label: item.label })),
		];
		return (
			<>
				<SettingsRow label={label}>
					<div className="flex min-w-0 items-center gap-2">
						<ModelRefreshButton
							label={label}
							pending={refreshMutation.isPending}
							disabled={agentId === ""}
							onClick={() => refreshMutation.mutate()}
						/>
						<SettingsOptionMenu
							aria-label={label}
							value={mode || "__default__"}
							options={options}
							triggerClassName="justify-end"
							onChange={(value) => {
								onModeChange(value === "__default__" ? "" : value);
								onModelChange("");
							}}
						/>
					</div>
				</SettingsRow>
				<AgentEffortField
					role={role}
					agentId={agentId}
					model={model}
					effort={effort}
					availability={availability}
					onChange={onEffortChange}
				/>
				{warning && <p className="px-1 text-xs leading-row text-warning">{warning}</p>}
			</>
		);
	}

	const hasCatalog = catalog?.selectionMode === "catalog" && (catalog.models?.length ?? 0) > 0;
	const modelIsInCatalog = catalog?.models?.some((item) => item.id === model) ?? false;
	const showCustomInput = hasCatalog && (customAgentId === agentId || (model !== "" && !modelIsInCatalog));
	const selectCatalogModel = (value: string) => {
		setCustomAgentId(null);
		onModelChange(value);
		onModeChange("");
	};
	const selectCustomModel = (value: string) => {
		setCustomAgentId(agentId);
		onModelChange(value);
		onModeChange("");
	};
	return (
		<>
			<SettingsRow label={label}>
				<div className="flex min-w-0 items-center gap-2">
					<ModelRefreshButton
						label={label}
						pending={refreshMutation.isPending}
						disabled={agentId === ""}
						onClick={() => refreshMutation.mutate()}
					/>
					{hasCatalog && !showCustomInput ? (
						<AgentModelCombobox
							aria-label={label}
							value={model}
							models={catalog.models}
							allowCustom={catalog.allowCustom}
							onChange={selectCatalogModel}
							onCustom={selectCustomModel}
							triggerClassName="justify-end"
						/>
					) : (
						<>
							<input
								id={datalistID}
								aria-label={label}
								className="settings-inline-input settings-model-control"
								value={model}
								disabled={agentId === ""}
								onChange={(event) => {
									onModelChange(event.target.value);
									onModeChange("");
								}}
								placeholder={query.isFetching ? t("settings.models.loading") : t("settings.project.agentDefault")}
							/>
							{hasCatalog && (
								<AgentModelCombobox
									aria-label={t("settings.models.optionsAria", { label })}
									value={model}
									models={catalog.models}
									allowCustom={catalog.allowCustom}
									onChange={selectCatalogModel}
									onCustom={selectCustomModel}
									triggerLabel={t("settings.models.browse")}
									triggerClassName="shrink-0"
								/>
							)}
						</>
					)}
				</div>
			</SettingsRow>
			<AgentEffortField
				role={role}
				agentId={agentId}
				model={model}
				effort={effort}
				availability={availability}
				onChange={onEffortChange}
			/>
			{warning && <p className="px-1 text-xs leading-row text-warning">{warning}</p>}
		</>
	);
}

function AgentEffortField({
	role,
	agentId,
	model,
	effort,
	availability,
	onChange,
}: {
	role: "worker" | "orchestrator";
	agentId: string;
	model: string;
	effort: string;
	availability?: AgentModelAvailabilityResponse;
	onChange: (value: string) => void;
}) {
	const { t } = useTranslation();
	const catalogValue = useMemo(() => ({ harness: agentId, model, effort: "" }), [agentId, model]);
	const harnesses = useMemo(
		() => buildModelCatalogView(availability, catalogValue, [catalogValue]),
		[availability, catalogValue],
	);
	const harness = harnesses.find((option) => option.id === agentId);
	const modelOption = harness?.models.find((option) => option.model === model);
	const catalogEfforts = buildEffortOptions(
		modelOption?.synthetic
			? [...(modelOption.efforts ?? []), ...harnessEfforts(harness)]
			: modelOption
				? (modelOption.efforts ?? [])
				: harnessEfforts(harness),
	);
	const trimmedEffort = effort.trim();
	const effortOptions =
		trimmedEffort && !catalogEfforts.includes(trimmedEffort) ? [...catalogEfforts, trimmedEffort] : catalogEfforts;
	const manualEffort = (model.trim() !== "" && shouldUseManualEffort(modelOption)) || effortOptions.length === 0;
	const label = t(`settings.models.${role}Effort`);
	const id = `${role}-effort`;
	return (
		<SettingsRow label={label}>
			{manualEffort ? (
				<>
					<input
						id={id}
						aria-label={label}
						className="settings-inline-input settings-model-control"
						value={effort}
						list={`${id}-options`}
						placeholder={t("settings.models.agentDefault")}
						disabled={agentId === ""}
						onChange={(event) => onChange(event.target.value)}
					/>
					<datalist id={`${id}-options`}>
						{effortOptions.map((option) => (
							<option key={option} value={option} />
						))}
					</datalist>
				</>
			) : (
				<select
					id={id}
					aria-label={label}
					className="settings-inline-input settings-model-control"
					value={effort}
					disabled={agentId === ""}
					onChange={(event) => onChange(event.target.value)}
				>
					<option value="">{t("settings.models.agentDefault")}</option>
					{effortOptions.map((option) => (
						<option key={option} value={option}>
							{option}
						</option>
					))}
				</select>
			)}
		</SettingsRow>
	);
}

function ModelRefreshButton({
	label,
	pending,
	disabled,
	onClick,
}: {
	label: string;
	pending: boolean;
	disabled: boolean;
	onClick: () => void;
}) {
	const { t } = useTranslation();
	return (
		<button
			type="button"
			aria-label={t("settings.models.refreshAria", { label: label.toLocaleLowerCase() })}
			title={t("settings.models.refreshAria", { label: label.toLocaleLowerCase() })}
			className="settings-option-trigger shrink-0 disabled:pointer-events-none disabled:opacity-50"
			disabled={disabled || pending}
			onClick={onClick}
		>
			<RefreshCw className={cn("size-icon-sm", pending && "animate-spin")} aria-hidden="true" />
		</button>
	);
}

function PermissionModeSelect({ value, onChange }: { value: string; onChange: (value: string) => void }) {
	const { t } = useTranslation();
	const options = [
		{ value: "__default__", label: t("settings.project.default") },
		...PERMISSION_MODE_VALUES.map((value) => ({
			value,
			label:
				value === "default"
					? t("settings.project.permissionDefault")
					: value === "accept-edits"
						? t("settings.project.permissionAcceptEdits")
						: value === "auto"
							? t("settings.project.permissionAuto")
							: t("settings.project.permissionBypass"),
		})),
	];

	return (
		<SettingsOptionMenu
			aria-label={t("settings.project.permissionMode")}
			value={value || "__default__"}
			options={options}
			onChange={(v) => onChange(v === "__default__" ? "" : v)}
		/>
	);
}

function SettingsInputRow({
	label,
	id,
	type = "text",
	value,
	onChange,
	placeholder,
}: {
	label: string;
	id: string;
	type?: string;
	value: string;
	onChange: (value: string) => void;
	placeholder?: string;
}) {
	return (
		<SettingsRow label={label}>
			<input
				id={id}
				aria-label={label}
				type={type}
				min={type === "number" ? 0 : undefined}
				className="settings-inline-input"
				value={value}
				onChange={(event) => onChange(event.target.value)}
				placeholder={placeholder}
			/>
		</SettingsRow>
	);
}

function SettingsTextareaRow({
	label,
	id,
	value,
	onChange,
	placeholder,
}: {
	label: string;
	id: string;
	value: string;
	onChange: (value: string) => void;
	placeholder?: string;
}) {
	return (
		<SettingsRow label={label}>
			<textarea
				id={id}
				aria-label={label}
				className="settings-inline-input min-h-20 resize-y py-2 font-mono"
				value={value}
				onChange={(event) => onChange(event.target.value)}
				placeholder={placeholder}
			/>
		</SettingsRow>
	);
}

function RulesFields({
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
	const { t } = useTranslation();
	return (
		<div className="flex flex-col gap-2">
			<SettingsTextareaRow label={label} id={idPrefix} value={rules} onChange={onRules} />
			<SettingsInputRow
				label={fileLabel}
				id={`${idPrefix}File`}
				value={file}
				onChange={onFile}
				placeholder={t("settings.project.instructionsFilePlaceholder")}
			/>
		</div>
	);
}

const ROLE_PROMPT_OPTIONS = ["worker", "orchestrator", "reviewer"] as const;
type RolePromptRole = (typeof ROLE_PROMPT_OPTIONS)[number];
const rolePromptQueryKey = (id: string, role: string) => ["project", id, "role-prompt", role] as const;

function RolePromptInspector({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const [role, setRole] = useState<RolePromptRole>("worker");
	const query = useQuery({
		queryKey: rolePromptQueryKey(projectId, role),
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/projects/{id}/roles/{role}/prompt", {
				params: { path: { id: projectId, role } },
			});
			if (error) throw new Error(apiErrorMessage(error, t("settings.project.promptInspectorFailed")));
			return data;
		},
	});
	return (
		<div className="flex flex-col gap-2">
			<SettingsRow label={t("settings.project.role")}>
				<SettingsOptionMenu
					aria-label={t("settings.project.role")}
					value={role}
					options={ROLE_PROMPT_OPTIONS.map((value) => ({ value, label: t(`settings.project.roles.${value}`) }))}
					onChange={(value) => setRole(value as RolePromptRole)}
				/>
			</SettingsRow>
			{query.isPending ? (
				<p className="px-1 text-xs text-settings-muted">{t("settings.project.promptInspectorLoading")}</p>
			) : query.isError ? (
				<p className="px-1 text-xs text-error">
					{query.error instanceof Error ? query.error.message : t("settings.project.promptInspectorFailed")}
				</p>
			) : (
				<>
					{query.data?.taskPromptTemplate && (
						<pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md bg-terminal px-3 py-3 font-mono text-xs leading-row text-terminal-foreground">
							{query.data.taskPromptTemplate}
						</pre>
					)}
					<pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-md bg-terminal px-3 py-3 font-mono text-xs leading-row text-terminal-foreground">
						{query.data?.prompt ?? ""}
					</pre>
				</>
			)}
		</div>
	);
}

function projectKindLabel(kind: string, t: TFunction): string {
	switch (kind) {
		case "single_repo":
			return t("settings.project.kind.singleRepo");
		case "workspace":
			return t("settings.project.kind.workspace");
		case "scratch":
			return t("settings.project.kind.scratch");
		default:
			return kind || t("settings.project.kind.unknown");
	}
}

function repositoryHref(repository: string): string {
	if (/^https?:\/\//i.test(repository)) return repository;
	if (repository.startsWith("git@")) {
		const [host, path] = repository.slice(4).split(":", 2);
		return `https://${host}/${path.replace(/\.git$/, "")}`;
	}
	if (repository.startsWith("ssh://")) {
		try {
			const parsed = new URL(repository);
			return `https://${parsed.hostname}${parsed.pathname.replace(/\.git$/, "")}`;
		} catch {
			return repository;
		}
	}
	return repository;
}

function scratchSupportedConfig(config: ProjectConfig): ProjectConfig {
	const { defaultBranch: _defaultBranch, reviewers: _reviewers, trackerIntake: _trackerIntake, ...supported } = config;
	return supported;
}

function blankToUndefined<T extends object>(obj: T): T | undefined {
	return Object.values(obj).some((v) => v !== undefined) ? obj : undefined;
}

function roleModelSelection(
	roleConfig: components["schemas"]["AgentConfig"] | undefined,
	sharedConfig: components["schemas"]["AgentConfig"] | undefined,
	harness: string,
): { model: string; effort: string } {
	return {
		model: roleModelValue(roleConfig, sharedConfig, harness, "model"),
		effort: roleModelValue(roleConfig, sharedConfig, harness, "effort"),
	};
}

function roleModelValue(
	roleConfig: AgentConfig | undefined,
	sharedConfig: AgentConfig | undefined,
	harness: string,
	field: "model" | "effort",
): string {
	let value = "";
	const assign = (next: string | undefined) => {
		if (!next) return;
		if (field === "model" && !modelCompatibleWithHarness(next, harness)) return;
		value = next;
	};
	assign(sharedConfig?.[field]);
	assign(roleConfig?.[field]);
	if (harness) {
		assign(sharedConfig?.modelByHarness?.[harness]?.[field]);
		assign(roleConfig?.modelByHarness?.[harness]?.[field]);
	}
	return value;
}

type ModelProvider = "" | "anthropic" | "openai" | "fugu";

function modelCompatibleWithHarness(model: string, harness: string): boolean {
	const modelProvider = classifyModelProvider(model);
	const harnessProvider = harnessModelProvider(harness);
	return modelProvider === "" || harnessProvider === "" || modelProvider === harnessProvider;
}

function harnessModelProvider(harness: string): ModelProvider {
	switch (harness) {
		case "claude-code":
			return "anthropic";
		case "codex":
			return "openai";
		case "codex-fugu":
			return "fugu";
		default:
			return "";
	}
}

function classifyModelProvider(model: string): ModelProvider {
	const normalized = model.toLowerCase().trim();
	if (!normalized) return "";
	if (hasModelFamily(normalized, "fugu")) return "fugu";
	if (
		hasModelFamily(normalized, "claude") ||
		hasModelFamily(normalized, "opus") ||
		hasModelFamily(normalized, "sonnet") ||
		hasModelFamily(normalized, "haiku") ||
		hasModelFamily(normalized, "fable")
	) {
		return "anthropic";
	}
	if (
		hasModelFamily(normalized, "gpt") ||
		hasModelFamily(normalized, "codex") ||
		normalized.startsWith("o1") ||
		normalized.startsWith("o3") ||
		normalized.startsWith("o4")
	) {
		return "openai";
	}
	return "";
}

function hasModelFamily(model: string, fragment: string): boolean {
	const index = model.indexOf(fragment);
	if (index === -1) return false;
	for (let start = index; start >= 0; start = model.indexOf(fragment, start + 1)) {
		const before = Array.from(model.slice(0, start)).at(-1) ?? "";
		const after = Array.from(model.slice(start + fragment.length)).at(0) ?? "";
		if (!/\p{L}/u.test(before) && !/\p{L}/u.test(after)) return true;
	}
	return false;
}

type AgentConfigField = "model" | "effort" | "mode";
type AgentConfigFieldSource = "none" | "shared-scalar" | "role-scalar" | "shared-harness" | "role-harness";
type RoleAgentSelection = {
	agentId: string;
	model: string;
	effort: string;
	mode: string;
	roleConfig?: AgentConfig;
};
type SharedAgentRemoval = { field: AgentConfigField; value: string; harness?: string };

function roleFieldSource(
	roleConfig: AgentConfig | undefined,
	sharedConfig: AgentConfig | undefined,
	harness: string,
	field: AgentConfigField,
): AgentConfigFieldSource {
	const fieldApplies = (value: string | undefined) =>
		Boolean(value) && (field !== "model" || modelCompatibleWithHarness(value ?? "", harness));
	let source: AgentConfigFieldSource = fieldApplies(sharedConfig?.[field]) ? "shared-scalar" : "none";
	if (fieldApplies(roleConfig?.[field])) source = "role-scalar";
	if (harness && field !== "mode") {
		if (fieldApplies(sharedConfig?.modelByHarness?.[harness]?.[field])) source = "shared-harness";
		if (fieldApplies(roleConfig?.modelByHarness?.[harness]?.[field])) source = "role-harness";
	}
	return source;
}

function buildRoleAgentConfig(
	existing: AgentConfig | undefined,
	shared: AgentConfig | undefined,
	originalAgentId: string,
	agentId: string,
	model: string,
	mode: string,
	effort: string,
	sharedRemovals: SharedAgentRemoval[],
): AgentConfig | undefined {
	const next = { ...existing };
	const trimmedModel = model.trim();
	const trimmedEffort = effort.trim();
	const trimmedMode = mode.trim();
	const modelByHarness = { ...(next.modelByHarness ?? {}) };
	const existingEntry = agentId ? existing?.modelByHarness?.[agentId] : undefined;
	const sharedEntry = agentId ? shared?.modelByHarness?.[agentId] : undefined;
	const scopeToHarness = Boolean(
		existingEntry?.model || existingEntry?.effort || sharedEntry?.model || sharedEntry?.effort,
	);
	const entry = agentId ? { ...(modelByHarness[agentId] ?? {}) } : {};
	const shouldMaterializeShared = (field: AgentConfigField, source: AgentConfigFieldSource, value: string) =>
		source.startsWith("shared-") &&
		sharedRemovals.some((removal) => {
			if (removal.field !== field || removal.value !== value) return false;
			if (removal.harness) return source === "shared-harness" && removal.harness === agentId;
			return source === "shared-scalar";
		});
	const writeScalarField = (field: AgentConfigField, value: string) => {
		if (value) next[field] = value;
		else delete next[field];
	};
	const writeHarnessField = (field: "model" | "effort", value: string) => {
		if (value) entry[field] = value;
		else delete entry[field];
	};
	const applyField = (field: "model" | "effort", value: string) => {
		const source = roleFieldSource(existing, shared, agentId, field);
		const effectiveValue = roleModelValue(existing, shared, agentId, field).trim();
		if (
			source.startsWith("shared-") &&
			value === effectiveValue &&
			!shouldMaterializeShared(field, source, effectiveValue)
		) {
			if (field === "model") {
				const agentChanged = originalAgentId !== agentId;
				if (agentChanged && next.model && !modelCompatibleWithHarness(next.model, agentId)) delete next.model;
				if (agentChanged && entry.model && !modelCompatibleWithHarness(entry.model, agentId)) delete entry.model;
			}
			return;
		}
		if (!value) {
			if (field === "model") {
				if (source === "role-harness") delete entry.model;
				else if (source === "role-scalar") delete next.model;
				return;
			}
			delete next[field];
			delete entry[field];
			return;
		}
		if (
			agentId &&
			(source === "role-harness" || source === "shared-harness" || (scopeToHarness && source !== "role-scalar"))
		) {
			writeHarnessField(field, value);
		} else {
			writeScalarField(field, value);
		}
	};
	const modeSource = roleFieldSource(existing, shared, agentId, "mode");
	const effectiveMode = (existing?.mode || shared?.mode || "").trim();
	if (!(
		modeSource === "shared-scalar" &&
		trimmedMode === effectiveMode &&
		!shouldMaterializeShared("mode", modeSource, effectiveMode)
	)) {
		writeScalarField("mode", trimmedMode);
	}
	applyField("model", trimmedModel);
	applyField("effort", trimmedEffort);
	if (agentId) {
		if (Object.keys(entry).length > 0) modelByHarness[agentId] = entry;
		else delete modelByHarness[agentId];
	}
	if (Object.keys(modelByHarness).length > 0) next.modelByHarness = modelByHarness;
	else delete next.modelByHarness;
	return Object.keys(next).length > 0 ? next : undefined;
}

function buildSharedAgentConfig(
	shared: AgentConfig | undefined,
	roleSelections: RoleAgentSelection[],
): { config: AgentConfig | undefined; removals: SharedAgentRemoval[] } {
	const next: AgentConfig = { ...(shared ?? {}) };
	const removals: SharedAgentRemoval[] = [];
	for (const field of ["model", "effort", "mode"] as const) {
		const value = shared?.[field];
		if (
			value &&
			roleSelections.some((selection) => {
				const selected = field === "model" ? selection.model : field === "effort" ? selection.effort : selection.mode;
				return (
					!selected.trim() &&
					roleFieldSource(selection.roleConfig, shared, selection.agentId, field) === "shared-scalar"
				);
			})
		) {
			delete next[field];
			removals.push({ field, value });
		}
	}
	const modelByHarness = { ...(next.modelByHarness ?? {}) };
	for (const selection of roleSelections) {
		if (!selection.agentId) continue;
		const entry = modelByHarness[selection.agentId];
		if (!entry) continue;
		const nextEntry = { ...entry };
		const removeSharedScalarFallback = (field: "model" | "effort") => {
			const scalarValue = shared?.[field];
			if (!scalarValue || next[field] === undefined) return;
			if (field === "model" && !modelCompatibleWithHarness(scalarValue, selection.agentId)) return;
			delete next[field];
			removals.push({ field, value: scalarValue });
		};
		if (
			!selection.model.trim() &&
			entry.model &&
			roleFieldSource(selection.roleConfig, shared, selection.agentId, "model") === "shared-harness"
		) {
			delete nextEntry.model;
			removals.push({ field: "model", harness: selection.agentId, value: entry.model });
			removeSharedScalarFallback("model");
		}
		if (
			!selection.effort.trim() &&
			entry.effort &&
			roleFieldSource(selection.roleConfig, shared, selection.agentId, "effort") === "shared-harness"
		) {
			delete nextEntry.effort;
			removals.push({ field: "effort", harness: selection.agentId, value: entry.effort });
			removeSharedScalarFallback("effort");
		}
		if (Object.keys(nextEntry).length > 0) modelByHarness[selection.agentId] = nextEntry;
		else delete modelByHarness[selection.agentId];
	}
	if (Object.keys(modelByHarness).length > 0) next.modelByHarness = modelByHarness;
	else delete next.modelByHarness;
	return { config: blankToUndefined(next), removals };
}

function setAgentConfigField(config: AgentConfig | undefined, removal: SharedAgentRemoval): AgentConfig {
	const next = { ...(config ?? {}) };
	if (removal.harness) {
		if (removal.field === "mode") return next;
		const modelByHarness = { ...(next.modelByHarness ?? {}) };
		modelByHarness[removal.harness] = {
			...(modelByHarness[removal.harness] ?? {}),
			[removal.field]: removal.value,
		};
		next.modelByHarness = modelByHarness;
	} else {
		next[removal.field] = removal.value;
	}
	return next;
}

function consumerUsesRemovedSharedField(
	roleConfig: AgentConfig | undefined,
	shared: AgentConfig | undefined,
	harness: string | undefined,
	removal: SharedAgentRemoval,
): boolean {
	if (removal.harness) {
		if (!harness || harness !== removal.harness) return false;
	} else if (harness === undefined) {
		harness = "";
	}
	const source = roleFieldSource(roleConfig, shared, harness, removal.field);
	return removal.harness ? source === "shared-harness" : source === "shared-scalar";
}

function preserveSharedAgentConsumers(
	config: ProjectConfig,
	removals: SharedAgentRemoval[],
	reviewers: ReviewerConfig[] | undefined,
): {
	prime: RoleOverride | undefined;
	reviewers: ReviewerConfig[] | undefined;
} {
	if (removals.length === 0) return { prime: config.prime, reviewers };
	let prime = config.prime;
	let nextReviewers = reviewers;
	for (const removal of removals) {
		if (consumerUsesRemovedSharedField(prime?.agentConfig, config.agentConfig, prime?.agent, removal)) {
			prime = { ...(prime ?? {}), agentConfig: setAgentConfigField(prime?.agentConfig, removal) };
		}
		nextReviewers = nextReviewers?.map((reviewer) =>
			consumerUsesRemovedSharedField(reviewer.agentConfig, config.agentConfig, reviewer.harness, removal)
				? { ...reviewer, agentConfig: setAgentConfigField(reviewer.agentConfig, removal) }
				: reviewer,
		);
	}
	return { prime, reviewers: nextReviewers };
}

function preserveWorkerMixSharedConsumers(
	workerMix: WorkerMixEntry[] | undefined,
	formWorkerMix: WorkerMixBucket[],
	initialWorkerMix: WorkerMixBucket[],
	shared: AgentConfig | undefined,
	removals: SharedAgentRemoval[],
): WorkerMixEntry[] | undefined {
	if (!workerMix || removals.length === 0) return workerMix;
	return workerMix.map((bucket, index) => {
		let next = bucket;
		for (const removal of removals) {
			if (removal.field !== "model" && removal.field !== "effort") continue;
			if (next[removal.field]) continue;
			if (workerMixFieldWasCleared(formWorkerMix, initialWorkerMix, index, bucket, removal.field)) continue;
			if (!consumerUsesRemovedSharedField(undefined, shared, next.agent, removal)) continue;
			next = { ...next, [removal.field]: removal.value };
		}
		return next;
	});
}

function workerMixFieldWasCleared(
	formWorkerMix: WorkerMixBucket[],
	initialWorkerMix: WorkerMixBucket[],
	index: number,
	bucket: WorkerMixEntry,
	field: "model" | "effort",
): boolean {
	const formBucket = formWorkerMix[index];
	const initialBucket = initialWorkerMix.find((candidate) => candidate.id === formBucket?.id);
	return Boolean(initialBucket?.[field] && initialBucket[field] !== bucket[field]);
}
