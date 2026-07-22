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
		],
		installed: [
			{ id: "claude-code", label: "claude-code", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex", label: "codex", authStatus: "authorized", reviewerCapable: true },
		],
		authorized: [
			{ id: "claude-code", label: "claude-code", authStatus: "authorized", reviewerCapable: true },
			{ id: "codex", label: "codex", authStatus: "authorized", reviewerCapable: true },
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
			modelOverride: { harness: "", model: "", effort: "" },
			trackerIntake: undefined,
		});
	});

	it("submits an explicit Fugu model and effort tuple", async () => {
		const onSubmit = renderSheet();

		await userEvent.selectOptions(screen.getByLabelText("Harness"), "codex-fugu");
		await userEvent.clear(screen.getByLabelText("Model"));
		await userEvent.type(screen.getByLabelText("Model"), "fugu");
		await userEvent.selectOptions(screen.getByLabelText("Effort"), "xhigh");
		await userEvent.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
		expect(onSubmit).toHaveBeenCalledWith({
			workerAgent: "claude-code",
			orchestratorAgent: "claude-code",
			modelOverride: { harness: "codex-fugu", model: "fugu", effort: "xhigh" },
			trackerIntake: undefined,
		});
	});

	it("restores an edited harness tuple and clears a harness with no edit", async () => {
		renderSheet();

		await userEvent.selectOptions(screen.getByLabelText("Harness"), "codex-fugu");
		await userEvent.type(screen.getByLabelText("Model"), "fugu");
		await userEvent.selectOptions(screen.getByLabelText("Effort"), "xhigh");
		await userEvent.selectOptions(screen.getByLabelText("Harness"), "opencode");
		expect(screen.getByLabelText("Model")).toHaveValue("");
		expect(screen.getByLabelText("Effort")).toHaveValue("");

		await userEvent.selectOptions(screen.getByLabelText("Harness"), "codex-fugu");
		expect(screen.getByLabelText("Model")).toHaveValue("fugu");
		expect(screen.getByLabelText("Effort")).toHaveValue("xhigh");
	});

	it("shows fallback provenance for the selected create-project harness", async () => {
		renderSheet();

		await userEvent.selectOptions(screen.getByLabelText("Harness"), "codex-fugu");
		expect(screen.getByText(/known fallback catalog/i)).toBeInTheDocument();
		expect(screen.getByText(/installed catalog could not be read/i)).toBeInTheDocument();
	});

	it("falls back to agent inventory when model catalogs are unavailable and keeps free-form model entry", async () => {
		getMock.mockResolvedValue({ data: undefined, error: { message: "catalog offline" } });
		renderSheetContext(vi.fn(), { availability: null });

		expect(
			await screen.findByText(/model catalogs are unavailable.*agent inventory.*model id manually/i, undefined, {
				timeout: 4_000,
			}),
		).toBeInTheDocument();
		expect(screen.getByLabelText("Harness")).toHaveDisplayValue("Agent default");
		expect(screen.getByRole("option", { name: "codex" })).toBeInTheDocument();

		await userEvent.selectOptions(screen.getByLabelText("Harness"), "codex");
		await userEvent.type(screen.getByLabelText("Model"), "private/codex-model");
		expect(screen.getByLabelText("Model")).toHaveValue("private/codex-model");
	});

	it("keeps cached model rows when an explicit refresh fails", async () => {
		getMock.mockResolvedValue({ data: undefined, error: { message: "refresh offline" } });
		renderSheetContext();
		await userEvent.selectOptions(screen.getByLabelText("Harness"), "codex-fugu");

		await userEvent.click(screen.getByRole("button", { name: "Refresh models" }));
		await waitFor(() => expect(getMock).toHaveBeenCalledWith("/api/v1/agents/models", expect.anything()));

		expect(screen.getByLabelText("Harness")).toHaveDisplayValue("Codex Fugu");
		expect(screen.getByText(/known fallback catalog/i)).toBeInTheDocument();
	});

	it("blocks submit when intake is enabled with no assignee, then passes the intake payload once one is set", async () => {
		const onSubmit = renderSheet();
		await chooseOption(screen.getByLabelText("Worker agent"), "claude-code");
		await chooseOption(screen.getByLabelText("Orchestrator agent"), "codex");

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
			modelOverride: { harness: "", model: "", effort: "" },
			trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
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
