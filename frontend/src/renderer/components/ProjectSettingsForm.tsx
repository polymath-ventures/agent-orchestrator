import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { components } from "../../api/schema";
import { agentsQueryKey, agentsQueryOptions, refreshAgents } from "../hooks/useAgentsQuery";
import { useWorkspaceQuery, workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { captureRendererEvent } from "../lib/telemetry";
import { spawnOrchestrator } from "../lib/spawn-orchestrator";
import { newestActiveOrchestrator } from "../types/workspace";
import { RequiredAgentField } from "./CreateProjectAgentSheet";
import { DashboardSubhead } from "./DashboardSubhead";
import { buildIntake, deriveGitHubRepo, IntakeFields, type IntakeForm, intakeNeedsRule } from "./IntakeFields";
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
	workerMixTotal,
} from "./WorkerMixFields";

type Project = components["schemas"]["Project"];
type ProjectConfig = components["schemas"]["ProjectConfig"];
type TrackerIntakeConfig = components["schemas"]["TrackerIntakeConfig"];
type ModelAvailability = components["schemas"]["DomainModelAvailability"];
type ModelByHarness = NonNullable<components["schemas"]["AgentConfig"]["modelByHarness"]>;
type ReviewerConfig = components["schemas"]["DomainReviewerConfig"];

const PERMISSION_MODE_OPTIONS = [
	{ value: "default", label: "Default" },
	{ value: "accept-edits", label: "Accept edits" },
	{ value: "auto", label: "Auto" },
	{ value: "bypass-permissions", label: "Bypass permissions" },
] as const;

const REVIEWER_OPTIONS = ["claude-code", "codex", "opencode"] as const;
const MODEL_HARNESS_OPTIONS = ["claude-code", "codex", "opencode"] as const;

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
	const [form, setForm] = useState({
		defaultBranch: config.defaultBranch ?? project.defaultBranch ?? "",
		sessionPrefix: config.sessionPrefix ?? "",
		workerAgent: config.worker?.agent ?? "",
		orchestratorAgent: config.orchestrator?.agent ?? "",
		model: config.agentConfig?.model ?? "",
		modelByHarness: toHarnessModelForm(config.agentConfig?.modelByHarness),
		permissions: config.agentConfig?.permissions ?? "",
		reviewerHarness: firstReviewer?.harness ?? "",
		reviewerModel: firstReviewer?.agentConfig?.model ?? "",
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
	const initialOrchestratorAgent = config.orchestrator?.agent ?? "";
	const missingRequiredAgent = form.workerAgent === "" || form.orchestratorAgent === "";
	const agentsQuery = useQuery(agentsQueryOptions);
	const agentCatalog = agentsQuery.data;
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
				worker: { ...config.worker, agent: form.workerAgent },
				orchestrator: { ...config.orchestrator, agent: form.orchestratorAgent },
				agentConfig: buildAgentConfig(config.agentConfig, form.model, form.permissions, form.modelByHarness),
				reviewers: buildReviewerConfig(firstReviewer, form.reviewerHarness, form.reviewerModel),
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
			onSaved();
		},
		onError: () => {
			void captureRendererEvent("ao.renderer.settings_save_failed", { project_id: projectId });
		},
	});

	return (
		<form
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
					<CardTitle className="text-control">Agents</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<RequiredAgentField
						id="workerAgent"
						value={form.workerAgent}
						placeholder="Select worker agent"
						label="Default worker agent"
						authorized={agentCatalog?.authorized}
						installed={agentCatalog?.installed}
						supported={agentCatalog?.supported}
						disabled={agentsQuery.isFetching && agentCatalog === undefined}
						invalid={validationError !== null && form.workerAgent === ""}
						onChange={(v) => setForm((f) => ({ ...f, workerAgent: v }))}
					/>
					<RequiredAgentField
						id="orchestratorAgent"
						value={form.orchestratorAgent}
						placeholder="Select orchestrator agent"
						label="Default orchestrator agent"
						authorized={agentCatalog?.authorized}
						installed={agentCatalog?.installed}
						supported={agentCatalog?.supported}
						disabled={agentsQuery.isFetching && agentCatalog === undefined}
						invalid={validationError !== null && form.orchestratorAgent === ""}
						onChange={(v) => setForm((f) => ({ ...f, orchestratorAgent: v }))}
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
					<Field label="Model override" htmlFor="model">
						<input
							id="model"
							className="h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
							value={form.model}
							onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}
							placeholder="(agent default)"
						/>
					</Field>
					<div className="grid gap-3 sm:grid-cols-3">
						{MODEL_HARNESS_OPTIONS.map((harness) => (
							<Field key={harness} label={`${harness} model`} htmlFor={`model-${harness}`}>
								<input
									id={`model-${harness}`}
									className="h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
									value={form.modelByHarness[harness]?.model ?? ""}
									onChange={(e) =>
										setForm((f) => ({
											...f,
											modelByHarness: patchHarnessModel(f.modelByHarness, harness, e.target.value),
										}))
									}
									placeholder="(default)"
								/>
							</Field>
						))}
					</div>
					<ModelAvailabilityRows rows={project.modelAvailability ?? []} />
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
					<CardTitle className="text-control">Reviewers</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<Field label="Default reviewer agent" htmlFor="reviewerHarness">
						<ReviewerSelect
							id="reviewerHarness"
							value={form.reviewerHarness}
							onChange={(v) => setForm((f) => ({ ...f, reviewerHarness: v }))}
						/>
					</Field>
					<Field label="Reviewer model" htmlFor="reviewerModel">
						<input
							id="reviewerModel"
							className="h-control-form w-full rounded-md border border-input bg-transparent px-2.5 text-control text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak disabled:opacity-50"
							value={form.reviewerModel}
							disabled={!form.reviewerHarness}
							onChange={(e) => setForm((f) => ({ ...f, reviewerModel: e.target.value }))}
							placeholder="(reviewer default)"
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
						content-preserving (only surrounding whitespace is normalized). A rules file is a repo-relative path; a
						configured-but-missing, empty, or oversized file fails the spawn loudly rather than silently dropping the
						instructions.
					</p>
					<RulesField
						label="Worker rules"
						fileLabel="Worker rules file (repo-relative)"
						idPrefix="agentRules"
						rules={form.agentRules}
						file={form.agentRulesFile}
						onRules={(v) => setForm((f) => ({ ...f, agentRules: v }))}
						onFile={(v) => setForm((f) => ({ ...f, agentRulesFile: v }))}
					/>
					<RulesField
						label="Orchestrator rules"
						fileLabel="Orchestrator rules file (repo-relative)"
						idPrefix="orchestratorRules"
						rules={form.orchestratorRules}
						file={form.orchestratorRulesFile}
						onRules={(v) => setForm((f) => ({ ...f, orchestratorRules: v }))}
						onFile={(v) => setForm((f) => ({ ...f, orchestratorRulesFile: v }))}
					/>
					<RulesField
						label="Reviewer rules"
						fileLabel="Reviewer rules file (repo-relative)"
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

function ReviewerSelect({ id, value, onChange }: { id: string; value: string; onChange: (value: string) => void }) {
	return (
		<Select value={value || "__default__"} onValueChange={(v) => onChange(v === "__default__" ? "" : v)}>
			<SelectTrigger id={id} className="h-control-form w-full text-control">
				<SelectValue />
			</SelectTrigger>
			<SelectContent>
				<SelectItem value="__default__">Project default</SelectItem>
				{REVIEWER_OPTIONS.map((reviewer) => (
					<SelectItem key={reviewer} value={reviewer}>
						{reviewer}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
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

function ModelAvailabilityRows({ rows }: { rows: ModelAvailability[] }) {
	if (rows.length === 0) {
		return null;
	}
	return (
		<div className="grid gap-2">
			{rows.map((row) => (
				<div
					key={`${row.harness}:${row.model}`}
					className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-md border border-border px-3 py-2 text-xs"
				>
					<span className="min-w-0 truncate font-mono text-foreground">
						{row.harness}/{row.model}
					</span>
					<span className={`shrink-0 font-medium ${availabilityClass(row.status)}`}>{availabilityLabel(row)}</span>
				</div>
			))}
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

function availabilityLabel(row: ModelAvailability) {
	if (row.reason === "not-probed") return "not probed";
	if (row.reason === "no-capability") return "no probe";
	if (row.reason === "probe-unavailable") return "probe unavailable";
	if (row.reason === "recovered") return "recovered";
	return row.status;
}

function availabilityClass(status: ModelAvailability["status"]) {
	if (status === "reachable") return "text-success";
	if (status === "unreachable") return "text-error";
	return "text-muted-foreground";
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

// HarnessModelForm mirrors the per-harness config entries. The form only
// exposes a model input, but effort must round-trip untouched: rebuilding
// entries as { model } alone would silently wipe a persisted effort on save.
type HarnessModelForm = Record<string, { model: string; effort?: string }>;

function toHarnessModelForm(modelByHarness: ModelByHarness | undefined) {
	const out: HarnessModelForm = {};
	for (const [harness, value] of Object.entries(modelByHarness ?? {})) {
		out[harness] = { model: value?.model ?? "", effort: value?.effort };
	}
	return out;
}

function patchHarnessModel(form: HarnessModelForm, harness: string, model: string) {
	return {
		...form,
		[harness]: { ...(form[harness] ?? {}), model },
	};
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
			if (value.effort) out.effort = value.effort;
			return [harness, out] as const;
		})
		.filter(([, value]) => Object.keys(value).length > 0);
	return entries.length > 0 ? Object.fromEntries(entries) : undefined;
}

function buildAgentConfig(
	current: ProjectConfig["agentConfig"],
	model: string,
	permissions: string,
	modelByHarnessForm: HarnessModelForm,
) {
	const next = {
		...current,
		model: model.trim() || undefined,
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

function buildReviewerConfig(current: ReviewerConfig | undefined, harness: string, model: string) {
	if (!harness) return undefined;
	const reviewer = { ...current, harness };
	const agentConfig = blankToUndefined({
		...current?.agentConfig,
		model: model.trim() || undefined,
	});
	if (agentConfig) {
		return [{ ...reviewer, agentConfig }];
	}
	return [reviewer];
}
