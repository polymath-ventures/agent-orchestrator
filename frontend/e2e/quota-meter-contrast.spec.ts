/**
 * The quota meter's usage bar must stay legible in every theme.
 *
 * This guard exists because a class-name assertion would not have caught the bug
 * it guards (agent-orchestrator#289). Two separate things broke, both silently:
 * the 2026-08-07 upstream sync deleted the fork-only `--color-quota-track` token
 * the groove was painted with, and the same sync's named-theme system redefined
 * `--accent` — the normal-severity fill — as a subtle hover surface
 * (`oklch(0.967 …)` in light). The markup never changed; only the resolved
 * colours did.
 *
 * So this reads the colours the browser actually computes, for every theme scope
 * `tokens.css` defines, and asserts real contrast. A deleted token resolves to
 * transparent and fails; a token redefined too close to its neighbour fails on
 * ratio. Do not rewrite this as a `toHaveClass` check — that is precisely the
 * test that stayed green throughout the bug.
 */
import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

/** Mirrors ThemeStyle in src/renderer/lib/theme.ts. */
const THEME_STYLES = [
	"orchestrate",
	"github",
	"catppuccin",
	"dracula",
	"tokyo-night",
	"rose-pine",
	"nord",
	"gruvbox",
	"solarized",
] as const;

const APPEARANCES = ["light", "dark"] as const;

/**
 * WCAG 1.4.11 non-text contrast. The bar is a graphical object conveying
 * information, so 3:1 against its adjacent colour is the accessibility floor
 * rather than a taste preference.
 */
const MIN_RATIO = 3;

/** A normal-severity reading — the only severity that was broken. */
const metrics = {
	history: [],
	probeStatuses: [
		{
			harness: "codex",
			state: "ok",
			hasData: true,
			probedAt: new Date(Date.now() - 4 * 60_000).toISOString(),
			snapshots: [{ windowName: "primary", used: 20, windowEnd: new Date(Date.now() + 36 * 3600_000).toISOString() }],
		},
	],
	latest: { quotas: [] },
};

type Rgba = { r: number; g: number; b: number; a: number };

/** Flatten a translucent colour onto what sits behind it. */
function over(fg: Rgba, bg: Rgba): Rgba {
	return {
		r: fg.r * fg.a + bg.r * (1 - fg.a),
		g: fg.g * fg.a + bg.g * (1 - fg.a),
		b: fg.b * fg.a + bg.b * (1 - fg.a),
		a: 1,
	};
}

function luminance({ r, g, b }: Rgba): number {
	const channel = (raw: number) => {
		const c = raw / 255;
		return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
	};
	return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

function contrast(a: Rgba, b: Rgba): number {
	const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
	return (hi + 0.05) / (lo + 0.05);
}

test("the usage bar stays legible against its track in every theme", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.route("**/api/v1/metrics", (route) => route.fulfill({ json: metrics }));
	await page.route("**/api/v1/metrics/probe", (route) => route.fulfill({ json: { statuses: [] } }));

	await page.goto("/");
	const bar = page.getByRole("progressbar", { name: /quota usage$/ }).first();
	await expect(bar).toBeVisible();

	const failures: string[] = [];

	for (const style of THEME_STYLES) {
		for (const appearance of APPEARANCES) {
			// Exactly what applyDocumentTheme / applyDocumentThemeStyle do.
			await page.evaluate(
				([styleName, mode]) => {
					const root = document.documentElement;
					root.dataset.theme = mode;
					root.style.colorScheme = mode;
					if (styleName === "orchestrate") delete root.dataset.styleTheme;
					else root.dataset.styleTheme = styleName;
				},
				[style, appearance] as const,
			);

			const sample = await bar.evaluate((track) => {
				const fillEl = track.firstElementChild;
				if (!fillEl) throw new Error("the bar has no fill element");

				// Let the browser parse the colour rather than regexing it here:
				// getComputedStyle hands back whatever function the token was authored
				// in (`oklch(0.945 0.003 286.32)`, `#58a6ff`, `rgb(…)`), and a canvas
				// resolves every one of those to sRGB bytes.
				const canvas = document.createElement("canvas");
				canvas.width = canvas.height = 1;
				const ctx = canvas.getContext("2d", { willReadFrequently: true });
				if (!ctx) throw new Error("no 2d context");
				const toRgba = (value: string) => {
					ctx.clearRect(0, 0, 1, 1);
					ctx.fillStyle = value;
					ctx.fillRect(0, 0, 1, 1);
					const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
					return { r, g, b, a: a / 255, css: value };
				};

				// Nearest ancestor painting an opaque background, so a translucent track
				// composites onto what the user actually sees behind it.
				let behind: Element | null = track.parentElement;
				let backdrop = "rgb(255, 255, 255)";
				while (behind) {
					const bg = getComputedStyle(behind).backgroundColor;
					if (toRgba(bg).a > 0) {
						backdrop = bg;
						break;
					}
					behind = behind.parentElement;
				}

				return {
					track: toRgba(getComputedStyle(track).backgroundColor),
					fill: toRgba(getComputedStyle(fillEl).backgroundColor),
					backdrop: toRgba(backdrop),
				};
			});

			const scope = `${style}/${appearance}`;
			const track = over(sample.track, sample.backdrop);

			// Checked separately from contrast, because contrast cannot see this: a
			// transparent track simply falls through to the card behind it, so the fill
			// still contrasts and the ratio still passes while the groove has silently
			// stopped being painted. That is exactly how the deletion of
			// --color-quota-track went unnoticed.
			if (sample.track.a === 0) {
				failures.push(`${scope}: track resolves to nothing (${sample.track.css}) — its token is undefined`);
			}

			// A deleted token resolves to rgba(0, 0, 0, 0). Reported separately from a
			// ratio failure because the fix is different: restore the token, not recolour.
			if (sample.fill.a === 0) {
				failures.push(`${scope}: fill resolves to nothing (${sample.fill.css}) — its token is undefined`);
				continue;
			}

			const fill = over(sample.fill, track);
			const ratio = contrast(fill, track);
			if (ratio < MIN_RATIO) {
				failures.push(
					`${scope}: fill ${sample.fill.css} on track ${sample.track.css} is ${ratio.toFixed(2)}:1, below ${MIN_RATIO}:1`,
				);
			}
		}
	}

	expect(failures, `quota bar contrast failures:\n${failures.join("\n")}`).toEqual([]);
});
