import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getMock, putMock, postMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	putMock: vi.fn(),
	postMock: vi.fn(),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: {
		GET: getMock,
		PUT: putMock,
		POST: postMock,
	},
	apiErrorMessage: (error: unknown) => {
		if (error instanceof Error) return error.message;
		if (typeof error === "object" && error !== null && "message" in error) {
			return String((error as { message: unknown }).message);
		}
		return "Request failed";
	},
}));

import { ProjectSettingsForm } from "./ProjectSettingsForm";
import { modelAvailabilityQueryKey } from "../hooks/useModelAvailabilityQuery";
import { workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import type { WorkspaceSummary } from "../types/workspace";

function renderSettings(projectId = "proj-1", workspaces?: WorkspaceSummary[]) {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: { retry: false },
			mutations: { retry: false },
		},
	});
	if (workspaces) {
		queryClient.setQueryData(workspaceQueryKey, workspaces);
	}
	render(
		<QueryClientProvider client={queryClient}>
			<ProjectSettingsForm projectId={projectId} />
		</QueryClientProvider>,
	);
	return queryClient;
}

async function chooseOption(trigger: HTMLElement, optionName: string) {
	await userEvent.click(trigger);
	await userEvent.click(await screen.findByRole("option", { name: optionName }));
}

const agentCatalogResponse = {
	data: {
		supported: [
			{ id: "claude-code", label: "Claude Code", reviewerCapable: true },
			{ id: "codex", label: "Codex", reviewerCapable: true },
			{ id: "codex-fugu", label: "Codex Fugu", reviewerCapable: false },
			{ id: "goose", label: "Goose", reviewerCapable: false },
			{ id: "kiro", label: "Kiro", reviewerCapable: false },
			{ id: "opencode", label: "OpenCode", reviewerCapable: true },
		],
		installed: [
			{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex-fugu", label: "Codex Fugu", authStatus: "authorized", reviewerCapable: false },
			{ id: "goose", label: "Goose", authStatus: "authorized", reviewerCapable: false },
			{ id: "kiro", label: "Kiro", authStatus: "unknown", reviewerCapable: false },
			{ id: "opencode", label: "OpenCode", authStatus: "authorized", reviewerCapable: true },
		],
		authorized: [
			{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex-fugu", label: "Codex Fugu", authStatus: "authorized", reviewerCapable: false },
			{ id: "goose", label: "Goose", authStatus: "authorized", reviewerCapable: false },
			{ id: "opencode", label: "OpenCode", authStatus: "authorized", reviewerCapable: true },
		],
	},
	error: undefined,
};

const modelCatalogResponse = {
	data: {
		checkedAt: "2026-07-22T00:00:00Z",
		harnesses: [
			{
				id: "claude-code",
				label: "Claude Code",
				reviewerCapable: true,
				catalogSource: "known-set",
				catalogVerified: false,
				models: [{ model: "opus", label: "Opus", efforts: ["high"], verified: false, status: "unknown" }],
			},
			{
				id: "codex",
				label: "Codex",
				reviewerCapable: true,
				catalogSource: "adapter",
				catalogVerified: true,
				models: [
					{
						model: "gpt-5-codex",
						label: "GPT-5 Codex",
						efforts: ["medium", "high"],
						verified: true,
						status: "reachable",
					},
				],
			},
			{
				id: "codex-fugu",
				label: "Codex Fugu",
				reviewerCapable: false,
				catalogSource: "adapter",
				catalogVerified: true,
				models: [{ model: "fugu", label: "Fugu", efforts: ["xhigh"], verified: true, status: "reachable" }],
			},
			{
				id: "opencode",
				label: "OpenCode",
				reviewerCapable: true,
				catalogSource: "adapter",
				catalogVerified: true,
				models: [
					{
						model: "openai/gpt-5.4",
						label: "GPT-5.4",
						efforts: ["high", "turbo"],
						verified: true,
						status: "reachable",
					},
				],
			},
		],
	},
	error: undefined,
};

function mockProject(project: Record<string, unknown>) {
	getMock.mockImplementation(async (path: string) => {
		if (path === "/api/v1/agents") return agentCatalogResponse;
		if (path === "/api/v1/agents/models") return modelCatalogResponse;
		if (path.includes("/roles/") && path.endsWith("/prompt")) return { data: { prompt: "" }, error: undefined };
		return {
			data: {
				status: "ok",
				project,
			},
			error: undefined,
		};
	});
}

beforeEach(() => {
	getMock.mockReset();
	putMock.mockReset();
	postMock.mockReset();
	putMock.mockResolvedValue({ data: { project: {} }, error: undefined });
	postMock.mockResolvedValue({
		data: { orchestrator: { id: "proj-1-orch-2" } },
		error: undefined,
		response: { status: 200 },
	});
});

describe("ProjectSettingsForm", () => {
	it("atomically saves the project display name and config without changing its stable ID", async () => {
		mockProject({
			id: "tg_content_factory_5863f66be3",
			name: "tg_content_factory_5863f66be3",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings("tg_content_factory_5863f66be3");

		const projectName = await screen.findByLabelText("Project name");
		await userEvent.clear(projectName);
		await userEvent.type(projectName, "TG Content Factory");
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock).toHaveBeenCalledWith("/api/v1/projects/{id}", {
			params: { path: { id: "tg_content_factory_5863f66be3" } },
			body: expect.objectContaining({ displayName: "TG Content Factory" }),
		});
		expect(screen.getByText("tg_content_factory_5863f66be3")).toBeInTheDocument();
	});

	it("loads the current project settings and saves the exposed fields without dropping hidden config", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			configETag: "etag-project-one",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "git@github.com:acme/project-one.git",
			defaultBranch: "main",
			config: {
				defaultBranch: "develop",
				sessionPrefix: "po",
				env: { FOO: "bar" },
				symlinks: [".env"],
				postCreate: ["npm install"],
				worker: {
					agent: "codex",
					agentConfig: { model: "worker-model" },
				},
				orchestrator: { agent: "claude-code" },
				agentConfig: {
					model: "claude-opus-4-5",
					modelByHarness: {
						"claude-code": { model: "opus" },
						codex: { model: "gpt-5-codex" },
					},
					permissions: "auto",
				},
				reviewers: [{ harness: "claude-code", agentConfig: { model: "claude-review" } }],
			},
			modelAvailability: [
				{
					harness: "codex",
					model: "gpt-5-codex",
					status: "unknown",
					reason: "not-probed",
				},
			],
		});

		renderSettings();

		expect(await screen.findByText("git@github.com:acme/project-one.git")).toBeInTheDocument();
		expect(screen.getByLabelText("Default branch")).toHaveValue("develop");
		expect(screen.getByLabelText("Session prefix")).toHaveValue("po");
		const projectHarness = screen.getByRole("combobox", { name: "Project model harness" });
		expect(projectHarness).toHaveTextContent("claude-code");
		expect(document.getElementById("project-model-model")).toHaveValue("opus");
		expect(screen.queryByText(/Status: unknown/i)).not.toBeInTheDocument();
		expect(document.getElementById("reviewer-model-model")).toHaveValue("claude-review");

		const workerAgent = screen.getByRole("combobox", { name: "Default worker agent" });
		const orchestratorAgent = screen.getByRole("combobox", { name: "Default orchestrator agent" });
		const permissionMode = screen.getByRole("combobox", { name: "Permission mode" });
		const reviewerAgent = screen.getByRole("combobox", { name: "Default reviewer agent" });
		expect(workerAgent).toHaveTextContent("codex");
		expect(orchestratorAgent).toHaveTextContent("claude-code");
		expect(permissionMode).toHaveTextContent("Auto");
		expect(reviewerAgent).toHaveTextContent("claude-code");

		await userEvent.clear(screen.getByLabelText("Default branch"));
		await userEvent.type(screen.getByLabelText("Default branch"), "release");
		await userEvent.clear(screen.getByLabelText("Session prefix"));
		await userEvent.type(screen.getByLabelText("Session prefix"), "rel");
		await chooseOption(projectHarness, "Scalar fallback");
		expect(document.getElementById("project-model-model")).toHaveValue("claude-opus-4-5");
		await userEvent.clear(document.getElementById("project-model-model")!);
		await userEvent.type(document.getElementById("project-model-model")!, "  gpt-5-codex  ");
		await chooseOption(projectHarness, "Codex");
		await userEvent.clear(document.getElementById("project-model-model")!);
		await userEvent.type(document.getElementById("project-model-model")!, "gpt-5.1-codex");
		await userEvent.clear(document.getElementById("reviewer-model-model")!);
		await userEvent.type(document.getElementById("reviewer-model-model")!, "  claude-reviewer  ");
		await chooseOption(workerAgent, "OpenCode");
		await chooseOption(orchestratorAgent, "Goose");
		await chooseOption(permissionMode, "Bypass permissions");

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock).toHaveBeenCalledWith("/api/v1/projects/{id}", {
			params: { path: { id: "proj-1" }, header: { "If-Match": "etag-project-one" } },
			body: {
				displayName: "Project One",
				config: {
					defaultBranch: "release",
					sessionPrefix: "rel",
					env: { FOO: "bar" },
					symlinks: [".env"],
					postCreate: ["npm install"],
					worker: {
						agent: "opencode",
						agentConfig: { modelByHarness: { codex: { model: "worker-model" } } },
					},
					orchestrator: { agent: "goose" },
					agentConfig: {
						model: "gpt-5-codex",
						modelByHarness: {
							"claude-code": { model: "opus" },
							codex: { model: "gpt-5.1-codex" },
						},
						permissions: "bypass-permissions",
					},
					reviewers: [
						{
							harness: "claude-code",
							agentConfig: { modelByHarness: { "claude-code": { model: "claude-reviewer" } } },
						},
					],
				},
			},
		});
		await waitFor(() => expect(postMock).toHaveBeenCalledTimes(1));
		expect(postMock).toHaveBeenCalledWith("/api/v1/orchestrators", {
			body: { projectId: "proj-1", clean: true },
		});
		expect(await screen.findByText("Saved.")).toBeInTheDocument();
	}, 20_000);

	it("loads and saves per-role instruction overrides", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "git@github.com:acme/project-one.git",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
				reviewerRulesFile: "docs/review-rules.md",
				primeRules: "Prime the next task.",
				primeRulesFile: "docs/prime-rules.md",
			},
		});

		renderSettings();

		const reviewerFile = await screen.findByLabelText("Reviewer instructions file path (repo-relative or absolute)");
		expect(reviewerFile).toHaveValue("docs/review-rules.md");
		expect(screen.queryByLabelText("Prime instructions")).not.toBeInTheDocument();

		await userEvent.type(screen.getByLabelText("Orchestrator instructions"), "Coordinate through workers.");

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		const body = putMock.mock.calls[0][1].body.config;
		expect(body.reviewerRulesFile).toBe("docs/review-rules.md");
		expect(body.orchestratorRules).toBe("Coordinate through workers.");
		expect(body.primeRules).toBe("Prime the next task.");
		expect(body.primeRulesFile).toBe("docs/prime-rules.md");
	}, 20_000);

	it("renders the selected role's assembled prompt in the inspector", async () => {
		getMock.mockImplementation(async (path: string, options?: { params?: { path?: { role?: string } } }) => {
			if (path === "/api/v1/agents") return agentCatalogResponse;
			if (path === "/api/v1/projects/{id}/roles/{role}/prompt") {
				const role = options?.params?.path?.role ?? "worker";
				return { data: { role, prompt: `ASSEMBLED ${role.toUpperCase()} PROMPT` }, error: undefined };
			}
			return {
				data: {
					status: "ok",
					project: {
						id: "proj-1",
						name: "Project One",
						kind: "single_repo",
						path: "/repo/project-one",
						repo: "git@github.com:acme/project-one.git",
						defaultBranch: "main",
						config: { worker: { agent: "codex" }, orchestrator: { agent: "claude-code" } },
					},
				},
				error: undefined,
			};
		});

		renderSettings();

		expect(await screen.findByText("ASSEMBLED WORKER PROMPT")).toBeInTheDocument();
		await chooseOption(screen.getByRole("combobox", { name: "Role" }), "reviewer");
		expect(await screen.findByText("ASSEMBLED REVIEWER PROMPT")).toBeInTheDocument();
	}, 20_000);

	it("labels the unconfigured reviewer as the automatic independent reviewer", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: { worker: { agent: "codex" }, orchestrator: { agent: "claude-code" } },
		});

		renderSettings();

		expect(await screen.findByRole("combobox", { name: "Default reviewer agent" })).toHaveTextContent(
			"Automatic independent reviewer",
		);
	});

	it("surfaces a fail-closed inspector error instead of a prompt", async () => {
		getMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/agents") return agentCatalogResponse;
			if (path === "/api/v1/projects/{id}/roles/{role}/prompt") {
				return { data: undefined, error: { message: "reviewer rules file docs/x.md: file is empty" } };
			}
			return {
				data: {
					status: "ok",
					project: {
						id: "proj-1",
						name: "Project One",
						kind: "single_repo",
						path: "/repo/project-one",
						repo: "",
						defaultBranch: "main",
						config: { worker: { agent: "codex" }, orchestrator: { agent: "claude-code" } },
					},
				},
				error: undefined,
			};
		});

		renderSettings();

		expect(await screen.findByText(/file is empty/)).toBeInTheDocument();
	}, 20_000);

	it("shows the daemon validation message when the atomic settings save fails", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});
		putMock.mockResolvedValue({
			data: undefined,
			error: { message: "invalid permissions" },
		});

		renderSettings();

		const projectName = await screen.findByLabelText("Project name");
		await userEvent.clear(projectName);
		await userEvent.type(projectName, "Updated Project");
		await userEvent.click(await screen.findByRole("button", { name: "Save changes" }));

		expect(await screen.findByText("invalid permissions")).toBeInTheDocument();
		expect(screen.queryByText("Saved.")).not.toBeInTheDocument();
		expect(postMock).not.toHaveBeenCalled();
	});

	it("rejects a blank project name before sending the settings update", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();

		const projectName = await screen.findByLabelText("Project name");
		await userEvent.clear(projectName);
		await userEvent.type(projectName, "   ");
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		expect(await screen.findByText("Project name is required.")).toBeInTheDocument();
		expect(putMock).not.toHaveBeenCalled();
	});

	it("allows saving an unchanged legacy project name over the current display-name cap", async () => {
		mockProject({
			id: "tg_content_factory_5863f66be3",
			name: "tg_content_factory_5863f66be3",
			configETag: "etag-long-name",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings("tg_content_factory_5863f66be3");

		await userEvent.click(await screen.findByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock).toHaveBeenCalledWith(
			"/api/v1/projects/{id}",
			expect.objectContaining({
				params: { path: { id: "tg_content_factory_5863f66be3" }, header: { "If-Match": "etag-long-name" } },
				body: expect.objectContaining({ displayName: "tg_content_factory_5863f66be3" }),
			}),
		);
	});

	it("rejects a changed project name over the current display-name cap before sending the settings update", async () => {
		mockProject({
			id: "tg_content_factory_5863f66be3",
			name: "tg_content_factory_5863f66be3",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings("tg_content_factory_5863f66be3");

		const projectName = await screen.findByLabelText("Project name");
		await userEvent.clear(projectName);
		await userEvent.type(projectName, "changed_factory_5863f66be3");
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		expect(await screen.findByText("Project name must be 20 characters or fewer.")).toBeInTheDocument();
		expect(putMock).not.toHaveBeenCalled();
	});

	it("requires worker and orchestrator agents for existing projects missing role config", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {},
		});

		renderSettings();

		expect(await screen.findByText("Worker and orchestrator agents are required.")).toBeInTheDocument();
		expect(screen.getByRole("combobox", { name: "Default worker agent" })).toHaveTextContent(
			"Select default worker agent",
		);
		expect(screen.getByRole("combobox", { name: "Default orchestrator agent" })).toHaveTextContent(
			"Select default orchestrator agent",
		);

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		expect(await screen.findAllByText("Worker and orchestrator agents are required.")).toHaveLength(2);
		expect(putMock).not.toHaveBeenCalled();
	});

	it("disables agent selectors while the initial agent catalog is loading", async () => {
		getMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/agents") {
				return new Promise(() => {});
			}
			return {
				data: {
					status: "ok",
					project: {
						id: "proj-1",
						name: "Project One",
						kind: "single_repo",
						path: "/repo/project-one",
						repo: "",
						defaultBranch: "main",
						config: {
							worker: { agent: "codex" },
							orchestrator: { agent: "claude-code" },
						},
					},
				},
				error: undefined,
			};
		});

		renderSettings();

		expect(await screen.findByRole("combobox", { name: "Default worker agent" })).toBeDisabled();
		expect(screen.getByRole("combobox", { name: "Default orchestrator agent" })).toBeDisabled();
		expect(screen.getByRole("combobox", { name: "Default reviewer agent" })).toBeDisabled();
	});

	it("shows unknown-auth agents as selectable with a warning in project settings", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();

		await waitFor(() => expect(screen.getAllByText("/repo/project-one").length).toBeGreaterThan(0));
		const workerAgent = screen.getByRole("combobox", { name: "Default worker agent" });
		await userEvent.click(workerAgent);
		const options = await screen.findAllByRole("option");
		expect(options.map((option) => option.textContent)).toEqual([
			"Claude Code",
			"Codex",
			"OpenCode",
			"Codex Fugu",
			"Goose",
			"KiroAuth unknown",
		]);
		expect(options[5]).not.toHaveAttribute("aria-disabled", "true");
	});

	it("shows scratch identity and saves only scratch-supported settings", async () => {
		mockProject({
			id: "scratch",
			name: "Scratch",
			kind: "scratch",
			path: "/home/me/.ao/scratch/default",
			repo: "",
			defaultBranch: "",
			config: {
				defaultBranch: "main",
				sessionPrefix: "ao",
				env: { FOO: "bar" },
				symlinks: [".env"],
				postCreate: ["npm install"],
				agentRules: "keep work small",
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
				agentConfig: {
					model: "gpt-5-codex",
					permissions: "auto",
				},
				reviewers: [{ harness: "codex" }],
				trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
			},
		});

		renderSettings("scratch");

		expect((await screen.findByText("kind")).closest("div")).toHaveTextContent("scratch");
		expect(screen.queryByLabelText("Default branch")).not.toBeInTheDocument();
		expect(screen.queryByLabelText("Session prefix")).not.toBeInTheDocument();
		expect(screen.queryByText("Reviewers")).not.toBeInTheDocument();
		expect(screen.queryByText("Tracker intake")).not.toBeInTheDocument();

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock).toHaveBeenCalledWith("/api/v1/projects/{id}", {
			params: { path: { id: "scratch" } },
			body: {
				displayName: "Scratch",
				config: {
					env: { FOO: "bar" },
					sessionPrefix: "ao",
					symlinks: [".env"],
					postCreate: ["npm install"],
					agentRules: "keep work small",
					worker: { agent: "codex" },
					orchestrator: { agent: "claude-code" },
					agentConfig: {
						model: "gpt-5-codex",
						permissions: "auto",
					},
				},
			},
		});
		expect(postMock).not.toHaveBeenCalled();
	});

	it("saves GitHub tracker intake settings, deriving the repo from the project's git origin", async () => {
		getMock.mockResolvedValue({
			data: {
				status: "ok",
				project: {
					id: "proj-1",
					name: "Project One",
					kind: "single_repo",
					path: "/repo/project-one",
					repo: "git@github.com:acme/project-one.git",
					defaultBranch: "main",
					config: {
						worker: { agent: "codex" },
						orchestrator: { agent: "claude-code" },
					},
				},
			},
			error: undefined,
		});

		renderSettings();

		await userEvent.click(await screen.findByLabelText("Enable issue intake"));

		// Repository is display-only, derived from the project's own git origin — no input to
		// fill. Assignee is the only eligibility rule in v1.
		expect(screen.getByRole("link", { name: "acme/project-one" })).toHaveAttribute(
			"href",
			"https://github.com/acme/project-one",
		);
		await userEvent.type(screen.getByLabelText("Assignee"), "octocat");

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		const body = putMock.mock.calls[0]?.[1]?.body;
		expect(body.config.trackerIntake).toEqual({
			enabled: true,
			provider: "github",
			assignee: "octocat",
		});
	});

	it("blocks save when intake is enabled with no assignee", async () => {
		getMock.mockResolvedValue({
			data: {
				status: "ok",
				project: {
					id: "proj-1",
					name: "Project One",
					kind: "single_repo",
					path: "/repo/project-one",
					repo: "git@github.com:acme/project-one.git",
					defaultBranch: "main",
					config: {
						worker: { agent: "codex" },
						orchestrator: { agent: "claude-code" },
					},
				},
			},
			error: undefined,
		});

		renderSettings();

		await userEvent.click(await screen.findByLabelText("Enable issue intake"));
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		expect(await screen.findAllByText("Enabling intake requires an assignee.")).toHaveLength(2);
		expect(putMock).not.toHaveBeenCalled();
	});

	it("restarts when the saved orchestrator agent already differs from the running orchestrator", async () => {
		getMock.mockResolvedValue({
			data: {
				status: "ok",
				project: {
					id: "proj-1",
					name: "Project One",
					kind: "single_repo",
					path: "/repo/project-one",
					repo: "",
					defaultBranch: "main",
					config: {
						worker: { agent: "codex" },
						orchestrator: { agent: "goose" },
					},
				},
			},
			error: undefined,
		});

		renderSettings("proj-1", [
			{
				id: "proj-1",
				name: "Project One",
				path: "/repo/project-one",
				orchestratorAgent: "goose",
				sessions: [
					{
						id: "proj-1-orchestrator",
						workspaceId: "proj-1",
						workspaceName: "Project One",
						title: "Orchestrator",
						provider: "claude-code",
						kind: "orchestrator",
						branch: "ao/proj-1-orchestrator",
						status: "working",
						createdAt: "2026-07-03T00:00:00Z",
						updatedAt: "2026-07-03T00:00:00Z",
						prs: [],
					},
				],
			},
		]);

		const orchestratorAgent = await screen.findByRole("combobox", { name: "Default orchestrator agent" });
		expect(orchestratorAgent).toHaveTextContent("goose");

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(postMock).toHaveBeenCalledTimes(1));
		expect(postMock).toHaveBeenCalledWith("/api/v1/orchestrators", {
			body: { projectId: "proj-1", clean: true },
		});
	});

	it("keeps the config save successful when orchestrator replacement fails", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});
		postMock.mockResolvedValue({
			data: undefined,
			error: { message: "missing goose binary" },
			response: { status: 500 },
		});

		const queryClient = renderSettings();
		const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

		const orchestratorAgent = await screen.findByRole("combobox", { name: "Default orchestrator agent" });
		await chooseOption(orchestratorAgent, "Goose");
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(postMock).toHaveBeenCalledTimes(1));
		expect(await screen.findByText("Saved.")).toBeInTheDocument();
		expect(await screen.findByText("Orchestrator restart failed: missing goose binary")).toBeInTheDocument();
		expect(screen.queryByText("Save failed")).not.toBeInTheDocument();
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["project", "proj-1"] });
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: modelAvailabilityQueryKey });
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: workspaceQueryKey });
	});

	it("saves a valid worker mix and the concurrency cap", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();

		await userEvent.click(await screen.findByRole("button", { name: "Add bucket" }));
		await chooseOption(screen.getAllByRole("combobox", { name: "Agent" })[0], "Codex");
		await userEvent.type(screen.getAllByLabelText("Weight")[0], "60");

		await userEvent.click(screen.getByRole("button", { name: "Add bucket" }));
		await chooseOption(screen.getAllByRole("combobox", { name: "Agent" })[1], "OpenCode");
		await userEvent.type(screen.getAllByLabelText("Weight")[1], "40");

		await userEvent.type(screen.getByLabelText("Max live workers (0 = unlimited)"), "3");

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		const body = putMock.mock.calls[0]?.[1]?.body;
		expect(body.config.workerMix).toEqual([
			{ agent: "codex", weight: 60 },
			{ agent: "opencode", weight: 40 },
		]);
		expect(body.config.maxLiveWorkers).toBe(3);
	}, 20_000);

	it("blocks save when the worker mix weights do not sum to 100", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();

		await userEvent.click(await screen.findByRole("button", { name: "Add bucket" }));
		await chooseOption(screen.getAllByRole("combobox", { name: "Agent" })[0], "Codex");
		await userEvent.type(screen.getAllByLabelText("Weight")[0], "60");

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		expect(await screen.findByText("Worker mix weights must sum to 100 (currently 60).")).toBeInTheDocument();
		expect(putMock).not.toHaveBeenCalled();
	}, 20_000);

	it("blocks save when a worker mix bucket has no agent", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();

		await userEvent.click(await screen.findByRole("button", { name: "Add bucket" }));
		await userEvent.type(screen.getAllByLabelText("Weight")[0], "100");
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		expect(await screen.findByText("Worker mix bucket 1 requires an agent.")).toBeInTheDocument();
		expect(putMock).not.toHaveBeenCalled();
	}, 20_000);

	it("blocks save when a worker mix bucket weight is out of range", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();

		await userEvent.click(await screen.findByRole("button", { name: "Add bucket" }));
		await chooseOption(screen.getAllByRole("combobox", { name: "Agent" })[0], "Codex");
		await userEvent.type(screen.getAllByLabelText("Weight")[0], "0");
		await userEvent.click(screen.getByRole("button", { name: "Add bucket" }));
		await chooseOption(screen.getAllByRole("combobox", { name: "Agent" })[1], "OpenCode");
		await userEvent.type(screen.getAllByLabelText("Weight")[1], "100");
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		expect(
			await screen.findByText("Worker mix bucket 1 weight must be a whole number from 1 to 100."),
		).toBeInTheDocument();
		expect(putMock).not.toHaveBeenCalled();
	}, 20_000);

	it("shows a live weight total that reflects edits", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();

		await userEvent.click(await screen.findByRole("button", { name: "Add bucket" }));
		await userEvent.type(screen.getAllByLabelText("Weight")[0], "60");
		await userEvent.click(screen.getByRole("button", { name: "Add bucket" }));
		await userEvent.type(screen.getAllByLabelText("Weight")[1], "30");

		expect(screen.getByText("Total: 90 / 100")).toBeInTheDocument();

		await userEvent.clear(screen.getAllByLabelText("Weight")[1]);
		await userEvent.type(screen.getAllByLabelText("Weight")[1], "40");

		expect(screen.getByText("Total: 100 / 100")).toBeInTheDocument();
	}, 20_000);

	it("round-trips an existing worker mix and cap through a save", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
				workerMix: [
					{ agent: "codex", weight: 70 },
					{ agent: "opencode", model: "gpt-5-codex", weight: 30 },
				],
				maxLiveWorkers: 4,
			},
		});

		renderSettings();

		expect(await screen.findByLabelText("Max live workers (0 = unlimited)")).toHaveValue(4);
		const weights = screen.getAllByLabelText("Weight");
		expect(weights[0]).toHaveValue(70);
		expect(weights[1]).toHaveValue(30);

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		const body = putMock.mock.calls[0]?.[1]?.body;
		expect(body.config.workerMix).toEqual([
			{ agent: "codex", weight: 70 },
			{ agent: "opencode", model: "gpt-5-codex", weight: 30 },
		]);
		expect(body.config.maxLiveWorkers).toBe(4);
	}, 20_000);

	it("renders dynamic model tuples for every settings scope and preserves hidden nested config", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				env: { HIDDEN: "yes" },
				agentConfig: {
					permissions: "auto",
					modelByHarness: { "codex-fugu": { model: "fugu", effort: "xhigh" } },
				},
				worker: {
					agent: "codex",
					agentConfig: {
						permissions: "accept-edits",
						modelByHarness: { codex: { model: "gpt-5-codex", effort: "high" } },
					},
				},
				orchestrator: {
					agent: "claude-code",
					agentConfig: {
						permissions: "bypass-permissions",
						modelByHarness: { "claude-code": { model: "opus", effort: "high" } },
					},
				},
				prime: {
					agent: "opencode",
					agentConfig: {
						permissions: "auto",
						modelByHarness: { opencode: { model: "openai/gpt-5.4", effort: "turbo" } },
					},
				},
				reviewers: [
					{
						harness: "codex",
						agentConfig: { permissions: "auto", modelByHarness: { codex: { model: "gpt-5-codex", effort: "medium" } } },
					},
					{ harness: "opencode", agentConfig: { model: "hidden-reviewer" } },
				],
			},
		});

		renderSettings();

		expect(await screen.findByRole("combobox", { name: "Project model harness" })).toHaveTextContent("codex-fugu");
		expect(screen.getByRole("combobox", { name: "Default worker agent" })).toHaveTextContent("codex");
		expect(screen.getByRole("combobox", { name: "Default orchestrator agent" })).toHaveTextContent("claude-code");
		expect(screen.queryByRole("combobox", { name: "Prime agent" })).not.toBeInTheDocument();
		expect(screen.getByRole("combobox", { name: "Default reviewer agent" })).toHaveTextContent("codex");
		expect(document.getElementById("project-model-model")).toHaveValue("fugu");
		expect(document.getElementById("worker-model-model")).toHaveValue("gpt-5-codex");
		expect(document.getElementById("orchestrator-model-model")).toHaveValue("opus");
		expect(document.getElementById("prime-model-model")).toBeNull();
		expect(document.getElementById("reviewer-model-model")).toHaveValue("gpt-5-codex");

		const projectHarness = screen.getByRole("combobox", { name: "Project model harness" });
		await userEvent.click(projectHarness);
		expect(await screen.findByRole("option", { name: "Codex Fugu" })).toBeInTheDocument();
		await userEvent.keyboard("{Escape}");
		const reviewerHarness = screen.getByRole("combobox", { name: "Default reviewer agent" });
		await userEvent.click(reviewerHarness);
		expect(screen.queryByRole("option", { name: "Codex Fugu" })).not.toBeInTheDocument();
		await userEvent.keyboard("{Escape}");

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		const saved = putMock.mock.calls[0]?.[1]?.body.config;
		expect(saved.env).toEqual({ HIDDEN: "yes" });
		expect(saved.agentConfig).toEqual({
			permissions: "auto",
			modelByHarness: { "codex-fugu": { model: "fugu", effort: "xhigh" } },
		});
		expect(saved.worker.agentConfig).toEqual({
			permissions: "accept-edits",
			modelByHarness: { codex: { model: "gpt-5-codex", effort: "high" } },
		});
		expect(saved.orchestrator.agentConfig).toEqual({
			permissions: "bypass-permissions",
			modelByHarness: { "claude-code": { model: "opus", effort: "high" } },
		});
		expect(saved.prime.agentConfig).toEqual({
			permissions: "auto",
			modelByHarness: { opencode: { model: "openai/gpt-5.4", effort: "turbo" } },
		});
		expect(saved.reviewers).toEqual([
			{
				harness: "codex",
				agentConfig: { permissions: "auto", modelByHarness: { codex: { model: "gpt-5-codex", effort: "medium" } } },
			},
			{ harness: "opencode", agentConfig: { model: "hidden-reviewer" } },
		]);
	}, 20_000);

	it("filters reviewer inventory fallback by server capability while preserving the current legacy reviewer", async () => {
		getMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/agents") return agentCatalogResponse;
			if (path === "/api/v1/agents/models") {
				return { data: undefined, error: { message: "model catalog offline" } };
			}
			if (path === "/api/v1/projects/{id}/roles/{role}/prompt") {
				return { data: { prompt: "assembled prompt" }, error: undefined };
			}
			return {
				data: {
					status: "ok",
					project: {
						id: "proj-1",
						name: "Project One",
						kind: "single_repo",
						path: "/repo/project-one",
						repo: "",
						defaultBranch: "main",
						config: {
							worker: { agent: "codex" },
							orchestrator: { agent: "claude-code" },
							reviewers: [{ harness: "goose" }],
						},
					},
				},
				error: undefined,
			};
		});

		renderSettings();
		const reviewerHarness = await screen.findByRole("combobox", { name: "Default reviewer agent" });
		expect(reviewerHarness).toHaveTextContent("goose");
		expect(
			await screen.findByText(/model catalogs are unavailable.*agent inventory remain usable/i, undefined, {
				timeout: 4_000,
			}),
		).toBeInTheDocument();

		await userEvent.click(reviewerHarness);
		expect(await screen.findByRole("option", { name: "Codex" })).toBeInTheDocument();
		expect(screen.getByRole("option", { name: "OpenCode" })).toBeInTheDocument();
		expect(screen.getByRole("option", { name: "Goose" })).toBeInTheDocument();
		expect(screen.queryByRole("option", { name: "Codex Fugu" })).not.toBeInTheDocument();
	});

	it("restores each worker harness pair and clears a harness with no saved pair", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: {
					agent: "codex",
					agentConfig: {
						modelByHarness: {
							codex: { model: "gpt-5-codex", effort: "high" },
							opencode: { model: "openai/gpt-5.4", effort: "xhigh" },
						},
					},
				},
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();
		const workerHarness = await screen.findByRole("combobox", { name: "Default worker agent" });
		const model = () => document.getElementById("worker-model-model");
		const effort = () => document.getElementById("worker-model-effort");
		expect(model()).toHaveValue("gpt-5-codex");
		expect(effort()).toHaveValue("high");

		await chooseOption(workerHarness, "OpenCode");
		expect(model()).toHaveValue("openai/gpt-5.4");
		expect(effort()).toHaveValue("xhigh");
		await chooseOption(workerHarness, "Codex Fugu");
		expect(model()).toHaveValue("");
		expect(effort()).toHaveValue("");
		await chooseOption(workerHarness, "Codex");
		expect(model()).toHaveValue("gpt-5-codex");
		expect(effort()).toHaveValue("high");
	}, 20_000);

	it("aligns the harness dropdown with the Model/Effort controls in each harness/model row", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
			},
		});

		renderSettings();
		await screen.findByRole("combobox", { name: "Project model harness" });

		// The harness picker's control sits directly under its single label. The
		// Model/Effort controls must match that — their per-field labels stay in
		// the DOM (for native label-click focus + a11y association) but are
		// visually hidden, or they render one row lower than the harness
		// dropdown sharing their bordered row.
		for (const text of ["Model", "Effort"]) {
			for (const label of screen.getAllByText(text, { selector: "label" })) {
				expect(label).toHaveClass("sr-only");
			}
		}

		// ...but must still be reachable with an accessible name for a11y.
		expect(document.getElementById("project-model-model")).toHaveAccessibleName("Model");
		expect(document.getElementById("project-model-effort")).toHaveAccessibleName("Effort");

		// The Model/Effort header row's height is set by its refresh button
		// (h-control-form). The harness label must be pinned to that same
		// token — not a hand-matched pixel value — so the two columns' controls
		// land on the same row.
		const harnessLabel = document.querySelector('label[for="project-model-harness"]');
		expect(harnessLabel).toHaveClass("h-control-form");
		expect(harnessLabel?.parentElement).toHaveClass("gap-2");
	}, 20_000);

	it("keeps a synthetic configured project pin visible when no catalog row exists", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
				agentConfig: { modelByHarness: { goose: { model: "custom/goose", effort: "high" } } },
			},
		});

		renderSettings();
		expect(await screen.findByRole("combobox", { name: "Project model harness" })).toHaveTextContent("goose");
		expect(document.getElementById("project-model-model")).toHaveValue("custom/goose");
	}, 20_000);

	it("preserves per-harness effort when saving a model edit", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "git@github.com:acme/project-one.git",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
				agentConfig: {
					modelByHarness: {
						codex: { model: "gpt-5-codex", effort: "high" },
					},
				},
			},
		});

		renderSettings();

		expect(await screen.findByRole("combobox", { name: "Project model harness" })).toHaveTextContent("codex");
		const modelInput = document.getElementById("project-model-model")!;
		expect(modelInput).toHaveValue("gpt-5-codex");
		await waitFor(() => expect(modelInput).not.toBeDisabled());
		await userEvent.clear(modelInput);
		await userEvent.type(modelInput, "gpt-5.1-codex");

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		const body = putMock.mock.calls[0]?.[1]?.body;
		// An uncatalogued model cannot safely inherit an effort that was only
		// advertised for the old model.
		expect(body.config.agentConfig.modelByHarness).toEqual({
			codex: { model: "gpt-5.1-codex" },
		});
	}, 20_000);

	it("keeps an untouched effort-only entry when another model is cleared", async () => {
		mockProject({
			id: "proj-1",
			name: "Project One",
			kind: "single_repo",
			path: "/repo/project-one",
			repo: "git@github.com:acme/project-one.git",
			defaultBranch: "main",
			config: {
				worker: { agent: "codex" },
				orchestrator: { agent: "claude-code" },
				agentConfig: {
					modelByHarness: {
						codex: { model: "gpt-5-codex", effort: "high" },
						"claude-code": { effort: "low" }, // effort-only: legal daemon config
					},
				},
			},
		});

		renderSettings();

		expect(await screen.findByRole("combobox", { name: "Project model harness" })).toHaveTextContent("claude-code");
		const projectHarness = screen.getByRole("combobox", { name: "Project model harness" });
		await chooseOption(projectHarness, "Codex");
		const modelInput = document.getElementById("project-model-model")!;
		expect(modelInput).toHaveValue("gpt-5-codex");
		await waitFor(() => expect(modelInput).not.toBeDisabled());
		await userEvent.clear(modelInput);

		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		const body = putMock.mock.calls[0]?.[1]?.body;
		// Clearing a selected model clears its stale effort, while a different
		// untouched effort-only entry survives the save verbatim.
		expect(body.config.agentConfig.modelByHarness).toEqual({
			"claude-code": { effort: "low" },
		});
	}, 20_000);
});
