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
import { quotaMeterClassName, type QuotaSeverity } from "./quota-meter-colors";
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

	it("renders ok+data as accessible meter rows with used percent, reset time, and an inventory label", async () => {
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
		expect(screen.getByText("45%")).toBeInTheDocument();
		const weeklyMeter = screen.getByRole("progressbar", { name: "Claude Code weekly (all models) quota usage" });
		expect(weeklyMeter).toHaveAttribute("aria-valuemin", "0");
		expect(weeklyMeter).toHaveAttribute("aria-valuemax", "100");
		expect(weeklyMeter).toHaveAttribute("aria-valuenow", "45");
		expect(weeklyMeter.firstElementChild).toHaveStyle({ width: "45%" });
		// The reset renders on its own line (so the narrow sidebar can't clip it).
		expect(screen.getAllByText(/^resets /).length).toBeGreaterThan(0);
		// The session window renders as the smaller secondary line — name and metric
		// are separate spans (the name truncates; the % metric never does).
		expect(screen.getByText("session")).toBeInTheDocument();
		expect(screen.getByText("12%")).toBeInTheDocument();
	});

	it("uses the shared agent label lookup for quota harness names", async () => {
		getMock.mockImplementation((url: string) => {
			if (url === "/api/v1/agents") {
				return Promise.resolve({
					data: {
						supported: [{ id: "codex", label: "Codex Supported" }],
						installed: [{ id: "codex", label: "Codex Installed" }],
						authorized: [{ id: "codex", label: "Codex Authorized" }],
					},
					error: undefined,
				});
			}
			if (url === "/api/v1/metrics") {
				return Promise.resolve({
					data: {
						history: [],
						latest: { quotas: [] },
						probeStatuses: [{ harness: "codex", state: "not_probed", hasData: false }],
					},
					error: undefined,
					response: { status: 200 },
				});
			}
			return Promise.resolve({ data: undefined, error: undefined, response: { status: 200 } });
		});

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("Codex Authorized")).toBeInTheDocument();
		expect(screen.queryByText("Codex Installed")).not.toBeInTheDocument();
	});

	it("names the headline window and renders a dated reset (weekday, month, date, time) — not a bare clock time", async () => {
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

		// The prominent line must NAME its window — no longer an anonymous "46% used".
		// Name and metric are sibling spans; the metric is `shrink-0` so a long name
		// can never truncate it away.
		expect(await screen.findByText("weekly (all models)")).toBeInTheDocument();
		expect(screen.getByText("46%")).toBeInTheDocument();
		// Reset must be dated: "3CharWeekday DD 3CharMonth HH:MM" (24-hour), e.g. "Mon 27 Jul 14:00",
		// and rendered on its own line so the sidebar can't truncate the date away.
		// TZ-robust: assert the SHAPE, so the runner's timezone doesn't pin the exact day/time.
		const reset = screen.getByText(/^resets /);
		expect(reset.textContent).toMatch(/^resets [A-Z][a-z]{2} \d{2} [A-Z][a-z]{2} \d{2}:\d{2}$/);
		// It must NOT render a bare 12-hour clock time like "2:00 PM".
		expect(reset.textContent).not.toMatch(/\d{1,2}:\d{2}\s?(AM|PM)/i);
	});

	// These cases pin which severity each threshold routes to, and that the meter
	// wears exactly the classes `quota-meter-colors.ts` declares — the link the
	// contrast guard relies on when it measures those classes in a real browser.
	// What they deliberately do NOT prove is that the result is legible:
	// a class name is exactly what stayed correct while the token behind it was
	// deleted or redefined (#289). That belongs to
	// `e2e/quota-meter-contrast.spec.ts`, which resolves the colours.
	it.each([
		[74, "normal", "text-foreground", "text-passive"],
		[75, "warning", "text-warning", "text-passive"],
		[89, "warning", "text-warning", "text-passive"],
		[90, "critical", "text-error", "text-error"],
	] as Array<[number, QuotaSeverity, string, string]>)(
		"uses the expected severity classes at %i%% used",
		async (used, expectedSeverity, expectedNumberClass, expectedResetClass) => {
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
								windowName: "weekly (all models)",
								used,
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

			const number = await screen.findByText(`${used}%`);
			const meter = screen.getByRole("progressbar", { name: "Claude Code weekly (all models) quota usage" });
			expect(number).toHaveClass(expectedNumberClass);
			// Exhaustive, not `toHaveClass`: the contrast guard measures a probe
			// wearing exactly these lists, so anything the meter carries that is not
			// declared in `quota-meter-colors.ts` is something the guard never sees.
			// That is how a bar dimmed by `opacity-*`, a blend mode, or a filter
			// could go invisible with the colours themselves unchanged. Declare it
			// there and the guard measures it; add it here only and this fails.
			expect(meter.className).toBe(quotaMeterClassName("track"));
			expect((meter.firstElementChild as Element).className).toBe(quotaMeterClassName("fill", expectedSeverity));
			expect(screen.getByText(/^resets /).parentElement).toHaveClass(expectedResetClass);
			if (used >= 75) {
				expect(screen.getByText(`${100 - used}% left`)).toBeInTheDocument();
			}
		},
	);

	it("renders a 0% window as a neutral meter with a hairline fill", async () => {
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
							used: 0,
							limit: 100,
							signalQuality: "exact",
							source: "probe",
							windowEnd: "2026-07-28T18:17:00Z",
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			],
		});

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("0%")).toHaveClass("text-passive");
		const meter = screen.getByRole("progressbar", { name: "Codex primary quota usage" });
		expect(meter).toHaveAttribute("aria-valuenow", "0");
		expect(meter.firstElementChild).toHaveStyle({ width: "0%", minWidth: "2px" });
	});

	it("announces unknown usage as indeterminate instead of 0%", async () => {
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
							limit: 100,
							signalQuality: "estimated",
							source: "probe",
							windowEnd: "2026-07-28T18:17:00Z",
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			],
		});

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("usage unknown")).toBeInTheDocument();
		const meter = screen.getByRole("progressbar", { name: "Codex primary quota usage" });
		expect(meter).not.toHaveAttribute("aria-valuenow");
		expect(meter).toHaveAttribute("aria-valuetext", "usage unknown");
		expect(meter.firstElementChild).toHaveStyle({ width: "0%" });
		expect(meter.firstElementChild).not.toHaveStyle({ minWidth: "2px" });
	});

	it("clamps over-100 usage to a full critical meter", async () => {
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
							used: 130,
							limit: 100,
							signalQuality: "exact",
							source: "probe",
							windowEnd: "2026-07-28T18:17:00Z",
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			],
		});

		renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("100%")).toHaveClass("text-error");
		expect(screen.getByText("0% left")).toBeInTheDocument();
		const meter = screen.getByRole("progressbar", { name: "Codex primary quota usage" });
		expect(meter).toHaveAttribute("aria-valuenow", "100");
		expect(meter.firstElementChild).toHaveStyle({ width: "100%", minWidth: "2px" });
	});

	it("renders a midnight reset as 00:00, never the ambiguous 24:00", async () => {
		// hourCycle:"h23" guard — `hour12:false` alone can resolve to h24 and print
		// "24:00" for midnight. (Runs under the suite's UTC env, so a UTC-midnight
		// windowEnd is local midnight.)
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
							windowName: "weekly (all models)",
							used: 46,
							limit: 100,
							signalQuality: "exact",
							source: "probe",
							windowEnd: "2026-07-27T00:00:00Z",
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			],
		});

		renderWithClient(<QuotaPanel />);

		const reset = await screen.findByText(/^resets /);
		expect(reset.textContent).toMatch(/\b00:00\b/);
		expect(reset.textContent).not.toMatch(/24:\d{2}/);
	});

	// Each of the three degenerate windowEnd forms the guard handles must omit the
	// reset clause entirely — never render "Invalid Date" or a reset line.
	it.each([
		["missing", undefined],
		["year-one sentinel", "0001-01-01T00:00:00Z"],
		["unparseable", "not-a-date"],
	])("omits the reset clause when windowEnd is %s", async (_label, windowEnd) => {
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
							...(windowEnd === undefined ? {} : { windowEnd }),
							observedAt: "2026-07-20T19:00:00Z",
						},
					],
				},
			],
		});

		const { container } = renderWithClient(<QuotaPanel />);

		expect(await screen.findByText("21%")).toBeInTheDocument();
		expect(screen.queryByText(/^resets /)).toBeNull();
		expect(container.textContent).not.toMatch(/Invalid Date/i);
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
