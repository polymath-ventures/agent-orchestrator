import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const { getMock, postMock } = vi.hoisted(() => ({ getMock: vi.fn(), postMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, POST: postMock },
	apiErrorMessage: (e: unknown) => (e instanceof Error ? e.message : "error"),
	hasTrustedApiBaseUrl: () => true,
}));

import { QuotaPanel } from "./QuotaPanel";
import { useUiStore } from "../stores/ui-store";

const AGENTS = {
	supported: [
		{ id: "claude-code", label: "Claude Code" },
		{ id: "codex", label: "Codex" },
	],
	installed: [
		{ id: "claude-code", label: "Claude Code" },
		{ id: "codex", label: "Codex" },
	],
	authorized: [],
};

type Metrics = {
	probeStatuses: Array<Record<string, unknown>>;
	quotas?: Array<Record<string, unknown>>;
};

function seed({ probeStatuses, quotas = [] }: Metrics) {
	getMock.mockImplementation((url: string) => {
		if (url === "/api/v1/agents") {
			return Promise.resolve({ data: AGENTS, error: undefined });
		}
		if (url === "/api/v1/metrics") {
			return Promise.resolve({
				data: { history: [], probeStatuses, latest: { quotas } },
				error: undefined,
				response: { status: 200 },
			});
		}
		return Promise.resolve({ data: undefined, error: undefined, response: { status: 200 } });
	});
}

function renderWithClient(node: ReactNode) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(<QueryClientProvider client={queryClient}>{node}</QueryClientProvider>);
}

describe("QuotaPanel", () => {
	beforeEach(() => {
		getMock.mockReset();
		postMock.mockReset();
		postMock.mockResolvedValue({ data: { statuses: [] }, error: undefined });
		useUiStore.setState({ isQuotaWidgetCollapsed: false });
	});

	it("renders ok+data inline with used percent, reset time, and an inventory label", async () => {
		seed({
			probeStatuses: [{ harness: "claude-code", state: "ok", hasData: true, probedAt: "2026-07-20T19:00:00Z" }],
			quotas: [
				{
					harness: "claude-code",
					accountId: "acct",
					windowName: "weekly (all models)",
					used: 45,
					limit: 100,
					signalQuality: "exact",
					source: "probe",
					windowEnd: "2026-07-25T15:00:00Z",
					observedAt: "2026-07-20T19:00:00Z",
				},
				{
					harness: "claude-code",
					accountId: "acct",
					windowName: "session",
					used: 12,
					limit: 100,
					signalQuality: "exact",
					source: "probe",
					windowEnd: "2026-07-20T21:00:00Z",
					observedAt: "2026-07-20T19:00:00Z",
				},
			],
		});

		renderWithClient(<QuotaPanel />);

		// Label resolves from the agents inventory, not a hardcoded map.
		expect(await screen.findByText("Claude Code")).toBeInTheDocument();
		expect(screen.getByText(/45% used/)).toBeInTheDocument();
		expect(screen.getByText(/45% used/).textContent).toMatch(/resets/);
		// The session window renders as the smaller secondary line.
		expect(screen.getByText(/session 12% used/)).toBeInTheDocument();
	});

	it("renders not_probed inline with a Probe button that triggers a POST", async () => {
		seed({ probeStatuses: [{ harness: "codex", state: "not_probed", hasData: false }] });

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("not probed yet")).toBeInTheDocument();
		const probeButton = screen.getByRole("button", { name: "Probe Codex" });
		await userEvent.click(probeButton);

		await waitFor(() => expect(postMock).toHaveBeenCalledWith("/api/v1/metrics/probe", { body: { harness: "codex" } }));
	});

	it("renders failed state with the reason inline", async () => {
		seed({
			probeStatuses: [{ harness: "codex", state: "failed", hasData: false, reason: "claude exited 1: boom" }],
		});

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText(/probe failed: claude exited 1: boom/)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Probe Codex" })).toBeInTheDocument();
	});

	it("renders ok+empty inline with a no-usage message", async () => {
		seed({ probeStatuses: [{ harness: "codex", state: "ok", hasData: false }] });

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("no usage recorded yet")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Probe Codex" })).toBeInTheDocument();
	});

	it("renders no_source inline without a probe button", async () => {
		seed({
			probeStatuses: [
				{ harness: "claude-code", state: "no_source", hasData: false, reason: "no /usage command found" },
			],
		});

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("no machine-readable source")).toBeInTheDocument();
		expect(screen.getByText("no /usage command found")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: /Probe/ })).not.toBeInTheDocument();
	});

	it("fires a probe-all POST from the header Refresh button", async () => {
		seed({ probeStatuses: [{ harness: "codex", state: "not_probed", hasData: false }] });

		renderWithClient(<QuotaPanel />);

		const refresh = await screen.findByRole("button", { name: "Refresh all quota probes" });
		await userEvent.click(refresh);

		await waitFor(() =>
			expect(postMock).toHaveBeenCalledWith("/api/v1/metrics/probe", { body: { harness: undefined } }),
		);
	});

	it("renders nothing when there are no probe statuses", async () => {
		seed({ probeStatuses: [] });

		renderWithClient(<QuotaPanel />);

		await waitFor(() => expect(getMock).toHaveBeenCalledWith("/api/v1/metrics"));
		expect(screen.queryByText("Quota")).not.toBeInTheDocument();
	});

	it("falls back to the raw harness id when the inventory has no match", async () => {
		seed({ probeStatuses: [{ harness: "mystery-harness", state: "not_probed", hasData: false }] });

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("mystery-harness")).toBeInTheDocument();
	});
});
