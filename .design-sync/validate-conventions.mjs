// Checks every utility class and component name asserted in conventions.md
// against the built bundle.
//
// conventions.md is inlined into the design agent's system prompt, so a name
// that does not exist is worse than no guidance at all: the agent trusts it,
// writes vocabulary that never resolves, and ships silently unstyled output.
// This shipped wrong twice by hand — `text-base` (defined in @theme but never
// emitted, since Tailwind is JIT) and `text-terminal` (a *background* token
// exposed as a text utility, i.e. near-black text on a near-black surface).
//
// Usage, after a build:  node .design-sync/validate-conventions.mjs ./ds-bundle
import { readFileSync, existsSync, readdirSync } from "node:fs";
import { join } from "node:path";

const OUT = process.argv[2] ?? "./ds-bundle";
const CONVENTIONS = ".design-sync/conventions.md";

for (const p of [CONVENTIONS, join(OUT, "_ds_bundle.css"), join(OUT, "_ds_bundle.js")]) {
	if (!existsSync(p)) {
		console.error(`✗ missing ${p} — build first`);
		process.exit(1);
	}
}

const md = readFileSync(CONVENTIONS, "utf8");
const css = readFileSync(join(OUT, "_ds_bundle.css"), "utf8");
const bundle = readFileSync(join(OUT, "_ds_bundle.js"), "utf8");

// Names the file deliberately cites as NOT part of this system.
const COUNTEREXAMPLES = new Set(["bg-slate-800", "text-gray-400", "text-[13.5px]"]);

// A class is present if it appears as a selector. Tailwind escapes `:` and `/`
// in generated selectors, so `hover:bg-x` is emitted as `.hover\:bg-x`.
const selectorPresent = (cls) => {
	const escaped = cls.replace(/[:/]/g, (c) => "\\" + c);
	return ["{", ",", ":", " ", ">"].some((suffix) => css.includes("." + escaped + suffix));
};

const utilityRx = /`((?:[a-z-]+:)?(?:bg|text|border|ring|rounded|font|fill|stroke)-[A-Za-z0-9[\]./-]+)`/g;
const claimedUtilities = [...new Set([...md.matchAll(utilityRx)].map((m) => m[1]))].filter(
	(c) => !COUNTEREXAMPLES.has(c),
);

const componentDirs = new Set(
	existsSync(join(OUT, "components"))
		? readdirSync(join(OUT, "components")).flatMap((g) => readdirSync(join(OUT, "components", g)))
		: [],
);
const claimedComponents = [...new Set([...md.matchAll(/`([A-Z][A-Za-z]+)`/g)].map((m) => m[1]))];

const missingUtilities = claimedUtilities.filter((c) => !selectorPresent(c));
// A component counts as real if it has an emitted card directory or is an
// export in the bundle (providers ship in the bundle without a card).
const missingComponents = claimedComponents.filter((c) => !componentDirs.has(c) && !bundle.includes(c));

// A colour utility whose declaration disagrees with its family is a trap:
// e.g. a `text-*` utility resolving to a *background* token.
const mismatched = [];
for (const cls of claimedUtilities) {
	const bare = cls.replace(/^[a-z-]+:/, "");
	const rule = css.match(new RegExp("\\." + bare.replace(/[:/]/g, (c) => "\\\\" + c) + "\\{([^}]*)\\}"));
	if (!rule) continue;
	if (bare.startsWith("text-") && /color:var\(--color-bg-/.test(rule[1])) {
		mismatched.push(`${cls} → ${rule[1]} (a background token used as a text colour)`);
	}
	if (bare.startsWith("bg-") && /background-color:var\(--color-text-/.test(rule[1])) {
		mismatched.push(`${cls} → ${rule[1]} (a text token used as a background)`);
	}
}

console.log(`checked ${claimedUtilities.length} utilities, ${claimedComponents.length} component names`);
let failed = false;
for (const [label, list] of [
	["not emitted in the compiled CSS", missingUtilities],
	["not a component in this design system", missingComponents],
	["resolves to the wrong kind of token", mismatched],
]) {
	if (list.length) {
		failed = true;
		console.error(`✗ ${label}:\n  ${list.join("\n  ")}`);
	}
}
if (failed) {
	console.error("\nconventions.md names things that do not exist — fix the name or cut the claim.");
	process.exit(1);
}
console.log("✓ every name in conventions.md verifies against the build");
