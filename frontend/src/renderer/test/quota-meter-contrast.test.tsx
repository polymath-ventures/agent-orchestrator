/**
 * Contrast guard for the agent-harness quota/usage meter (fork feature 3).
 *
 * The mount guard next door proves the meter is on screen. This one proves you
 * can see the bar once it is: it renders the real `QuotaPanel`, reads the
 * background utilities off the DOM nodes the component actually produced, and
 * resolves those utilities to real colours out of `tokens.css` — once per theme
 * scope the sheet defines.
 *
 * It exists because agent-orchestrator#289 was invisible to every test we had.
 * The 2026-08-07 upstream sync deleted `--color-quota-track` (the track then
 * painted with a custom property defined nowhere) and redefined `--accent` as a
 * near-background hover surface (the normal-severity fill then painted the
 * exact colour of the track). Both casualties are class-name-identical to the
 * working version, so an assertion on `bg-accent` would have passed throughout.
 * Resolving the colour is the whole point — do not weaken these tests back into
 * class-name checks.
 *
 * Floor: WCAG 2.x non-text contrast, 3:1.
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	backgroundClass,
	contrastRatio,
	formatRgb,
	resolveBackgroundColor,
	themeScopes,
	type ThemeScope,
} from "./theme-token-colors";

const { getMock, postMock } = vi.hoisted(() => ({ getMock: vi.fn(), postMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, POST: postMock },
	apiErrorMessage: (e: unknown) => (e instanceof Error ? e.message : "error"),
	hasTrustedApiBaseUrl: () => true,
}));

import { QuotaPanel } from "../components/QuotaPanel";

/** WCAG 2.x minimum contrast for non-text user-interface components. */
const NON_TEXT_CONTRAST_FLOOR = 3;

const AGENTS = {
	supported: [{ id: "codex", label: "Codex" }],
	installed: [{ id: "codex", label: "Codex" }],
	authorized: [],
};

function seed(used: number) {
	getMock.mockImplementation((url: string) => {
		if (url === "/api/v1/agents") return Promise.resolve({ data: AGENTS, error: undefined });
		if (url === "/api/v1/metrics") {
			return Promise.resolve({
				data: {
					history: [],
					latest: { quotas: [] },
					probeStatuses: [
						{
							harness: "codex",
							state: "ok",
							hasData: true,
							probedAt: "2026-08-14T19:00:00Z",
							snapshots: [
								{
									harness: "codex",
									accountId: "acct",
									windowName: "primary",
									used,
									limit: 100,
									signalQuality: "exact",
									source: "probe",
									windowEnd: "2026-08-21T18:17:00Z",
									observedAt: "2026-08-14T19:00:00Z",
								},
							],
						},
					],
				},
				error: undefined,
				response: { status: 200 },
			});
		}
		return Promise.resolve({ data: undefined, error: undefined, response: { status: 200 } });
	});
}

/**
 * Render the real meter at a given usage and hand back the utilities the
 * component chose for the groove and for the fill inside it.
 */
async function meterUtilities(used: number): Promise<{ track: string; fill: string }> {
	seed(used);
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
	const wrapper = ({ children }: { children: ReactNode }) => (
		<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
	);
	render(<QuotaPanel />, { wrapper });

	const meter = await screen.findByRole("progressbar", { name: "Codex primary quota usage" });
	const fillElement = meter.firstElementChild;
	expect(fillElement, "the meter should render a fill element inside its track").not.toBeNull();

	const track = backgroundClass(meter);
	const fill = backgroundClass(fillElement as Element);
	expect(track, `the meter track carries no background utility (classes: ${meter.className})`).not.toBeNull();
	expect(
		fill,
		`the meter fill carries no background utility (classes: ${(fillElement as Element).className})`,
	).not.toBeNull();
	return { track: track as string, fill: fill as string };
}

function resolveOrFail(className: string, scope: ThemeScope, role: string) {
	const color = resolveBackgroundColor(className, scope);
	expect(
		color,
		`${role} utility \`${className}\` resolves to no colour in "${scope.name}" — the custom property behind it is defined nowhere`,
	).not.toBeNull();
	return color as NonNullable<typeof color>;
}

describe("quota meter contrast", () => {
	beforeEach(() => {
		getMock.mockReset();
		postMock.mockReset();
	});

	const scopes = themeScopes();

	it("covers the base pair plus every named theme in both modes", () => {
		// A parser regression that yielded no scopes would make every contrast
		// assertion below pass vacuously, so pin the shape of what we enumerate.
		expect(scopes.length).toBeGreaterThanOrEqual(18);
		expect(scopes.map((scope) => scope.name)).toEqual(
			expect.arrayContaining(["base · dark", "base · light", "github · dark", "solarized · light"]),
		);
		expect(scopes.every((scope) => scope.name.endsWith("dark") || scope.name.endsWith("light"))).toBe(true);
	});

	it("resolves utilities to the colours the compiled stylesheet paints", () => {
		// The contrast assertions are only worth anything if the resolver agrees
		// with Tailwind. These expectations were read off the compiled output
		// (`.bg-accent { background-color: var(--accent) }`, and so on) and pin
		// the two hops that are easy to get wrong: the `@theme inline` bridge,
		// and the `--color-warning` ⇄ `--bridge-warning` round trip.
		const github = scopes.find((scope) => scope.name === "github · dark") as ThemeScope;
		const base = scopes.find((scope) => scope.name === "base · dark") as ThemeScope;
		const solarizedLight = scopes.find((scope) => scope.name === "solarized · light") as ThemeScope;

		expect(formatRgb(resolveBackgroundColor("bg-accent", github) as never)).toBe("#21262d");
		expect(formatRgb(resolveBackgroundColor("bg-muted", solarizedLight) as never)).toBe("#eee8d5");
		expect(formatRgb(resolveBackgroundColor("bg-foreground", github) as never)).toBe("#ccd3d8");
		expect(formatRgb(resolveBackgroundColor("bg-warning", base) as never)).toBe("#fb923c");
		expect(resolveBackgroundColor("bg-error", base)).not.toBeNull();
		// A property nothing declares must read as unresolved, not as a colour.
		expect(resolveBackgroundColor("bg-[var(--color-nonexistent-token)]", base)).toBeNull();
	});

	it("paints a normal-severity fill legible against its track in every theme", async () => {
		const { track, fill } = await meterUtilities(20);

		const failures: string[] = [];
		for (const scope of scopes) {
			const trackColor = resolveOrFail(track, scope, "track");
			const fillColor = resolveOrFail(fill, scope, "fill");
			const ratio = contrastRatio(fillColor, trackColor);
			if (ratio < NON_TEXT_CONTRAST_FLOOR) {
				failures.push(
					`${scope.name}: ${ratio.toFixed(2)}:1 — fill ${formatRgb(fillColor)} (${fill}) on track ${formatRgb(trackColor)} (${track})`,
				);
			}
		}

		expect(
			failures,
			`the usage bar is below the ${NON_TEXT_CONTRAST_FLOOR}:1 non-text contrast floor in:\n${failures.join("\n")}`,
		).toEqual([]);
	});

	it("keeps the warning and critical fills resolvable and distinct from the track", async () => {
		// Warning and critical are out of scope for the contrast floor: they are
		// the severities that stayed visible through #289, and the ticket keeps
		// their colours as they are. They still have to resolve, and they still
		// have to differ from the groove — the deletion half of the regression.
		for (const [severity, used] of [
			["warning", 78],
			["critical", 93],
		] as const) {
			const { track, fill } = await meterUtilities(used);
			expect(fill, `${severity} should not reuse the normal-severity fill`).not.toEqual(track);
			for (const scope of scopes) {
				const trackColor = resolveOrFail(track, scope, `${severity} track`);
				const fillColor = resolveOrFail(fill, scope, `${severity} fill`);
				expect(
					contrastRatio(fillColor, trackColor),
					`${severity} fill is indistinguishable from its track in "${scope.name}"`,
				).toBeGreaterThan(1.5);
			}
		}
	});
});
