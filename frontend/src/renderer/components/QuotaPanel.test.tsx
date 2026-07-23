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
			probeStatuses: [
				{
					harness: "claude-code",
					state: "ok",
					hasData: true,
					probedAt: "2026-07-20T19:00:00Z",
					// The widget renders directly from the status's snapshots (no dependence
					// on the separately-refreshed latest.quotas), so seed them here.
					snapshots: [
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

	it("names the headline window and renders a dated reset (weekday, month, date, tz) — not a bare clock time", async () => {
		seed({
			probeStatuses: [
				{
					harness: "claude-code",
					state: "ok",
					hasData: true,
					probedAt: "2026-07-20T19:00:00Z",
					snapshots: [
						{
							harness: "claude-code",
							accountId: "acct",
							// Headline window: the weekly "all models" limit that actually resets days later.
							windowName: "weekly (all models)",
							used: 46,
							limit: 100,
							signalQuality: "exact",
							source: "probe",
							windowEnd: "2026-07-27T14:00:00Z",
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			],
		});

		renderWithClient(<QuotaPanel />);

		const headline = await screen.findByText(/46% used/);
		// The prominent line must NAME its window — no longer an anonymous "46% used".
		expect(headline.textContent).toMatch(/weekly \(all models\)/);
		// Reset must be dated: "3CharWeekday 3CharMonth DD HH:MM TZ" (24-hour), e.g. "Mon Jul 27 14:00 UTC".
		// TZ-robust: assert the SHAPE, so the runner's timezone doesn't pin the exact day/time.
		expect(headline.textContent).toMatch(/resets [A-Z][a-z]{2} [A-Z][a-z]{2} \d{2} \d{2}:\d{2} \S+/);
		// It must NOT render a bare 12-hour clock time like "2:00 PM".
		expect(headline.textContent).not.toMatch(/\d{1,2}:\d{2}\s?(AM|PM)/i);
	});

	it("omits the reset clause when windowEnd is missing or zero-valued — no 'Invalid Date'", async () => {
		seed({
			probeStatuses: [
				{
					harness: "codex",
					state: "ok",
					hasData: true,
					probedAt: "2026-07-20T19:00:00Z",
					snapshots: [
						{
							harness: "codex",
							accountId: "acct",
							windowName: "primary",
							used: 21,
							limit: 100,
							signalQuality: "exact",
							source: "probe",
							// No windowEnd at all — the reset clause must simply be omitted.
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			],
		});

		renderWithClient(<QuotaPanel />);

		const headline = await screen.findByText(/21% used/);
		expect(headline.textContent).not.toMatch(/resets/);
		expect(headline.textContent).not.toMatch(/Invalid Date/i);
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

	it("disables every probe action while any probe is in flight", async () => {
		seed({
			probeStatuses: [
				{ harness: "codex", state: "not_probed", hasData: false },
				{ harness: "claude-code", state: "not_probed", hasData: false },
			],
		});
		// Hold the POST open so the mutation stays pending while we assert.
		let resolvePost: () => void = () => {};
		postMock.mockImplementation(
			() =>
				new Promise((res) => {
					resolvePost = () => res({ data: { statuses: [] }, error: undefined });
				}),
		);

		renderWithClient(<QuotaPanel />);

		const codexBtn = await screen.findByRole("button", { name: "Probe Codex" });
		const claudeBtn = screen.getByRole("button", { name: "Probe Claude Code" });
		const refresh = screen.getByRole("button", { name: "Refresh all quota probes" });
		expect(codexBtn).toBeEnabled();
		expect(claudeBtn).toBeEnabled();
		expect(refresh).toBeEnabled();

		await userEvent.click(codexBtn);

		// A probe for codex is in flight: the codex button, the OTHER harness's
		// button, and the header Refresh must all be disabled to prevent a second
		// concurrent probe.
		await waitFor(() => expect(codexBtn).toBeDisabled());
		expect(claudeBtn).toBeDisabled();
		expect(refresh).toBeDisabled();

		// Once the probe resolves, every action is re-enabled.
		resolvePost();
		await waitFor(() => expect(codexBtn).toBeEnabled());
		expect(claudeBtn).toBeEnabled();
		expect(refresh).toBeEnabled();
	});

	it("surfaces an inline error when a probe mutation rejects", async () => {
		seed({ probeStatuses: [{ harness: "codex", state: "not_probed", hasData: false }] });
		postMock.mockResolvedValue({ data: undefined, error: { message: "probe blew up" } });

		renderWithClient(<QuotaPanel />);

		const probeButton = await screen.findByRole("button", { name: "Probe Codex" });
		await userEvent.click(probeButton);

		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent(/probe failed/i);
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
