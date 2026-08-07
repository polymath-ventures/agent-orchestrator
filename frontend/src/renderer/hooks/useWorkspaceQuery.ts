import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, hasTrustedApiBaseUrl } from "../lib/api-client";
import { mockWorkspaces } from "../lib/mock-data";
import { usesPreviewWorkspaceData } from "../lib/preview-mode";
import { captureRendererEvent } from "../lib/telemetry";
import {
	FLEET_WORKSPACE_ID,
	type PauseState,
	type ProjectKind,
	type PRState,
	type PullRequestFacts,
	type SessionKind,
	toAgentProvider,
	toProjectKind,
	toSessionActivity,
	toSessionStatus,
	type WorkspaceSession,
	type WorkspaceSummary,
} from "../types/workspace";

function toPauseState(state?: string): PauseState | undefined {
	return state === "running" || state === "draining" || state === "paused" ? state : undefined;
}

function toSessionKind(kind?: string): SessionKind | undefined {
	return kind === "orchestrator" || kind === "worker" || kind === "prime" ? kind : undefined;
}

function toPullRequestFacts(pr: components["schemas"]["SessionPRFacts"]): PullRequestFacts {
	return {
		url: pr.url,
		number: pr.number,
		state: pr.state as PRState,
		ci: pr.ci,
		review: pr.review,
		mergeability: pr.mergeability,
		reviewComments: pr.reviewComments,
		updatedAt: pr.updatedAt,
	};
}

export const workspaceQueryKey = ["workspaces"] as const;
const reportedUnknownSessionFields = new Set<string>();

function reportUnknownSessionField(field: "status" | "activity", value?: string): void {
	const reason = value ? "unrecognized" : "missing";
	const key = `${field}:${reason}`;
	if (reportedUnknownSessionFields.has(key)) return;
	reportedUnknownSessionFields.add(key);
	void captureRendererEvent("ao.renderer.session_state_unknown", { field, reason });
}

// e2e seam (dev:web only): the Playwright fake-agent harness injects
// `window.__aoFakeAgent` (see e2e/support/fake-bridge.ts) to drive a
// deterministic, mutable session timeline off the SSE refetch path. Compiled
// out of the packaged build — the packaged renderer never sets VITE_NO_ELECTRON
// and always hits the real daemon.
type FakeAgentSeam = { snapshot: () => WorkspaceSummary[] };

async function fetchWorkspaces(): Promise<WorkspaceSummary[]> {
	if (usesPreviewWorkspaceData) {
		const fake =
			typeof window !== "undefined"
				? (window as unknown as { __aoFakeAgent?: FakeAgentSeam }).__aoFakeAgent
				: undefined;
		if (fake) return fake.snapshot();
	}
	if (!hasTrustedApiBaseUrl()) {
		if (usesPreviewWorkspaceData) return mockWorkspaces;
		throw new Error("AO daemon API is not ready");
	}

	const [{ data: projectsData, error: projectsError }, { data: sessionsData, error: sessionsError }] =
		await Promise.all([apiClient.GET("/api/v1/projects"), apiClient.GET("/api/v1/sessions")]);

	if (projectsError || sessionsError) throw projectsError ?? sessionsError;

	const sessions = sessionsData?.sessions ?? [];
	const workspaces: WorkspaceSummary[] = (projectsData?.projects ?? []).map((project) => {
		const kind = toProjectKind(project.kind) ?? ("single_repo" satisfies ProjectKind);
		return {
			id: project.id,
			name: project.name,
			kind,
			path: project.path,
			orchestratorAgent: project.orchestratorAgent ? toAgentProvider(project.orchestratorAgent) : undefined,
			paused: project.paused,
			pauseState: toPauseState(project.pauseState),
			drainingWorkers: project.drainingWorkers,
			sessions: sessions
				.filter((session) => session.projectId === project.id)
				.map((session) => toWorkspaceSession(session, project.id, project.name)),
		};
	});
	const fleetSessions = sessions
		.filter((session) => !session.projectId && toSessionKind(session.kind) === "prime")
		.map((session) => toWorkspaceSession(session, FLEET_WORKSPACE_ID, "AO Fleet"));
	if (fleetSessions.length > 0) {
		workspaces.push({ id: FLEET_WORKSPACE_ID, name: "AO Fleet", path: "", sessions: fleetSessions });
	}
	return workspaces;
}

function toWorkspaceSession(
	session: components["schemas"]["ControllersSessionView"],
	workspaceId: string,
	workspaceName: string,
): WorkspaceSession {
	const status = toSessionStatus(session.status, session.isTerminated);
	const scmStatus = session.scmStatus ? toSessionStatus(session.scmStatus) : undefined;
	const activity = toSessionActivity(session.activity);
	if (status === "unknown") reportUnknownSessionField("status", session.status);
	if (!activity || activity.state === "unknown") reportUnknownSessionField("activity", session.activity?.state);
	const reviewerHarness = ["claude-code", "codex", "codex-fugu", "opencode"].includes(session.reviewerHarness ?? "")
		? (session.reviewerHarness as WorkspaceSession["reviewerHarness"])
		: undefined;
	return {
		id: session.id,
		terminalHandleId: session.terminalHandleId,
		workspaceId,
		workspaceName,
		title: session.displayName ?? session.issueId ?? session.id,
		issueId: session.issueId,
		provider: toAgentProvider(session.harness),
		reviewerHarness,
		kind: toSessionKind(session.kind),
		mode: session.mode === "chat" ? "chat" : "tui",
		branch: session.branch || undefined,
		status,
		scmStatus,
		isTerminated: session.isTerminated,
		terminateOnPrMerge: session.terminateOnPrMerge ?? false,
		createdAt: session.createdAt,
		updatedAt: session.updatedAt,
		activity,
		previewUrl: session.previewUrl,
		previewRevision: session.previewRevision,
		isPinned: session.isPinned ?? false,
		pinnedAt: session.pinnedAt ?? undefined,
		prs: (session.prs ?? []).map(toPullRequestFacts),
	};
}

// Shared so route loaders can prefetch via queryClient.ensureQueryData (paired
// with the router's defaultPreload: "intent") and the hook reads the same cache.
export const workspaceQueryOptions = {
	queryKey: workspaceQueryKey,
	queryFn: fetchWorkspaces,
	retry: 1,
	refetchInterval: 15_000,
};

export function useWorkspaceQuery() {
	return useQuery(workspaceQueryOptions);
}
