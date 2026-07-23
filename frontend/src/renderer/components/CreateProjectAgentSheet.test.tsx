import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { agentsQueryKey } from "../hooks/useAgentsQuery";
import { modelAvailabilityQueryKey, type AgentModelAvailabilityResponse } from "../hooks/useModelAvailabilityQuery";
import { CreateProjectAgentSheet, defaultAuthorizedAgent, RequiredAgentField } from "./CreateProjectAgentSheet";

const { getMock, postMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	postMock: vi.fn(),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, POST: postMock },
	apiErrorMessage: (error: unknown, fallback?: string) =>
		typeof error === "object" && error !== null && "message" in error
			? String((error as { message: unknown }).message)
			: (fallback ?? "Request failed"),
}));

const modelAvailability: AgentModelAvailabilityResponse = {
	checkedAt: "2026-07-22T03:04:05Z",
	harnesses: [
		{
			id: "claude-code",
			label: "Claude Code",
			reviewerCapable: true,
			catalogSource: "adapter",
			catalogVerified: true,
			models: [
				{
					model: "opus",
					label: "Opus",
					efforts: ["high", "max"],
					defaultEffort: "high",
					verified: true,
					status: "reachable",
				},
			],
		},
		{
			id: "codex-fugu",
			label: "Codex Fugu",
			reviewerCapable: false,
			catalogSource: "known-set",
			catalogVerified: false,
			catalogReason: "installed catalog could not be read",
			models: [
				{
					model: "fugu",
					label: "Fugu",
					efforts: ["high", "xhigh"],
					defaultEffort: "high",
					verified: false,
					status: "unknown",
					reason: "not probed",
					reasonCode: "not-probed",
				},
			],
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
					label: "OpenAI GPT-5.4",
					efforts: ["medium", "high"],
					defaultEffort: "medium",
					verified: true,
					status: "reachable",
				},
			],
		},
	],
};

function renderSheet(onSubmit = vi.fn().mockResolvedValue(undefined)) {
	return renderSheetContext(onSubmit).onSubmit;
}

function renderSheetContext(
	onSubmit = vi.fn().mockResolvedValue(undefined),
	options: { availability?: AgentModelAvailabilityResponse | null } = {},
) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	queryClient.setQueryData(agentsQueryKey, {
		supported: [
			{ id: "claude-code", label: "claude-code", reviewerCapable: true },
			{ id: "codex", label: "codex", reviewerCapable: true },
			{ id: "codex-fugu", label: "Codex Fugu", reviewerCapable: false },
			{ id: "opencode", label: "OpenCode", reviewerCapable: true },
		],
		installed: [
			{ id: "claude-code", label: "claude-code", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex", label: "codex", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex-fugu", label: "Codex Fugu", authStatus: "authorized", reviewerCapable: false },
			{ id: "opencode", label: "OpenCode", authStatus: "authorized", reviewerCapable: true },
		],
		authorized: [
			{ id: "claude-code", label: "claude-code", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex", label: "codex", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex-fugu", label: "Codex Fugu", authStatus: "authorized", reviewerCapable: false },
			{ id: "opencode", label: "OpenCode", authStatus: "authorized", reviewerCapable: true },
		],
	});
	if (options.availability !== null) {
		queryClient.setQueryData(modelAvailabilityQueryKey, options.availability ?? modelAvailability);
	}
	render(
		<QueryClientProvider client={queryClient}>
			<CreateProjectAgentSheet
				isCreating={false}
				kind="single_repo"
				onOpenChange={() => undefined}
				onSubmit={onSubmit}
				open={true}
				path="/repo/new-project"
			/>
		</QueryClientProvider>,
	);
	return { onSubmit, queryClient };
}

async function chooseOption(trigger: HTMLElement, optionName: string) {
	await userEvent.click(trigger);
	await userEvent.click(await screen.findByRole("option", { name: optionName }));
}

describe("CreateProjectAgentSheet", () => {
	beforeEach(() => {
		getMock.mockReset();
		postMock.mockReset();
	});

	it("chooses the highest-priority authorized default agent", () => {
		expect(
			defaultAuthorizedAgent([
				{ id: "opencode", label: "OpenCode", authStatus: "authorized", reviewerCapable: true },
				{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true },
			]),
		).toBe("codex");
	});

	it("falls back to the alphabetically first authorized agent when no priority agent is authorized", () => {
		expect(
			defaultAuthorizedAgent([
				{ id: "goose", label: "Goose", authStatus: "authorized", reviewerCapable: false },
				{ id: "devin", label: "Devin", authStatus: "authorized", reviewerCapable: false },
			]),
		).toBe("devin");
	});

	it("uses the compact trigger size for agent fields", () => {
		render(
			<RequiredAgentField
				id="agent"
				label="Agent"
				onChange={() => undefined}
				placeholder="Project default"
				value="claude-code"
			/>,
		);

		expect(screen.getByLabelText("Agent")).toHaveAttribute("data-size", "sm");
	});

	it("caps the agent menu height with a theme token", async () => {
		render(
			<RequiredAgentField id="agent" label="Agent" onChange={() => undefined} placeholder="Project default" value="" />,
		);

		await userEvent.click(screen.getByLabelText("Agent"));

		expect(await screen.findByRole("listbox")).toHaveClass("max-h-select-menu-max!");
	});

	it("creates without intake when the toggle is left off", async () => {
		const onSubmit = renderSheet();

		await userEvent.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
		expect(onSubmit).toHaveBeenCalledWith({
			workerAgent: "claude-code",
			orchestratorAgent: "claude-code",
			reviewerAgent: "",
			modelDefaults: {},
			trackerIntake: undefined,
		});
	});

	it("uses harness terminology and keeps the reviewer automatic by default", () => {
		renderSheet();

		expect(screen.getByRole("dialog", { name: "Project harnesses" })).toBeInTheDocument();
		expect(screen.getByRole("combobox", { name: "Worker harness" })).toBeInTheDocument();
		expect(screen.getByRole("combobox", { name: "Orchestrator harness" })).toBeInTheDocument();
		expect(screen.getByRole("combobox", { name: "Reviewer harness" })).toHaveTextContent(
			"Automatic independent reviewer",
		);
		expect(screen.queryByText("Optional model override")).not.toBeInTheDocument();
		expect(screen.getByText("Harness availability is cached.")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Refresh harnesses" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Refresh model catalog" })).toBeInTheDocument();
	});

	it("shows one default model row per selected harness and submits configured defaults", async () => {
		const onSubmit = renderSheet();

		await chooseOption(screen.getByRole("combobox", { name: "Orchestrator harness" }), "codex");
		await chooseOption(screen.getByRole("combobox", { name: "Reviewer harness" }), "codex");
		expect(screen.getByText("Claude Code default model")).toBeInTheDocument();
		expect(screen.getByText("codex default model")).toBeInTheDocument();
		expect(screen.getAllByText(/launch may fail if the harness rejects/i)).toHaveLength(2);

		await userEvent.clear(document.getElementById("newProjectModel-claude-code-model")!);
		await userEvent.type(document.getElementById("newProjectModel-claude-code-model")!, "opus");
		await userEvent.selectOptions(document.getElementById("newProjectModel-claude-code-effort")!, "high");
		await userEvent.type(document.getElementById("newProjectModel-codex-model")!, "gpt-5-codex");
		await userEvent.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
		expect(onSubmit).toHaveBeenCalledWith({
			workerAgent: "claude-code",
			orchestratorAgent: "codex",
			reviewerAgent: "codex",
			modelDefaults: {
				"claude-code": { model: "opus", effort: "high" },
				codex: { model: "gpt-5-codex", effort: "" },
			},
			trackerIntake: undefined,
		});
	});

	it("hides advisory setup model probe status but keeps actionable warnings", async () => {
		const setupAvailability: AgentModelAvailabilityResponse = {
			...modelAvailability,
			harnesses: [
				...modelAvailability.harnesses,
				{
					id: "codex",
					label: "Codex",
					reviewerCapable: true,
					catalogSource: "adapter",
					catalogVerified: true,
					models: [
						{
							model: "gpt-5.5",
							label: "GPT-5.5",
							efforts: ["high"],
							defaultEffort: "high",
							verified: false,
							status: "unknown",
							reason: "not probed; only configured pins are live-validated",
							reasonCode: "not-probed",
						},
						{
							model: "retired-model",
							label: "Retired model",
							efforts: ["high"],
							defaultEffort: "high",
							verified: false,
							status: "unreachable",
							reason: "model rejected by provider",
						},
					],
				},
			],
		};
		renderSheetContext(vi.fn(), { availability: setupAvailability });

		await chooseOption(screen.getByRole("combobox", { name: "Worker harness" }), "codex");
		await userEvent.type(document.getElementById("newProjectModel-codex-model")!, "gpt-5.5");

		expect(screen.queryByText(/Status: unknown/i)).not.toBeInTheDocument();
		expect(screen.queryByText(/not probed/i)).not.toBeInTheDocument();
		expect(screen.getAllByText(/launch may fail if the harness rejects/i).length).toBeGreaterThan(0);

		await userEvent.clear(document.getElementById("newProjectModel-codex-model")!);
		await userEvent.type(document.getElementById("newProjectModel-codex-model")!, "retired-model");

		expect(screen.getByText(/Status: unreachable/i)).toBeInTheDocument();
		expect(screen.getByText(/model rejected by provider/i)).toBeInTheDocument();
	});

	it("restores an edited harness tuple and clears a harness with no edit", async () => {
		renderSheet();

		await chooseOption(screen.getByRole("combobox", { name: "Worker harness" }), "codex");
		await userEvent.type(document.getElementById("newProjectModel-codex-model")!, "gpt-5-codex");
		await chooseOption(screen.getByRole("combobox", { name: "Worker harness" }), "claude-code");
		expect(document.getElementById("newProjectModel-codex-model")).not.toBeInTheDocument();

		await chooseOption(screen.getByRole("combobox", { name: "Worker harness" }), "codex");
		expect(document.getElementById("newProjectModel-codex-model")).toHaveValue("gpt-5-codex");
	});

	it("shows fallback provenance for the selected create-project harness", async () => {
		renderSheet();

		await chooseOption(screen.getByRole("combobox", { name: "Worker harness" }), "Codex Fugu");
		expect(screen.getByText(/known fallback catalog/i)).toBeInTheDocument();
		expect(screen.getByText(/installed catalog could not be read/i)).toBeInTheDocument();
	});

	it("falls back to agent inventory when model catalogs are unavailable and keeps free-form model entry", async () => {
		getMock.mockResolvedValue({ data: undefined, error: { message: "catalog offline" } });
		renderSheetContext(vi.fn(), { availability: null });

		expect(
			await screen.findByText(
				/model catalogs are unavailable.*model id manually.*selected harness default/i,
				undefined,
				{
					timeout: 4_000,
				},
			),
		).toBeInTheDocument();
		expect(screen.getByText("claude-code default model")).toBeInTheDocument();

		await chooseOption(screen.getByRole("combobox", { name: "Worker harness" }), "codex");
		await userEvent.type(document.getElementById("newProjectModel-codex-model")!, "private/codex-model");
		expect(document.getElementById("newProjectModel-codex-model")).toHaveValue("private/codex-model");
	});

	it("keeps cached model rows when an explicit refresh fails", async () => {
		getMock.mockResolvedValue({ data: undefined, error: { message: "refresh offline" } });
		renderSheetContext();
		await chooseOption(screen.getByRole("combobox", { name: "Worker harness" }), "Codex Fugu");

		await userEvent.click(screen.getAllByRole("button", { name: "Refresh model catalog" })[0]);
		await waitFor(() => expect(getMock).toHaveBeenCalledWith("/api/v1/agents/models", expect.anything()));

		expect(screen.getByText(/known fallback catalog/i)).toBeInTheDocument();
	});

	it("blocks submit when intake is enabled with no assignee, then passes the intake payload once one is set", async () => {
		const onSubmit = renderSheet();
		await chooseOption(screen.getByRole("combobox", { name: "Worker harness" }), "claude-code");
		await chooseOption(screen.getByRole("combobox", { name: "Orchestrator harness" }), "codex");

		await userEvent.click(screen.getByLabelText("Enable issue intake"));
		// Enabled with no eligibility rule → submit stays disabled (compact sheet
		// carries no inline guard prose; gating is the disabled button).
		expect(screen.getByRole("button", { name: "Create and start" })).toBeDisabled();

		await userEvent.type(screen.getByLabelText("Assignee"), "octocat");
		await userEvent.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
		expect(onSubmit).toHaveBeenCalledWith({
			workerAgent: "claude-code",
			orchestratorAgent: "codex",
			reviewerAgent: "",
			modelDefaults: {},
			trackerIntake: { enabled: true, provider: "github", repo: undefined, assignee: "octocat" },
		});
	});

	it("keeps the create sheet minimal: info tooltip instead of prose, no repo row or credential hint", async () => {
		renderSheet();
		// Info affordance is present even before enabling; the descriptive prose is not.
		expect(screen.getByLabelText("What does enabling issue intake do?")).toBeInTheDocument();
		expect(screen.queryByText(/Auto-spawn worker sessions from matching tracker issues/)).not.toBeInTheDocument();

		await userEvent.click(screen.getByLabelText("Enable issue intake"));
		expect(screen.queryByText("Repository")).not.toBeInTheDocument();
		expect(screen.queryByText(/Reads credentials from/)).not.toBeInTheDocument();
	});
});
