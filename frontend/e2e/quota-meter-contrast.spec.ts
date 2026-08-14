/**
 * Contrast guard for the agent-harness quota/usage meter (fork feature 3).
 *
 * The mount guard (`src/renderer/test/shell-quota-widget-mount.test.tsx`) proves
 * the meter is on screen. This proves you can see the bar once it is.
 *
 * It exists because agent-orchestrator#289 was invisible to every test we had.
 * The 2026-08-07 upstream sync deleted the fork-only `--color-quota-track`, so
 * the groove painted a custom property defined nowhere; and it redefined
 * `--accent` — the normal-severity fill — as a hover surface holding the exact
 * value of `--muted`. Both casualties leave the class names untouched, so an
 * assertion on `bg-accent` passed throughout. Only the resolved colour catches
 * this, and the only thing that resolves CSS correctly is a browser: the
 * cascade, `@theme inline`, `var()` chains, `oklch()`, and alpha compositing all
 * have to agree with what ships. So this runs against the real built bundle,
 * reads `getComputedStyle`, and flattens the layers on a canvas — no second
 * implementation of CSS to drift from the one that ships.
 *
 * `QUOTA_METER_COLORS` is imported from the component, so a class swap there is
 * measured here rather than silently going unchecked; `QuotaPanel.test.tsx`
 * asserts the rendered meter really carries those utilities.
 *
 * Floor: WCAG 2.x non-text contrast, 3:1.
 */
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test } from "@playwright/test";
import { QUOTA_METER_COLORS } from "../src/renderer/components/quota-meter-colors";

/** WCAG 2.x minimum contrast for non-text user-interface components. */
const NON_TEXT_CONTRAST_FLOOR = 3;

type ThemeScope = { name: string; attributes: Record<string, string> };

/**
 * Every theme the token sheet can be switched into: the base dark/light pair
 * plus each named `data-style-theme` in both modes. Read out of the sheet, so a
 * theme added upstream is covered without touching this file.
 */
function themeScopes(testDir: string): ThemeScope[] {
	// Anchored on Playwright's rootDir (this spec's own `e2e/` directory) rather
	// than `import.meta.url`, which would flip the spec into ESM — a shape the
	// runner's transform does not load.
	const css = readFileSync(resolve(testDir, "../src/styles/tokens.css"), "utf8");
	const named = [...new Set([...css.matchAll(/data-style-theme="([\w-]+)"/g)].map((match) => match[1]))];
	const scopes: ThemeScope[] = [
		{ name: "base · dark", attributes: {} },
		{ name: "base · light", attributes: { "data-theme": "light" } },
	];
	for (const theme of named) {
		scopes.push({ name: `${theme} · dark`, attributes: { "data-style-theme": theme } });
		scopes.push({ name: `${theme} · light`, attributes: { "data-style-theme": theme, "data-theme": "light" } });
	}
	return scopes;
}

test("@T0 the quota meter's usage bar stays legible against its track in every theme", async ({ page }) => {
	const scopes = themeScopes(test.info().config.rootDir);
	// A regex that stopped matching would make every assertion below pass
	// vacuously, so pin the shape of what we enumerate before trusting it.
	expect(scopes.length, "expected the base pair plus every named theme in both modes").toBeGreaterThanOrEqual(18);
	expect(scopes.map((scope) => scope.name)).toContain("solarized · light");

	await page.goto("/");
	await page.waitForFunction(() => getComputedStyle(document.documentElement).getPropertyValue("--muted") !== "");

	const measurements = await page.evaluate(
		({ scopes, colors }) => {
			// One probe stack in the nesting the meter actually uses: the fill sits
			// inside the track, which sits on the chip's `bg-background` surface.
			const host = document.createElement("div");
			host.className = "bg-background";
			host.style.cssText = "position:fixed;left:-9999px;top:0;width:40px";
			const track = document.createElement("div");
			track.className = colors.track;
			track.style.cssText = "width:40px;height:20px";
			const fill = document.createElement("div");
			fill.style.cssText = "width:40px;height:20px";
			track.appendChild(fill);
			host.appendChild(track);
			document.body.appendChild(host);

			const canvas = document.createElement("canvas");
			canvas.width = canvas.height = 1;
			const ctx = canvas.getContext("2d", { willReadFrequently: true });
			if (!ctx) throw new Error("no 2d context");
			// Painting the layers onto a canvas in order is how the alpha gets
			// handled: `fillRect` composites source-over exactly as CSS backgrounds
			// do, so a translucent token blends with what is under it instead of
			// being read as if it were opaque. Computed values arrive in whatever
			// syntax the author wrote — Chromium preserves `oklch()` — and the
			// canvas normalises all of them. The white base only matters if the
			// page surface itself is translucent. A layer that paints nothing —
			// what an undefined custom property computes to, and half of #289 —
			// comes back null rather than as the colour beneath it.
			const flatten = (...layers: string[]): [number, number, number] | null => {
				ctx.clearRect(0, 0, 1, 1);
				for (const layer of ["#ffffff", ...layers]) {
					ctx.fillStyle = "#000000";
					ctx.fillStyle = layer;
					ctx.fillRect(0, 0, 1, 1);
				}
				const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
				const top = layers[layers.length - 1];
				return top === "rgba(0, 0, 0, 0)" || top === "transparent" ? null : [r, g, b];
			};

			const saved = {
				theme: document.documentElement.getAttribute("data-theme"),
				style: document.documentElement.getAttribute("data-style-theme"),
			};
			const results: Array<{ scope: string; colors: Record<string, [number, number, number] | null> }> = [];
			for (const scope of scopes) {
				document.documentElement.removeAttribute("data-theme");
				document.documentElement.removeAttribute("data-style-theme");
				for (const [attribute, value] of Object.entries(scope.attributes)) {
					document.documentElement.setAttribute(attribute, value);
				}
				const surface = getComputedStyle(host).backgroundColor;
				const trackCss = getComputedStyle(track).backgroundColor;
				const measured: Record<string, [number, number, number] | null> = {
					[colors.track]: flatten(surface, trackCss),
				};
				for (const utility of Object.values(colors.fill)) {
					fill.className = utility;
					measured[utility] = flatten(surface, trackCss, getComputedStyle(fill).backgroundColor);
				}
				fill.className = "";
				results.push({ scope: scope.name, colors: measured });
			}
			document.documentElement.removeAttribute("data-theme");
			document.documentElement.removeAttribute("data-style-theme");
			if (saved.theme) document.documentElement.setAttribute("data-theme", saved.theme);
			if (saved.style) document.documentElement.setAttribute("data-style-theme", saved.style);
			host.remove();
			return results;
		},
		{ scopes, colors: QUOTA_METER_COLORS as unknown as { track: string; fill: Record<string, string> } },
	);

	const luminance = ([r, g, b]: [number, number, number]) => {
		const channel = (value: number) => {
			const v = value / 255;
			return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
		};
		return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
	};
	const contrast = (a: [number, number, number], b: [number, number, number]) => {
		const [lighter, darker] = [luminance(a), luminance(b)].sort((x, y) => y - x);
		return (lighter + 0.05) / (darker + 0.05);
	};
	const hex = (rgb: [number, number, number]) => `#${rgb.map((v) => v.toString(16).padStart(2, "0")).join("")}`;

	const failures: string[] = [];
	for (const { scope, colors } of measurements) {
		const track = colors[QUOTA_METER_COLORS.track];
		if (!track) {
			failures.push(
				`${scope}: track \`${QUOTA_METER_COLORS.track}\` paints nothing — its custom property is undefined`,
			);
			continue;
		}
		for (const [severity, utility] of Object.entries(QUOTA_METER_COLORS.fill)) {
			const fill = colors[utility];
			if (!fill) {
				failures.push(`${scope}: ${severity} fill \`${utility}\` paints nothing — its custom property is undefined`);
				continue;
			}
			const ratio = contrast(fill, track);
			const detail = `${scope}: ${severity} ${ratio.toFixed(2)}:1 — fill ${hex(fill)} (${utility}) on track ${hex(track)}`;
			// Only normal severity carries the contrast floor. Warning and critical
			// are the severities that stayed visible through #289 and keep their
			// colours; they still have to resolve and to differ from the groove.
			// (`bg-warning` measures 2.59:1 in gruvbox light — noted on #289, out of
			// scope here.)
			if (severity === "normal" ? ratio < NON_TEXT_CONTRAST_FLOOR : ratio <= 1.5) failures.push(detail);
		}
	}

	expect(failures, `usage bar below the ${NON_TEXT_CONTRAST_FLOOR}:1 non-text floor:\n${failures.join("\n")}`).toEqual(
		[],
	);
});
