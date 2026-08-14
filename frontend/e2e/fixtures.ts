import type { Page, Route } from "@playwright/test";
import type { components } from "../src/api/schema";

const now = "2026-07-20T18:00:00.000Z";

type ShellTerminalFixture = {
	handleId: string;
	projectId?: string;
	sessionId?: string;
	workingDir: string;
	title: string;
	createdAt: string;
};

export async function installBrowserModeApiFixtures(page: Page, options: { includePrimeSession?: boolean } = {}) {
	const state = {
		muxConnections: 0,
		shellSeq: 1,
		shellTerminals: [
			{
				handleId: "shellterm-fixture-1",
				projectId: "api-gateway",
				workingDir: "/Users/me/api-gateway",
				title: "api-gateway",
				createdAt: now,
			},
		] as ShellTerminalFixture[],
	};
	await page.routeWebSocket(/\/mux$/, (ws) => {
		state.muxConnections += 1;
		ws.onMessage((message) => {
			if (typeof message !== "string") return;
			const frame = JSON.parse(message) as { ch?: string; data?: string; id?: string; type?: string };
			if (frame.ch !== "terminal" || !frame.id) return;
			if (frame.type === "open") {
				ws.send(JSON.stringify({ ch: "terminal", type: "opened", id: frame.id }));
				ws.send(
					JSON.stringify({
						ch: "terminal",
						type: "data",
						id: frame.id,
						data: Buffer.from("mux fixture ready\r\n", "utf8").toString("base64"),
					}),
				);
			}
		});
	});
	await page.route("**/healthz", (route) =>
		route.fulfill({
			json: {
				status: "ok",
				service: "agent-orchestrator-daemon",
				pid: 4242,
			},
		}),
	);
	await page.route("**/readyz", (route) =>
		route.fulfill({
			json: {
				status: "ready",
				service: "agent-orchestrator-daemon",
				pid: 4242,
			},
		}),
	);
	await page.route("**/api/v1/events", (route) =>
		route.fulfill({
			contentType: "text/event-stream",
			body: "event: ping\ndata: {}\n\n",
		}),
	);
	await page.route("**/api/v1/import", (route) => route.fulfill({ json: { available: false, legacyRoot: "" } }));
	await page.route("**/api/v1/notifications", (route) => route.fulfill({ json: { notifications: [] } }));
	await page.route("**/api/v1/attention/operator", (route) => route.fulfill({ json: { items: [] } }));
	await page.route("**/api/v1/behavior/convergence", (route) => route.fulfill({ json: { items: [] } }));
	await page.route("**/api/v1/shell-terminals", async (route) => {
		if (route.request().method() === "GET") {
			return route.fulfill({ json: { shellTerminals: state.shellTerminals } });
		}
		if (route.request().method() !== "POST") return route.fallback();
		const body = (route.request().postDataJSON() ?? {}) as { projectId?: string; sessionId?: string };
		state.shellSeq += 1;
		// Echo sessionId back like the daemon does: a session-scoped shell only
		// surfaces in that session's own tab strip (SessionView filters on it), so
		// a shell opened from a session pane must carry the scope to appear there.
		const shellTerminal = {
			handleId: `shellterm-fixture-${state.shellSeq}`,
			projectId: body.projectId,
			sessionId: body.sessionId,
			workingDir: `/Users/me/${body.projectId ?? ".ao"}`,
			title: body.projectId ?? "shell",
			createdAt: new Date().toISOString(),
		};
		state.shellTerminals = [...state.shellTerminals, shellTerminal];
		return route.fulfill({ status: 201, json: { shellTerminal } });
	});
	await page.route("**/api/v1/shell-terminals/*", (route) => {
		if (route.request().method() !== "DELETE") return route.fallback();
		const handleId = decodeURIComponent(route.request().url().split("/").pop() ?? "");
		state.shellTerminals = state.shellTerminals.filter((terminal) => terminal.handleId !== handleId);
		return route.fulfill({ status: 204, body: "" });
	});
	await page.route("**/api/v1/projects", (route) => {
		if (route.request().method() !== "GET") return route.fallback();
		return route.fulfill({
			json: {
				projects: [
					{
						id: "api-gateway",
						kind: "git",
						name: "api-gateway",
						orchestratorAgent: "codex",
						path: "/Users/me/api-gateway",
						sessionPrefix: "ao",
					},
				],
			},
		});
	});
	await page.route("**/api/v1/projects/api-gateway", (route) =>
		route.fulfill({
			json: {
				status: "ok",
				project: {
					id: "api-gateway",
					kind: "git",
					name: "api-gateway",
					path: "/Users/me/api-gateway",
					repo: "api-gateway",
					defaultBranch: "main",
					config: {
						orchestrator: { agent: "codex" },
						worker: { agent: "codex" },
						reviewers: [{ harness: "codex" }],
					},
				},
			},
		}),
	);
	await page.route("**/api/v1/sessions", (route) => {
		if (route.request().method() !== "GET") return route.fallback();
		return route.fulfill({
			json: {
				sessions: [
					...(options.includePrimeSession
						? [
								session({
									id: "fleet-prime",
									kind: "prime",
									displayName: "AO Prime",
									status: "working",
									terminalHandleId: "term-prime",
								}),
							]
						: []),
					session({
						id: "orch-api-gateway",
						kind: "orchestrator",
						displayName: "Orchestrator",
						status: "working",
						terminalHandleId: "term-orch",
					}),
					session({
						id: "fix-webgl-fallback",
						kind: "worker",
						displayName: "fix-webgl-fallback",
						branch: "fix/webgl-fallback",
						status: "working",
						terminalHandleId: "term-webgl",
					}),
					session({
						id: "refactor-mux",
						kind: "worker",
						displayName: "Split terminal mux responsibilities",
						branch: "refactor/mux",
						status: "working",
						terminalHandleId: "term-refactor-mux",
					}),
					session({
						id: "stacked-auth",
						kind: "worker",
						displayName: "auth stack",
						branch: "stacked/auth",
						status: "review_pending",
						terminalHandleId: "term-stacked-auth",
						prs: [prFacts(41, "open"), prFacts(42, "draft"), prFacts(40, "merged")],
					}),
					session({
						id: "demo-ci-failed",
						kind: "worker",
						displayName: "Fix flaky renderer smoke",
						branch: "fix/renderer-smoke",
						status: "ci_failed",
						terminalHandleId: "term-demo-ci-failed",
						autoInjectCI: false,
						prs: [
							{
								...prFacts(43, "open"),
								ci: "failing",
								mergeability: "blocked",
								review: "none",
							},
						],
					}),
				],
			},
		});
	});
	await page.route("**/api/v1/sessions/*/pr", handleSessionPRs);
	await page.route("**/api/v1/sessions/*/reviews", handleSessionReviews);
	await page.route("**/api/v1/sessions/*/workspace/files", (route) =>
		route.fulfill({
			json: {
				sessionId: "refactor-mux",
				truncated: false,
				files: [
					{
						path: "internal/mux/terminal_mux.go",
						status: "modified",
						additions: 28,
						deletions: 9,
					},
				],
			},
		}),
	);
	await page.route("**/api/v1/sessions/*/workspace/file?*", (route) =>
		route.fulfill({
			json: {
				path: "internal/mux/terminal_mux.go",
				status: "modified",
				patch: "@@ -1 +1 @@\n-old\n+new\n",
			},
		}),
	);
	return state;
}

function session(input: {
	autoInjectCI?: boolean;
	branch?: string;
	displayName: string;
	id: string;
	kind: "orchestrator" | "prime" | "worker";
	prs?: components["schemas"]["SessionPRFacts"][];
	status: string;
	terminalHandleId: string;
}) {
	return {
		activity: { lastActivityAt: now, state: "active" },
		autoInjectCI: input.autoInjectCI ?? true,
		branch: input.branch ?? "main",
		createdAt: now,
		displayName: input.displayName,
		harness: "codex",
		id: input.id,
		isTerminated: false,
		issueId: input.kind === "worker" ? "#2" : undefined,
		kind: input.kind,
		projectId: input.kind === "prime" ? undefined : "api-gateway",
		prs: input.prs ?? [],
		status: input.status,
		terminalHandleId: input.terminalHandleId,
		updatedAt: now,
	};
}

function prFacts(number: number, state: "open" | "draft" | "merged"): components["schemas"]["SessionPRFacts"] {
	return {
		ci: state === "open" ? "passing" : "unknown",
		mergeability: state === "open" ? "mergeable" : "unknown",
		number,
		review: state === "open" ? "approved" : "none",
		reviewComments: false,
		state,
		updatedAt: now,
		url: `https://github.com/me/api-gateway/pull/${number}`,
	};
}

function prSummary(
	number: number,
	state: "open" | "draft" | "merged",
	title: string,
): components["schemas"]["SessionPRSummary"] {
	return {
		additions: number,
		author: "agent",
		changedFiles: 1,
		ci: {
			autoInjectCI: true,
			failingChecks: [],
			state: state === "open" ? "passing" : "unknown",
		},
		deletions: 2,
		headSha: `sha-${number}`,
		htmlUrl: `https://github.com/me/api-gateway/pull/${number}`,
		mergeability: {
			prUrl: `https://github.com/me/api-gateway/pull/${number}`,
			reasons: [],
			state: state === "open" ? "mergeable" : "unknown",
		},
		number,
		observedAt: now,
		provider: "github",
		repo: "me/api-gateway",
		review: {
			decision: state === "open" ? "approved" : "none",
			hasUnresolvedHumanComments: false,
			unresolvedBy: [],
		},
		sourceBranch: `branch-${number}`,
		state,
		targetBranch: "main",
		title,
		updatedAt: now,
		url: `https://github.com/me/api-gateway/pull/${number}`,
	};
}

function handleSessionPRs(route: Route) {
	const sessionId =
		route
			.request()
			.url()
			.match(/\/sessions\/([^/]+)\/pr/)?.[1] ?? "";
	const prs: components["schemas"]["SessionPRSummary"][] =
		sessionId === "stacked-auth"
			? [
					prSummary(41, "open", "Auth stack parent"),
					prSummary(42, "draft", "Auth stack child"),
					prSummary(40, "merged", "Auth stack base"),
				]
			: sessionId === "demo-ci-failed"
				? [
						{
							...prSummary(43, "open", "Fix flaky renderer smoke"),
							ci: {
								autoInjectCI: false,
								failingChecks: [
									{
										conclusion: "failure",
										name: "renderer smoke",
										status: "failed",
										url: "https://github.com/me/api-gateway/actions/runs/43/jobs/1",
									},
								],
								state: "failing",
							},
							mergeability: {
								prUrl: "https://github.com/me/api-gateway/pull/43",
								reasons: ["required checks failing"],
								state: "blocked",
							},
							review: {
								decision: "none",
								hasUnresolvedHumanComments: false,
								unresolvedBy: [],
							},
						},
					]
				: [];
	return route.fulfill({ json: { sessionId, prs } });
}

function handleSessionReviews(route: Route) {
	const sessionId =
		route
			.request()
			.url()
			.match(/\/sessions\/([^/]+)\/reviews/)?.[1] ?? "";
	return route.fulfill({
		json: {
			reviewerHandleId: sessionId === "stacked-auth" ? "reviewer-pane" : "",
			reviews:
				sessionId === "stacked-auth"
					? [
							{
								latestRun: {
									batchId: "batch-1",
									body: "Looks good.",
									createdAt: now,
									githubReviewId: "4101",
									harness: "codex",
									id: "run-1",
									prUrl: "https://github.com/me/api-gateway/pull/41",
									reviewId: "review-1",
									sessionId,
									status: "delivered",
									targetSha: "sha-41",
									verdict: "approved",
								},
								prNumber: 41,
								prUrl: "https://github.com/me/api-gateway/pull/41",
								status: "up_to_date",
								targetSha: "sha-41",
								title: "Auth stack parent",
							},
						]
					: [],
		},
	});
}
