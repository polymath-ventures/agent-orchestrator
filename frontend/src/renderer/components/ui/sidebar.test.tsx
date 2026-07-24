import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { render, screen } from "@testing-library/react";
import type * as React from "react";
import { describe, expect, it } from "vitest";
import { SidebarMenuButton, SidebarProvider } from "./sidebar";

const here = path.dirname(fileURLToPath(import.meta.url));
const primitives = readdirSync(here)
	.filter((file) => file.endsWith(".tsx") && !file.endsWith(".test.tsx"))
	.sort();
const stylesheets = [
	readFileSync(path.resolve(here, "../../styles.css"), "utf8"),
	readFileSync(path.resolve(here, "../../../styles/tokens.css"), "utf8"),
];

/** Radix writes these onto the element itself at runtime; no stylesheet declares them. */
const RUNTIME_INJECTED = /^--radix-/;

/**
 * Custom properties that actually exist at runtime.
 *
 * `styles.css` maps the shadcn token names inside `@theme inline`, and `inline`
 * means Tailwind resolves each mapping into the utilities it generates rather
 * than emitting it as a `:root` custom property. So `--color-sidebar-border` is
 * a Tailwind *theme key* — real as `border-sidebar-border` or
 * `shadow-sidebar-border`, absent as a `var()` name. Only declarations outside
 * the `@theme inline` block survive as custom properties you can name.
 */
function declaredCustomProperties(css: string): Set<string> {
	const names = new Set<string>();
	let depth = 0;
	let themeInlineDepth: number | null = null;

	for (const line of css.split("\n")) {
		if (themeInlineDepth === null && /@theme\s+inline\s*\{/.test(line)) {
			themeInlineDepth = depth;
			depth += 1;
			continue;
		}

		const declaration = line.match(/^\s*(--[\w-]+)\s*:/);
		if (declaration && themeInlineDepth === null) {
			names.add(declaration[1]);
		}

		depth += (line.match(/\{/g)?.length ?? 0) - (line.match(/\}/g)?.length ?? 0);
		if (themeInlineDepth !== null && depth <= themeInlineDepth) {
			themeInlineDepth = null;
		}
	}

	return names;
}

/** `var(--x)` plus Tailwind's `w-(--x)` shorthand, which compiles to the same thing. */
function referencedCustomProperties(source: string): Set<string> {
	const names = new Set<string>();
	for (const [, name] of source.matchAll(/var\(\s*(--[\w-]+)/g)) {
		names.add(name);
	}
	for (const [, name] of source.matchAll(/-\((--[\w-]+)\)/g)) {
		names.add(name);
	}
	return names;
}

/** Properties the module sets itself, e.g. `style={{ "--sidebar-width": … }}`. */
function inlineSetCustomProperties(source: string): Set<string> {
	return new Set(Array.from(source.matchAll(/"(--[\w-]+)"\s*:/g), ([, name]) => name));
}

describe("ui primitive custom properties", () => {
	// #127: the sidebar outline variant named `var(--sidebar-border)` /
	// `var(--sidebar-accent)` — upstream shadcn's bare token names. This fork
	// remapped those to `--color-sidebar-*` under `@theme inline`, so neither
	// name resolves. A `box-shadow` whose `var()` has no value and no fallback
	// is invalid at computed-value time, which drops the whole declaration: no
	// ring rendered, no warning. Same class as #119 (Skeleton `bg-accent`).
	//
	// These primitives are vendored from shadcn, so every one of them is a place
	// the remapping can be half-applied. Check the whole directory rather than
	// the one file that happened to be reported.
	//
	// Deliberately a text scan, not a parse: it reads declarations without
	// tracking which selector they sit under, and does not strip comments. That
	// costs precision the fork does not currently need — every token here is
	// declared for all themes — and buys a guard small enough to keep honest.
	// If it ever needs selector awareness, it wants a real CSS parser instead.
	const declared = new Set(stylesheets.flatMap((css) => Array.from(declaredCustomProperties(css))));

	// `it.each([])` registers nothing and reports success, so an empty listing
	// would look exactly like a clean sweep.
	it("scans every vendored primitive", () => {
		expect(primitives.length).toBeGreaterThan(0);
		expect(primitives).toContain("sidebar.tsx");
	});

	it.each(primitives)("%s only names custom properties that resolve at runtime", (file) => {
		const source = readFileSync(path.join(here, file), "utf8");
		const inlineSet = inlineSetCustomProperties(source);

		const dangling = Array.from(referencedCustomProperties(source))
			.filter((name) => !declared.has(name) && !inlineSet.has(name) && !RUNTIME_INJECTED.test(name))
			.sort();

		expect(dangling).toEqual([]);
	});
});

describe("SidebarMenuButton", () => {
	function renderButton(props: React.ComponentProps<typeof SidebarMenuButton> = {}) {
		render(
			<SidebarProvider>
				<SidebarMenuButton data-testid="menu-button" {...props} />
			</SidebarProvider>,
		);
		return screen.getByTestId("menu-button");
	}

	it("draws the outline ring through the sidebar border token", () => {
		const button = renderButton({ variant: "outline" });

		// Geometry and colour are separate utilities: `shadow-[0_0_0_1px]` emits
		// `0 0 0 1px var(--tw-shadow-color, currentcolor)`, and the colour utility
		// fills that slot from the theme.
		expect(button).toHaveClass("shadow-[0_0_0_1px]", "shadow-sidebar-border", "hover:shadow-sidebar-accent");
	});

	it("leaves the default variant without a ring", () => {
		const button = renderButton();

		expect(button).not.toHaveClass("shadow-[0_0_0_1px]");
		expect(button).toHaveClass("hover:bg-sidebar-accent");
	});
});
