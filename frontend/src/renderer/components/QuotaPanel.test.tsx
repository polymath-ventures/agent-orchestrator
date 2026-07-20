import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const { getMock } = vi.hoisted(() => ({ getMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock },
	apiErrorMessage: (e: unknown) => (e instanceof Error ? e.message : "error"),
	hasTrustedApiBaseUrl: () => true,
}));

import { QuotaPanel } from "./QuotaPanel";

function renderWithClient(node: ReactNode) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(<QueryClientProvider client={queryClient}>{node}</QueryClientProvider>);
}

describe("QuotaPanel", () => {
	beforeEach(() => {
		getMock.mockReset();
	});

	it("renders exact quota snapshots from metrics", async () => {
		getMock.mockResolvedValue({
			data: {
				history: [],
				latest: {
					quotas: [
						{
							harness: "codex",
							accountId: "chatgpt",
							model: "gpt-5",
							remaining: 8,
							limit: 100,
							signalQuality: "exact",
							source: "test",
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			},
			error: undefined,
			response: { status: 200 },
		});

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("Quota")).toBeInTheDocument();
		expect(screen.getByText("codex/chatgpt/gpt-5")).toBeInTheDocument();
		expect(screen.getByText("8.0%")).toBeInTheDocument();
		expect(screen.getByText("exact")).toBeInTheDocument();
	});

	it("renders no-signal snapshots honestly", async () => {
		getMock.mockResolvedValue({
			data: {
				history: [],
				latest: {
					quotas: [
						{
							harness: "claude-code",
							accountId: "unknown",
							signalQuality: "none",
							source: "local inspection",
							basis: "No stable public quota surface.",
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			},
			error: undefined,
			response: { status: 200 },
		});

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("claude-code/unknown")).toBeInTheDocument();
		expect(screen.getByText("no signal")).toBeInTheDocument();
		expect(screen.getByText("none")).toBeInTheDocument();
	});

	it("stays hidden when metrics are disabled", async () => {
		getMock.mockResolvedValue({
			data: undefined,
			error: { code: "NOT_IMPLEMENTED", message: "not implemented" },
			response: { status: 501 },
		});

		renderWithClient(<QuotaPanel />);

		await waitFor(() => expect(getMock).toHaveBeenCalledWith("/api/v1/metrics"));
		expect(screen.queryByText("Quota")).not.toBeInTheDocument();
	});
});
