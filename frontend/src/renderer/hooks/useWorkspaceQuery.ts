import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, hasTrustedApiBaseUrl } from "../lib/api-client";
import {
	FLEET_WORKSPACE_ID,
	type PauseState,
	type PRState,
	type ProjectKind,
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

// e2e seam (dev:web only): the Playwright fake-agent harness injects
// `window.__aoFakeAgent` (see e2e/support/fake-bridge.ts) to drive a
// deterministic, mutable session timeline off the SSE refetch path. Compiled
// out of the packaged build — the packaged renderer never sets VITE_NO_ELECTRON
// and always hits the real daemon.
type FakeAgentSeam = { snapshot: () => WorkspaceSummary[] };

async function fetchWorkspaces(): Promise<WorkspaceSummary[]> {
	if (import.meta.env.VITE_NO_ELECTRON === "1") {
		const fake =
			typeof window !== "undefined"
				? (window as unknown as { __aoFakeAgent?: FakeAgentSeam }).__aoFakeAgent
				: undefined;
		if (fake) return fake.snapshot();
	}
	if (!hasTrustedApiBaseUrl()) {
		return [];
	}

	const [{ data: projectsData, error: projectsError }, { data: sessionsData, error: sessionsError }] =
		await Promise.all([apiClient.GET("/api/v1/projects"), apiClient.GET("/api/v1/sessions")]);

	if (projectsError || sessionsError) throw projectsError ?? sessionsError;

	const sessions = sessionsData?.sessions ?? [];
	const workspaces: WorkspaceSummary[] = (projectsData?.projects ?? []).map((project) => ({
		id: project.id,
		name: project.name,
		kind: toProjectKind(project.kind) ?? ("single_repo" satisfies ProjectKind),
		path: project.path,
		orchestratorAgent: project.orchestratorAgent ? toAgentProvider(project.orchestratorAgent) : undefined,
		paused: project.paused,
		pauseState: toPauseState(project.pauseState),
		drainingWorkers: project.drainingWorkers,
		sessions: sessions
			.filter((session) => session.projectId === project.id)
			.map((session) => toWorkspaceSession(session, project.id, project.name)),
	}));
	const projectlessPrimeSessions = sessions
		.filter((session) => !session.projectId && toSessionKind(session.kind) === "prime")
		.map((session) => toWorkspaceSession(session, FLEET_WORKSPACE_ID, "AO Fleet"));
	if (projectlessPrimeSessions.length > 0) {
		workspaces.push({
			id: FLEET_WORKSPACE_ID,
			name: "AO Fleet",
			path: "",
			sessions: projectlessPrimeSessions,
		});
	}
	return workspaces;
}

function toWorkspaceSession(
	session: components["schemas"]["ControllersSessionView"],
	workspaceId: string,
	workspaceName: string,
): WorkspaceSession {
	return {
		id: session.id,
		terminalHandleId: session.terminalHandleId,
		workspaceId,
		workspaceName,
		title: session.displayName ?? session.issueId ?? session.id,
		issueId: session.issueId,
		provider: toAgentProvider(session.harness),
		kind: toSessionKind(session.kind),
		branch: session.branch ?? `session/${session.id}`,
		status: toSessionStatus(session.status, session.isTerminated),
		createdAt: session.createdAt,
		updatedAt: session.updatedAt,
		activity: toSessionActivity(session.activity),
		previewUrl: session.previewUrl,
		previewRevision: session.previewRevision,
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
