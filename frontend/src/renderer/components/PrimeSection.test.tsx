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

beforeEach(() => {
	primeSettings = {
		enabled: false,
		displayName: "AO Prime",
		agent: "codex",
		agentConfig: { model: "gpt-5-codex", effort: "high" },
		rules: "Keep watch.",
		rulesFile: "/etc/ao/prime.md",
		wakeInterval: "15m",
	};
	getMock.mockReset().mockImplementation((path: string) => {
		if (path === "/api/v1/prime/settings") {
			return Promise.resolve({
				data: { settings: primeSettings },
				error: undefined,
			});
		}
		if (path === "/api/v1/agents/models") {
			return Promise.resolve({ data: modelAvailability, error: undefined });
		}
		if (path === "/api/v1/agents") {
			return Promise.resolve({
				data: {
					supported: [
						{ id: "codex", label: "Codex", reviewerCapable: true },
						{ id: "kiro", label: "Kiro", reviewerCapable: false },
					],
					installed: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
					authorized: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
				},
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

		expect(await screen.findByLabelText("Enable fleet Prime")).not.toBeChecked();
		expect(screen.getByLabelText("Display name")).toHaveValue("AO Prime");
		await waitFor(() => expect(screen.getByLabelText("Harness")).toHaveValue("codex"));
		expect(screen.getByLabelText("Model")).toHaveValue("gpt-5-codex");
		expect(screen.getByLabelText("Effort")).toHaveValue("high");
		expect(screen.getByLabelText("Wake interval minutes")).toHaveValue(15);
		expect(screen.getByLabelText("Instructions file path")).toHaveValue("/etc/ao/prime.md");
		expect(screen.getByText(/Inline instructions are loaded first/i)).toBeInTheDocument();
		expect(screen.queryByText(/Legacy Prime environment/i)).not.toBeInTheDocument();
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

	it("saves edited global Prime settings", async () => {
		const user = userEvent.setup();
		renderPrimeSection();

		await user.click(await screen.findByLabelText("Enable fleet Prime"));
		await user.clear(screen.getByLabelText("Display name"));
		await user.type(screen.getByLabelText("Display name"), "Fleet Lead");
		await user.clear(screen.getByLabelText("Wake interval minutes"));
		await user.type(screen.getByLabelText("Wake interval minutes"), "20");
		await user.click(screen.getByRole("button", { name: "Save Prime" }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock).toHaveBeenCalledWith("/api/v1/prime/settings", {
			body: {
				settings: expect.objectContaining({
					enabled: true,
					displayName: "Fleet Lead",
					agent: "codex",
					wakeInterval: "20m",
				}),
			},
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
