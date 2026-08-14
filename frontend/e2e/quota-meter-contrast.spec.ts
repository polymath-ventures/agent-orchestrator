/**
 * The quota meter's usage bar must stay legible.
 *
 * A class-name assertion cannot catch what broke here (agent-orchestrator#289):
 * the markup never changed, only the colours it resolved to. The 2026-08-07 sync
 * deleted the fork-only `--color-quota-track` token the groove was painted with,
 * and its named-theme system redefined `--accent` — the normal-severity fill — as
 * a subtle hover surface (`oklch(0.967 …)` in light). `QuotaPanel.test.tsx`
 * asserted `"bg-accent"` throughout and stayed green.
 *
 * So this resolves the colours in a real browser and checks they are actually
 * distinguishable. It reads computed styles rather than screenshot pixels: that
 * covers both ways this has broken — a token deleted, and a token redefined too
 * pale — without a PNG decoder. It would not notice a fill hidden by `opacity` or
 * `clip-path`; no such regression has occurred, and `shell-quota-widget-mount`
 * already guards that the meter renders at all.
 */
import { expect, test } from "@playwright/test";
import { THEME_STYLES } from "../src/renderer/lib/theme";
import { installBrowserModeApiFixtures } from "./fixtures";

const APPEARANCES = ["light", "dark"] as const;

/** WCAG 1.4.11: the bar is a graphical object conveying information. */
const MIN_RATIO = 3;

const metrics = {
	history: [],
	probeStatuses: [
		{
			harness: "codex",
			state: "ok",
			hasData: true,
			probedAt: new Date(Date.now() - 4 * 60_000).toISOString(),
			// Normal severity — the only one that was broken.
			snapshots: [{ windowName: "primary", used: 40, windowEnd: new Date(Date.now() + 36 * 3600_000).toISOString() }],
		},
	],
	latest: { quotas: [] },
};

type Rgba = { r: number; g: number; b: number; a: number; css: string };

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

/** Flatten a translucent colour onto what sits behind it. */
function flatten(fg: Rgba, bg: Rgba): Rgba {
	return {
		r: fg.r * fg.a + bg.r * (1 - fg.a),
		g: fg.g * fg.a + bg.g * (1 - fg.a),
		b: fg.b * fg.a + bg.b * (1 - fg.a),
		a: 1,
		css: fg.css,
	};
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
			// What applyDocumentTheme / applyDocumentThemeStyle do. Applied after mount
			// on purpose: the renderer re-applies its stored theme during hydration and
			// would overwrite anything set before it.
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
				const fill = track.firstElementChild;
				if (!fill) throw new Error("the bar has no fill element");

				// Let the browser parse the colour: getComputedStyle returns whatever
				// function the token was authored in (`oklch(…)`, `#58a6ff`, `rgb(…)`),
				// and a canvas resolves all of them to sRGB bytes.
				const canvas = document.createElement("canvas");
				canvas.width = canvas.height = 1;
				const ctx = canvas.getContext("2d", { willReadFrequently: true });
				if (!ctx) throw new Error("no 2d context");
				const resolve = (css: string) => {
					ctx.clearRect(0, 0, 1, 1);
					ctx.fillStyle = css;
					ctx.fillRect(0, 0, 1, 1);
					const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
					return { r, g, b, a: a / 255, css };
				};

				// The card the bar sits on, for compositing a translucent groove.
				let behind: Element | null = track.parentElement;
				let card = "rgb(255, 255, 255)";
				while (behind) {
					const bg = getComputedStyle(behind).backgroundColor;
					if (resolve(bg).a > 0) {
						card = bg;
						break;
					}
					behind = behind.parentElement;
				}

				return {
					track: resolve(getComputedStyle(track).backgroundColor),
					fill: resolve(getComputedStyle(fill).backgroundColor),
					card: resolve(card),
				};
			});

			const scope = `${style}/${appearance}`;

			// Checked separately from the ratio, and from each other, because the fixes
			// differ: an undefined token needs restoring, a pale one needs recolouring.
			// A transparent groove also hides from the ratio check — it just shows the
			// card, so the fill still contrasts. That is how the deleted track token
			// went unnoticed.
			if (sample.track.a === 0) {
				failures.push(`${scope}: track resolves to nothing (${sample.track.css}) — its token is undefined`);
			}
			if (sample.fill.a === 0) {
				failures.push(`${scope}: fill resolves to nothing (${sample.fill.css}) — its token is undefined`);
				continue;
			}

			const track = flatten(sample.track, sample.card);
			const ratio = contrast(flatten(sample.fill, track), track);
			if (ratio < MIN_RATIO) {
				failures.push(
					`${scope}: fill ${sample.fill.css} on track ${sample.track.css} is ${ratio.toFixed(2)}:1, below ${MIN_RATIO}:1`,
				);
			}
		}
	}

	expect(failures, `quota bar contrast failures:\n${failures.join("\n")}`).toEqual([]);
});
