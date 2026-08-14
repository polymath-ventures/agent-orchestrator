/**
 * Test-only resolver for the app's design-token colours.
 *
 * Guard tests need to answer "what colour does this Tailwind utility actually
 * paint, in this theme?" — not "does the element carry this class name?". A
 * class-name assertion passes happily while the token behind it is deleted or
 * quietly redefined, which is exactly how the quota meter's usage bar went
 * invisible (agent-orchestrator#289): an upstream sync deleted
 * `--color-quota-track` and redefined `--accent` as a near-background hover
 * surface, and every existing test stayed green.
 *
 * So this module walks the same path the browser does, statically:
 *
 *   `bg-foo`            → custom property `--color-foo`
 *   `bg-[var(--foo)]`   → custom property `--foo`
 *   `--color-foo`       → `tokens.css` if it defines it, else the `@theme`
 *                         bridge in `styles.css`, then follow `var()` onward
 *   value               → sRGB, composited over `--background` when translucent
 *
 * It is deliberately a small hand-rolled CSS reader rather than a PostCSS
 * dependency: `tokens.css` is a flat sheet of `selector { --prop: value }`
 * blocks with no at-rules, and the `@theme` block is a single flat block.
 */

import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

// Read the stylesheets off disk. Vitest stubs CSS imports (including `?raw`)
// to an empty string unless `css: true`, and an empty sheet would make every
// contrast assertion pass vacuously — the failure mode these guards exist to
// prevent. Anchor on the frontend package root by walking up from the working
// directory, so the suite runs the same from the repo root or from `frontend/`.
function frontendRoot(): string {
	let directory = process.cwd();
	for (;;) {
		if (existsSync(resolve(directory, "src/styles/tokens.css"))) return directory;
		const parent = dirname(directory);
		if (parent === directory) throw new Error(`Cannot locate src/styles/tokens.css above ${process.cwd()}`);
		directory = parent;
	}
}

const root = frontendRoot();
const tokensCss = readFileSync(resolve(root, "src/styles/tokens.css"), "utf8");
const stylesCss = readFileSync(resolve(root, "src/renderer/styles.css"), "utf8");

export type Rgb = { r: number; g: number; b: number; a: number };

type Block = { selectors: string[]; specificity: number; order: number; declarations: Map<string, string> };

/** A single resolvable theme: the root attributes that select it, plus a readable name. */
export type ThemeScope = { name: string; attributes: Record<string, string> };

function stripComments(css: string): string {
	return css.replace(/\/\*[\s\S]*?\*\//g, "");
}

/**
 * Specificity of the simple selectors used by the token sheet. Every scope
 * selector is built from `:root`, classes, and attribute selectors, so counting
 * those three is enough to order the cascade correctly (ids never appear).
 */
function specificityOf(selector: string): number {
	const pseudoClasses = selector.match(/:[a-z-]+(?:\([^)]*\))?/g)?.length ?? 0;
	const classes = selector.match(/\.[\w-]+/g)?.length ?? 0;
	const attributes = selector.match(/\[[^\]]+\]/g)?.length ?? 0;
	return pseudoClasses + classes + attributes;
}

function parseDeclarations(body: string): Map<string, string> {
	const declarations = new Map<string, string>();
	for (const declaration of body.split(";")) {
		const separator = declaration.indexOf(":");
		if (separator < 0) continue;
		const property = declaration.slice(0, separator).trim();
		if (!property.startsWith("--")) continue;
		declarations.set(property, declaration.slice(separator + 1).trim());
	}
	return declarations;
}

/** Parse the flat `selector { … }` blocks of the token sheet, in source order. */
function parseTokenSheet(css: string): Block[] {
	const blocks: Block[] = [];
	const rule = /([^{}]+)\{([^{}]*)\}/g;
	let match: RegExpExecArray | null;
	while ((match = rule.exec(css)) !== null) {
		const selectors = match[1]
			.split(",")
			.map((selector) => selector.trim())
			.filter(Boolean);
		if (selectors.length === 0) continue;
		const declarations = parseDeclarations(match[2]);
		if (declarations.size === 0) continue;
		blocks.push({
			selectors,
			specificity: Math.max(...selectors.map(specificityOf)),
			order: blocks.length,
			declarations,
		});
	}
	return blocks;
}

/** Pull the declarations out of the single `@theme` block that bridges `--color-*` to the raw tokens. */
function parseThemeBridge(css: string): Map<string, string> {
	const start = css.indexOf("@theme");
	if (start < 0) return new Map();
	const open = css.indexOf("{", start);
	if (open < 0) return new Map();
	let depth = 0;
	for (let i = open; i < css.length; i += 1) {
		if (css[i] === "{") depth += 1;
		else if (css[i] === "}") {
			depth -= 1;
			if (depth === 0) return parseDeclarations(css.slice(open + 1, i));
		}
	}
	return new Map();
}

const tokenBlocks = parseTokenSheet(stripComments(tokensCss));
const themeBridge = parseThemeBridge(stripComments(stylesCss));

/**
 * Every theme the token sheet can be switched into: the base dark/light pair
 * plus each named `data-style-theme` in both modes. Derived from the sheet, so
 * a theme added upstream is covered by the guards without touching this file.
 */
export function themeScopes(): ThemeScope[] {
	const named = [...new Set([...tokensCss.matchAll(/data-style-theme="([\w-]+)"/g)].map((match) => match[1]))];
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

/**
 * Which blocks apply to a scope, in cascade order. Selector matching is done by
 * jsdom against a throwaway document rather than by string comparison, so novel
 * scope selectors are handled the same way a browser would handle them.
 */
function applicableBlocks(scope: ThemeScope): Block[] {
	const doc = document.implementation.createHTMLDocument("theme-scope");
	for (const [attribute, value] of Object.entries(scope.attributes)) {
		doc.documentElement.setAttribute(attribute, value);
	}
	return tokenBlocks
		.filter((block) => block.selectors.some((selector) => doc.documentElement.matches(selector)))
		.sort((a, b) => a.specificity - b.specificity || a.order - b.order);
}

const scopeCache = new Map<string, Map<string, string>>();

function declarationsFor(scope: ThemeScope): Map<string, string> {
	const key = JSON.stringify(scope.attributes);
	const cached = scopeCache.get(key);
	if (cached) return cached;
	const resolved = new Map<string, string>();
	for (const block of applicableBlocks(scope)) {
		for (const [property, value] of block.declarations) resolved.set(property, value);
	}
	scopeCache.set(key, resolved);
	return resolved;
}

/**
 * Look a custom property up the way the cascade does. Tailwind hoists the
 * `@theme` block above the token sheet in the emitted stylesheet, so where both
 * declare a property the sheet's value is the one that lands — which is also
 * what keeps `--color-warning` out of a round trip through `--bridge-warning`.
 */
function lookup(property: string, scope: ThemeScope): string | undefined {
	return declarationsFor(scope).get(property) ?? themeBridge.get(property);
}

function srgbToLinear(channel: number): number {
	return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
}

function linearToSrgb(channel: number): number {
	const encoded = channel <= 0.0031308 ? 12.92 * channel : 1.055 * channel ** (1 / 2.4) - 0.055;
	return Math.min(1, Math.max(0, encoded));
}

function parseAlpha(raw: string | undefined): number {
	if (raw === undefined) return 1;
	const value = raw.trim();
	return value.endsWith("%") ? Number.parseFloat(value) / 100 : Number.parseFloat(value);
}

function parseHex(value: string): Rgb | null {
	const digits = value.slice(1);
	if (!/^[0-9a-f]{3,8}$/i.test(digits)) return null;
	const expanded = digits.length <= 4 ? [...digits].map((d) => d + d).join("") : digits;
	if (expanded.length !== 6 && expanded.length !== 8) return null;
	const channel = (index: number) => Number.parseInt(expanded.slice(index, index + 2), 16) / 255;
	return { r: channel(0), g: channel(2), b: channel(4), a: expanded.length === 8 ? channel(6) : 1 };
}

function parseOklch(value: string): Rgb | null {
	const match = /^oklch\(\s*([\d.]+%?)\s+([\d.]+%?)\s+([\d.]+)(?:deg)?\s*(?:\/\s*([\d.]+%?)\s*)?\)$/i.exec(value);
	if (!match) return null;
	const lightness = match[1].endsWith("%") ? Number.parseFloat(match[1]) / 100 : Number.parseFloat(match[1]);
	const chroma = match[2].endsWith("%") ? (Number.parseFloat(match[2]) / 100) * 0.4 : Number.parseFloat(match[2]);
	const hue = (Number.parseFloat(match[3]) * Math.PI) / 180;
	const a = chroma * Math.cos(hue);
	const b = chroma * Math.sin(hue);
	const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
	const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
	const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3;
	return {
		r: linearToSrgb(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
		g: linearToSrgb(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
		b: linearToSrgb(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
		a: parseAlpha(match[4]),
	};
}

function parseRgbFunction(value: string): Rgb | null {
	const match = /^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)\s*(?:[/,]\s*([\d.]+%?)\s*)?\)$/i.exec(value);
	if (!match) return null;
	return {
		r: Number.parseFloat(match[1]) / 255,
		g: Number.parseFloat(match[2]) / 255,
		b: Number.parseFloat(match[3]) / 255,
		a: parseAlpha(match[4]),
	};
}

function parseLiteralColor(value: string): Rgb | null {
	const literal = value.trim();
	if (literal === "transparent") return { r: 0, g: 0, b: 0, a: 0 };
	if (literal.startsWith("#")) return parseHex(literal);
	if (literal.startsWith("oklch(")) return parseOklch(literal);
	if (/^rgba?\(/i.test(literal)) return parseRgbFunction(literal);
	return null;
}

/** Resolve a value that may be a literal colour or a `var()` chain into sRGB. */
function resolveValue(value: string, scope: ThemeScope, seen: Set<string>): Rgb | null {
	const literal = parseLiteralColor(value);
	if (literal) return literal;
	const reference = /^var\(\s*(--[\w-]+)\s*(?:,([\s\S]+))?\)$/.exec(value.trim());
	if (!reference) return null;
	const property = reference[1];
	if (!seen.has(property)) {
		seen.add(property);
		const referenced = lookup(property, scope);
		if (referenced !== undefined) {
			const resolved = resolveValue(referenced, scope, seen);
			if (resolved) return resolved;
		}
	}
	return reference[2] ? resolveValue(reference[2], scope, seen) : null;
}

/** Composite a translucent colour over an opaque backdrop, in linear sRGB. */
function compositeOver(color: Rgb, backdrop: Rgb): Rgb {
	if (color.a >= 1) return color;
	const blend = (top: number, bottom: number) =>
		linearToSrgb(srgbToLinear(top) * color.a + srgbToLinear(bottom) * (1 - color.a));
	return { r: blend(color.r, backdrop.r), g: blend(color.g, backdrop.g), b: blend(color.b, backdrop.b), a: 1 };
}

/**
 * The declaration a Tailwind background utility compiles to.
 *
 * `bg-[var(--foo)]` is an arbitrary value: it paints `var(--foo)` and resolves
 * straight off the cascade. `bg-foo` is a theme utility, and because the app's
 * `@theme` block is declared `inline`, Tailwind substitutes that block's value
 * for `--color-foo` into the utility rather than emitting a reference to it —
 * `.bg-accent` compiles to `background-color: var(--accent)`, not
 * `var(--color-accent)`. Reading the bridge first is what makes this resolver
 * agree with the shipped stylesheet for every token the sheet also names.
 */
function backgroundDeclaration(className: string, scope: ThemeScope): string | undefined {
	const arbitrary = /^bg-\[var\((--[\w-]+)\)\]$/.exec(className);
	if (arbitrary) return lookup(arbitrary[1], scope);
	const named = /^bg-([a-z][\w-]*)$/.exec(className);
	if (!named) return undefined;
	const property = `--color-${named[1]}`;
	return themeBridge.get(property) ?? declarationsFor(scope).get(property);
}

/** Whether a class name is a background utility this resolver understands. */
export function isBackgroundUtility(className: string): boolean {
	return /^bg-\[var\(--[\w-]+\)\]$/.test(className) || /^bg-[a-z][\w-]*$/.test(className);
}

/** The single `bg-*` utility on an element, or null when it carries none. */
export function backgroundClass(element: Element): string | null {
	return [...element.classList].find(isBackgroundUtility) ?? null;
}

/**
 * The opaque colour a Tailwind background utility paints in a theme, or null
 * when the custom property behind it resolves to nothing — which is what a
 * deleted token looks like from here.
 */
export function resolveBackgroundColor(className: string, scope: ThemeScope): Rgb | null {
	const declared = backgroundDeclaration(className, scope);
	if (declared === undefined) return null;
	const color = resolveValue(declared, scope, new Set());
	if (!color) return null;
	if (color.a >= 1) return color;
	const backdropDeclaration = lookup("--background", scope);
	const backdrop = backdropDeclaration ? resolveValue(backdropDeclaration, scope, new Set(["--background"])) : null;
	return compositeOver(color, backdrop ?? { r: 1, g: 1, b: 1, a: 1 });
}

export function relativeLuminance({ r, g, b }: Rgb): number {
	return 0.2126 * srgbToLinear(r) + 0.7152 * srgbToLinear(g) + 0.0722 * srgbToLinear(b);
}

/** WCAG 2.x contrast ratio, 1–21. */
export function contrastRatio(a: Rgb, b: Rgb): number {
	const [lighter, darker] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x);
	return (lighter + 0.05) / (darker + 0.05);
}

export function formatRgb({ r, g, b }: Rgb): string {
	const hex = (channel: number) =>
		Math.round(channel * 255)
			.toString(16)
			.padStart(2, "0");
	return `#${hex(r)}${hex(g)}${hex(b)}`;
}
