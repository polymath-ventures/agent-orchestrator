import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../api/schema";
import type { AgentModelAvailabilityResponse } from "../hooks/useModelAvailabilityQuery";
import { PrimeSection } from "./PrimeSection";

const { getMock, putMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	putMock: vi.fn(),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, PUT: putMock },
	apiErrorMessage: (error: unknown) =>
		typeof error === "object" && error && "message" in error ? String(error.message) : "API error",
}));

function renderPrimeSection() {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(
		<QueryClientProvider client={queryClient}>
			<PrimeSection />
		</QueryClientProvider>,
	);
	return queryClient;
}

async function chooseOption(trigger: HTMLElement, optionName: string) {
	await userEvent.click(trigger);
	await userEvent.click(await screen.findByRole("option", { name: optionName }));
}

const modelAvailability: AgentModelAvailabilityResponse = {
	checkedAt: "2026-07-23T01:02:03Z",
	harnesses: [
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
					defaultEffort: "medium",
					verified: true,
					status: "reachable",
				},
			],
		},
		{
			id: "kiro",
			label: "Kiro",
			reviewerCapable: false,
			catalogSource: "none",
			catalogVerified: false,
			models: [],
		},
	],
};

let primeSettings: components["schemas"]["DomainPrimeSettings"];
let agentCatalog: components["schemas"]["ListAgentsResponse"];
let modelAvailabilityResult: {
	data?: AgentModelAvailabilityResponse;
	error?: { message: string };
};

beforeEach(() => {
	primeSettings = {
		enabled: false,
		displayName: "AO Prime",
		agent: "codex",
		agentConfig: { model: "gpt-5-codex", effort: "high", permissions: "auto" },
		rules: "Keep watch.",
		rulesFile: "/etc/ao/prime.md",
		wakeInterval: "15m",
	};
	agentCatalog = {
		supported: [
			{ id: "codex", label: "Codex", reviewerCapable: true },
			{ id: "kiro", label: "Kiro", reviewerCapable: false },
		],
		installed: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
		authorized: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
	};
	modelAvailabilityResult = { data: modelAvailability, error: undefined };
	getMock.mockReset().mockImplementation((path: string) => {
		if (path === "/api/v1/prime/settings") {
			return Promise.resolve({
				data: { settings: primeSettings },
				error: undefined,
			});
		}
		if (path === "/api/v1/agents/models") {
			return Promise.resolve(modelAvailabilityResult);
		}
		if (path === "/api/v1/agents") {
			return Promise.resolve({
				data: agentCatalog,
				error: undefined,
			});
		}
		return Promise.resolve({ data: undefined, error: { message: `unexpected GET ${path}` } });
	});
	putMock.mockReset().mockResolvedValue({
		data: {
			settings: { enabled: true, displayName: "Fleet Lead", agent: "codex", agentConfig: {}, wakeInterval: "20m" },
		},
		error: undefined,
	});
});

describe("PrimeSection", () => {
	it("loads global Prime settings with harness, model, minutes, and instructions fields", async () => {
		renderPrimeSection();

		expect(await screen.findByLabelText("Enable Prime")).not.toBeChecked();
		expect(screen.getByLabelText("Display name")).toHaveValue("AO Prime");
		await waitFor(() => expect(screen.getByLabelText("Harness")).toHaveValue("codex"));
		expect(screen.getByLabelText("Model")).toHaveValue("gpt-5-codex");
		expect(screen.getByLabelText("Effort")).toHaveValue("high");
		expect(screen.getByRole("combobox", { name: "Permission mode" })).toHaveTextContent("Auto");
		expect(screen.getByLabelText("Harness").querySelector('option[value=""]')).toHaveTextContent("Select harness");
		expect(screen.getByLabelText("Model")).toHaveAttribute("placeholder", "Select model");
		expect(screen.getByLabelText("Effort").querySelector('option[value=""]')).toHaveTextContent("Select effort");
		expect(screen.getByLabelText("Wake interval minutes")).toHaveValue(15);
		expect(screen.getByLabelText("Instructions file path")).toHaveValue("/etc/ao/prime.md");
		expect(screen.getByText(/Manual model IDs are allowed/i)).toBeInTheDocument();
		expect(screen.getByText(/Inline instructions are loaded first/i)).toBeInTheDocument();
		expect(screen.queryByText(/Legacy Prime environment/i)).not.toBeInTheDocument();
	});

	it("groups instruction fields after the model selector", async () => {
		renderPrimeSection();

		await screen.findByLabelText("Enable Prime");
		await waitFor(() => expect(screen.getByLabelText("Harness")).toHaveValue("codex"));
		const displayName = screen.getByText("Display name");
		const wakeInterval = screen.getByText("Wake interval minutes");
		const modelSection = screen.getByText("Prime model and effort");
		const manualModelHelp = screen.getByText(/Manual model IDs are allowed/i);
		const inlineInstructions = screen.getByText("Inline instructions");
		const instructionsFilePath = screen.getByText("Instructions file path");
		const helpText = screen.getByText(/Inline instructions are loaded first/i);

		expect(displayName.compareDocumentPosition(wakeInterval) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
		expect(wakeInterval.compareDocumentPosition(modelSection) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
		expect(modelSection.compareDocumentPosition(manualModelHelp) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
		expect(manualModelHelp.compareDocumentPosition(inlineInstructions) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
		expect(modelSection.compareDocumentPosition(inlineInstructions) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
		expect(
			inlineInstructions.compareDocumentPosition(instructionsFilePath) & Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
		expect(instructionsFilePath.compareDocumentPosition(helpText) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
	});

	it("limits Prime harness choices to installed inventory plus the saved harness", async () => {
		renderPrimeSection();

		const harness = await screen.findByLabelText("Harness");
		await waitFor(() =>
			expect(
				Array.from(harness.querySelectorAll("option")).map((option) => ({
					value: option.value,
					label: option.textContent,
				})),
			).toEqual([
				{ value: "", label: "Select harness" },
				{ value: "codex", label: "Codex" },
			]),
		);
	});

	it("does not offer installed harnesses that are explicitly unauthorized", async () => {
		agentCatalog = {
			...agentCatalog,
			supported: [...(agentCatalog.supported ?? []), { id: "cursor", label: "Cursor", reviewerCapable: false }],
			installed: [
				...(agentCatalog.installed ?? []),
				{ id: "cursor", label: "Cursor", authStatus: "unauthorized", reviewerCapable: false },
			],
		};
		modelAvailabilityResult = {
			data: {
				...modelAvailability,
				harnesses: [
					...modelAvailability.harnesses,
					{
						id: "cursor",
						label: "Cursor",
						reviewerCapable: false,
						catalogSource: "none",
						catalogVerified: false,
						models: [],
					},
				],
			},
			error: undefined,
		};

		renderPrimeSection();

		const harness = await screen.findByLabelText("Harness");
		await waitFor(() =>
			expect(Array.from(harness.querySelectorAll("option")).map((option) => option.value)).toEqual(["", "codex"]),
		);
	});

	it("falls back to authorized agent inventory when model catalogs are unavailable", async () => {
		modelAvailabilityResult = { data: undefined, error: { message: "offline" } };
		primeSettings = { ...primeSettings, agent: "", agentConfig: {} };

		renderPrimeSection();

		const harness = await screen.findByLabelText("Harness");
		await waitFor(() =>
			expect(
				Array.from(harness.querySelectorAll("option")).map((option) => ({
					value: option.value,
					label: option.textContent,
				})),
			).toEqual([
				{ value: "", label: "Select harness" },
				{ value: "codex", label: "Codex" },
			]),
		);
		expect(await screen.findByText(/Model catalogs are unavailable/i)).toBeInTheDocument();
	});

	it("hides non-actionable unknown model probe status", async () => {
		modelAvailabilityResult = {
			data: {
				...modelAvailability,
				harnesses: modelAvailability.harnesses.map((harness) =>
					harness.id === "codex"
						? {
								...harness,
								models: harness.models.map((model) => ({
									...model,
									verified: false,
									status: "unknown" as const,
									reason: "not probed; only configured pins are live-validated",
								})),
							}
						: harness,
				),
			},
			error: undefined,
		};

		renderPrimeSection();

		await waitFor(() => expect(screen.getByLabelText("Model")).toHaveValue("gpt-5-codex"));
		expect(screen.queryByText(/Status: unknown/i)).not.toBeInTheDocument();
		expect(screen.queryByText(/only configured pins are live-validated/i)).not.toBeInTheDocument();
	});

	it("saves edited global Prime settings", async () => {
		const user = userEvent.setup();
		renderPrimeSection();

		await user.click(await screen.findByLabelText("Enable Prime"));
		await user.clear(screen.getByLabelText("Display name"));
		await user.type(screen.getByLabelText("Display name"), "Fleet Lead");
		await user.clear(screen.getByLabelText("Wake interval minutes"));
		await user.type(screen.getByLabelText("Wake interval minutes"), "20");
		await chooseOption(screen.getByRole("combobox", { name: "Permission mode" }), "Accept edits");
		await user.click(screen.getByRole("button", { name: "Save Prime" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock).toHaveBeenCalledWith("/api/v1/prime/settings", {
			body: {
				settings: expect.objectContaining({
					enabled: true,
					displayName: "Fleet Lead",
					agent: "codex",
					agentConfig: expect.objectContaining({ permissions: "accept-edits" }),
					wakeInterval: "20m",
				}),
			},
		});
	});

	it("displays Claude Code when the saved Prime harness has dropped out of the catalog, preserving the saved harness on save", async () => {
		const user = userEvent.setup();
		primeSettings = {
			...primeSettings,
			agent: "ghost-harness",
			agentConfig: { model: "ghost-model", effort: "high", permissions: "auto" },
		};
		agentCatalog = {
			supported: [
				...(agentCatalog.supported ?? []),
				{ id: "claude-code", label: "Claude Code", reviewerCapable: true },
			],
			installed: [
				...(agentCatalog.installed ?? []),
				{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
			],
			authorized: [
				...(agentCatalog.authorized ?? []),
				{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
			],
		};
		modelAvailabilityResult = {
			data: {
				...modelAvailability,
				harnesses: [
					...modelAvailability.harnesses,
					{
						id: "claude-code",
						label: "Claude Code",
						reviewerCapable: true,
						catalogSource: "known-set",
						catalogVerified: false,
						models: [],
					},
				],
			},
			error: undefined,
		};

		renderPrimeSection();

		await waitFor(() => expect(screen.getByLabelText("Harness")).toHaveValue("claude-code"));

		await user.click(screen.getByRole("button", { name: "Save Prime" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock).toHaveBeenCalledWith("/api/v1/prime/settings", {
			body: { settings: expect.objectContaining({ agent: "ghost-harness" }) },
		});
	});

	it("rejects wake intervals outside the supported minute range before saving", async () => {
		const user = userEvent.setup();
		renderPrimeSection();

		await user.clear(await screen.findByLabelText("Wake interval minutes"));
		await user.type(screen.getByLabelText("Wake interval minutes"), "361");
		await user.click(screen.getByRole("button", { name: "Save Prime" }));

		expect(await screen.findByText("Wake interval must be between 1 and 360 minutes.")).toBeInTheDocument();
		expect(putMock).not.toHaveBeenCalled();
	});

	it("does not coerce persisted non-minute wake durations into a valid minute value", async () => {
		const user = userEvent.setup();
		primeSettings = { ...primeSettings, wakeInterval: "30s" };
		renderPrimeSection();

		const wakeInterval = await screen.findByLabelText("Wake interval minutes");
		await waitFor(() => expect(wakeInterval).toHaveValue(null));
		await user.click(screen.getByRole("button", { name: "Save Prime" }));

		expect(await screen.findByText("Wake interval must be between 1 and 360 minutes.")).toBeInTheDocument();
		expect(putMock).not.toHaveBeenCalled();
	});

	it("parses persisted decimal duration values without dropping characters", async () => {
		primeSettings = { ...primeSettings, wakeInterval: "1.5h" };
		renderPrimeSection();

		const wakeInterval = await screen.findByLabelText("Wake interval minutes");
		await waitFor(() => expect(wakeInterval).toHaveValue(90));
	});
});
