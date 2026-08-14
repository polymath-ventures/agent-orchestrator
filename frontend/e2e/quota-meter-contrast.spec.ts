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
	// The chip is the nearest ancestor painting an opaque background, so its
	// screenshot contains the bar plus the surface the bar sits on.
	const chip = bar.locator("xpath=ancestor::*[contains(@class,'bg-background')][1]");
	await expect(chip).toBeVisible();

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
			const chipBox = await chip.boundingBox();
			const barBox = await bar.boundingBox();
			if (!chipBox || !barBox) {
				failures.push(`${scope}: the bar or its chip has no layout box`);
				continue;
			}

			const png = PNG.sync.read(await chip.screenshot());
			const scale = png.width / chipBox.width;
			const at = (cssX: number, cssY: number): Rgb => {
				const x = Math.min(png.width - 1, Math.max(0, Math.round((cssX - chipBox.x) * scale)));
				const y = Math.min(png.height - 1, Math.max(0, Math.round((cssY - chipBox.y) * scale)));
				const i = (png.width * y + x) << 2;
				return { r: png.data[i], g: png.data[i + 1], b: png.data[i + 2] };
			};

			const midY = barBox.y + barBox.height / 2;
			// 40% used: sample well inside the fill and well inside the remainder, away
			// from the rounded caps and their antialiasing.
			const fill = at(barBox.x + barBox.width * 0.15, midY);
			const track = at(barBox.x + barBox.width * 0.85, midY);
			// The chip's own padding, level with the bar: background only, no glyphs.
			const backdrop = at(chipBox.x + 3, midY);

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
