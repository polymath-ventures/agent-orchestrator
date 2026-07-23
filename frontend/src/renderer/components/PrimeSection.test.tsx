import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
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

beforeEach(() => {
	getMock.mockReset().mockResolvedValue({
		data: {
			settings: {
				enabled: false,
				displayName: "AO Prime",
				agent: "codex",
				agentConfig: { model: "gpt-5-codex", effort: "high" },
				rules: "Keep watch.",
				wakeInterval: "15m",
			},
			legacyEnvironment: { configured: true, projectId: "ao" },
		},
		error: undefined,
	});
	putMock.mockReset().mockResolvedValue({
		data: {
			settings: { enabled: true, displayName: "Fleet Lead", agent: "codex", agentConfig: {}, wakeInterval: "20m" },
			legacyEnvironment: { configured: true, projectId: "ao" },
		},
		error: undefined,
	});
});

describe("PrimeSection", () => {
	it("loads global Prime settings and legacy env state", async () => {
		renderPrimeSection();

		expect(await screen.findByLabelText("Enable fleet Prime")).not.toBeChecked();
		expect(screen.getByLabelText("Display name")).toHaveValue("AO Prime");
		await waitFor(() => expect(screen.getByLabelText("Agent")).toHaveValue("codex"));
		expect(screen.getByText("Legacy Prime environment is configured for ao.")).toBeInTheDocument();
	});

	it("saves edited global Prime settings", async () => {
		const user = userEvent.setup();
		renderPrimeSection();

		await user.click(await screen.findByLabelText("Enable fleet Prime"));
		await user.clear(screen.getByLabelText("Display name"));
		await user.type(screen.getByLabelText("Display name"), "Fleet Lead");
		await user.clear(screen.getByLabelText("Wake interval"));
		await user.type(screen.getByLabelText("Wake interval"), "20m");
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
});
