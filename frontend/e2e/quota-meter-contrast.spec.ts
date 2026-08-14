/**
 * The quota meter's usage bar must stay legible in every theme.
 *
 * This guard exists because a class-name assertion would not have caught the bug
 * it guards (agent-orchestrator#289). Two things broke silently: the 2026-08-07
 * upstream sync deleted the fork-only `--color-quota-track` token the groove was
 * painted with, and the same sync's named-theme system redefined `--accent` —
 * the normal-severity fill — as a subtle hover surface (`oklch(0.967 …)` in
 * light). The markup never changed; only the resolved colours did.
 *
 * So this measures **rendered pixels**, not declared styles. It screenshots the
 * chip and samples three points: inside the fill, inside the empty track, and the
 * chip background beside the bar. Reading the pixels rather than the tokens is
 * what makes it honest — an earlier version of this file read
 * `getComputedStyle().backgroundColor` and passed happily with `opacity: 0` on
 * the fill, because a declared colour says nothing about what reached the screen.
 * It also means opacity, filters, ancestor compositing and translucency need no
 * special handling here: the browser has already resolved them.
 *
 * Do not rewrite this as a `toHaveClass` check, and do not go back to reading
 * computed styles. Both are exactly the tests that stayed green through this bug.
 */
import { expect, test } from "@playwright/test";
import { PNG } from "pngjs";
import { THEME_STYLES } from "../src/renderer/lib/theme";
import { installBrowserModeApiFixtures } from "./fixtures";

const APPEARANCES = ["light", "dark"] as const;

/**
 * WCAG 1.4.11 non-text contrast. The bar is a graphical object conveying
 * information, so 3:1 against its adjacent colour is the accessibility floor
 * rather than a taste preference.
 */
const MIN_RATIO = 3;

/**
 * The groove is meant to be subtle, so it is checked for *existence* rather than
 * contrast: it must be painted at all. An 8%-alpha track over a light card lands
 * around 20 levels away; anything under this is indistinguishable from an
 * unpainted groove, which is what a deleted token produces.
 */
const MIN_TRACK_DELTA = 4;

/** A normal-severity reading — the only severity that was broken. */
const metrics = {
	history: [],
	probeStatuses: [
		{
			harness: "codex",
			state: "ok",
			hasData: true,
			probedAt: new Date(Date.now() - 4 * 60_000).toISOString(),
			snapshots: [{ windowName: "primary", used: 40, windowEnd: new Date(Date.now() + 36 * 3600_000).toISOString() }],
		},
	],
	latest: { quotas: [] },
};

type Rgb = { r: number; g: number; b: number };

function luminance({ r, g, b }: Rgb): number {
	const channel = (raw: number) => {
		const c = raw / 255;
		return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
	};
	return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

function contrast(a: Rgb, b: Rgb): number {
	const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
	return (hi + 0.05) / (lo + 0.05);
}

function channelDelta(a: Rgb, b: Rgb): number {
	return Math.max(Math.abs(a.r - b.r), Math.abs(a.g - b.g), Math.abs(a.b - b.b));
}

function describe({ r, g, b }: Rgb): string {
	return `rgb(${r}, ${g}, ${b})`;
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
			// Exactly what applyDocumentTheme / applyDocumentThemeStyle do. Applied
			// after mount on purpose: the renderer re-applies its stored theme during
			// hydration and would overwrite anything set before that.
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

			const scope = `${style}/${appearance}`;
			// Find the painted surface behind the bar by computed background rather than
			// by class name: a `bg-background` → `bg-card` refactor is a rename, not a
			// regression, and a guard that fails on it teaches people to distrust it.
			const region = await bar.evaluate((el) => {
				const transparent = (colour: string) => colour === "transparent" || /,\s*0\)$/.test(colour);
				let node: Element | null = el.parentElement;
				while (node && transparent(getComputedStyle(node).backgroundColor)) node = node.parentElement;
				const host = (node ?? el).getBoundingClientRect();
				const self = el.getBoundingClientRect();
				return {
					host: { x: host.x, y: host.y, width: host.width, height: host.height },
					bar: { x: self.x, y: self.y, width: self.width, height: self.height },
				};
			});
			const chipBox = region.host;
			const barBox = region.bar;
			if (!chipBox.width || !barBox.width) {
				failures.push(`${scope}: the bar or its surrounding surface has no layout box`);
				continue;
			}

			const png = PNG.sync.read(await page.screenshot({ clip: chipBox }));
			const scale = png.width / chipBox.width;
			const pixel = (cssX: number, cssY: number): Rgb => {
				const x = Math.min(png.width - 1, Math.max(0, Math.round((cssX - chipBox.x) * scale)));
				const y = Math.min(png.height - 1, Math.max(0, Math.round((cssY - chipBox.y) * scale)));
				const i = (png.width * y + x) << 2;
				return { r: png.data[i], g: png.data[i + 1], b: png.data[i + 2] };
			};
			// Median of several samples across a span, not one pixel: a single sample can
			// be stood up by a sliver of colour (a clip-path exposing just that point)
			// while the bar reads as empty, and it is more exposed to antialiasing.
			const median = (values: number[]) => values.slice().sort((a, b) => a - b)[values.length >> 1];
			const at = (fromFraction: number, toFraction: number, cssY: number): Rgb => {
				const samples = [0, 0.5, 1].map((step) => {
					const fraction = fromFraction + (toFraction - fromFraction) * step;
					return pixel(barBox.x + barBox.width * fraction, cssY);
				});
				return {
					r: median(samples.map((s) => s.r)),
					g: median(samples.map((s) => s.g)),
					b: median(samples.map((s) => s.b)),
				};
			};

			const midY = barBox.y + barBox.height / 2;
			// 40% used: span well inside the fill and well inside the remainder, clear of
			// the rounded caps and their antialiasing.
			const fill = at(0.1, 0.3, midY);
			const track = at(0.6, 0.9, midY);
			// The surface's own padding, level with the bar: background only, no glyphs.
			const backdrop = pixel(chipBox.x + 3, midY);

			const ratio = contrast(fill, track);
			if (ratio < MIN_RATIO) {
				failures.push(
					`${scope}: fill ${describe(fill)} on track ${describe(track)} is ${ratio.toFixed(2)}:1, below ${MIN_RATIO}:1`,
				);
			}

			// Checked separately, because contrast cannot see it: an unpainted track
			// simply shows the card behind it, so the fill still contrasts and the ratio
			// still passes while the groove has silently stopped being drawn. That is
			// precisely how the deletion of --color-quota-track went unnoticed.
			const trackDelta = channelDelta(track, backdrop);
			if (trackDelta < MIN_TRACK_DELTA) {
				failures.push(
					`${scope}: track ${describe(track)} is indistinguishable from the card ${describe(backdrop)} ` +
						`(${trackDelta} levels) — the groove is not being painted`,
				);
			}
		}
	}

	expect(failures, `quota bar contrast failures:\n${failures.join("\n")}`).toEqual([]);
});
