import { describe, expect, it } from "vitest";
import { NEUTRAL_HARNESS_TILE, getHarnessGlyphView } from "./harness-glyphs";
import type { AgentProvider } from "../types/workspace";

const GLYPH_TEST_PROVIDER_MAP = {
	"claude-code": true,
	codex: true,
	"codex-fugu": true,
	aider: true,
	opencode: true,
	grok: true,
	droid: true,
	amp: true,
	agy: true,
	crush: true,
	cursor: true,
	qwen: true,
	copilot: true,
	goose: true,
	auggie: true,
	continue: true,
	devin: true,
	cline: true,
	kimi: true,
	kiro: true,
	kilocode: true,
	vibe: true,
	pi: true,
	autohand: true,
	muse: true,
	fake: true,
} satisfies Record<AgentProvider, true>;

const GLYPH_TEST_PROVIDERS = Object.keys(GLYPH_TEST_PROVIDER_MAP) as AgentProvider[];

// The sidebar indicator must resolve *something* renderable for every harness
// the daemon can spawn, including ones added after this table was written.
// These cover the resolver only; the chip itself is covered in
// HarnessGlyph.test.tsx.
describe("getHarnessGlyphView", () => {
	it("gives Claude Code its brand tile and a knockout mark", () => {
		const view = getHarnessGlyphView("claude-code");
		expect(view.label).toBe("Claude Code");
		expect(view.tile).toBe("#D97757");
		expect(view.paths.length).toBeGreaterThan(0);
		expect(view.monogram).toBeUndefined();
	});

	it("gives Codex its brand gradient rather than a flat tile", () => {
		const view = getHarnessGlyphView("codex");
		expect(view.label).toBe("Codex");
		// The approved treatment is the published Codex gradient, not the native
		// white app tile (which glares on dark and vanishes on light).
		expect(view.tile).toContain("#B1A7FF");
		expect(view.tile).toContain("#7A9DFF");
		expect(view.tile).toContain("#3941FF");
		expect(view.paths.length).toBeGreaterThan(0);
		expect(view.pip).toBeUndefined();
	});

	it("distinguishes codex-fugu from codex with a pip, not a different mark", () => {
		const codex = getHarnessGlyphView("codex");
		const fugu = getHarnessGlyphView("codex-fugu");
		expect(fugu.paths).toEqual(codex.paths);
		expect(fugu.tile).toBe(codex.tile);
		expect(fugu.pip).toBe("#22D3EE");
		expect(fugu.label).not.toBe(codex.label);
	});

	it("falls back to a neutral monogram for a harness with no published mark", () => {
		const view = getHarnessGlyphView("aider");
		expect(view.label).toBe("Aider");
		expect(view.tile).toBe(NEUTRAL_HARNESS_TILE);
		expect(view.paths).toHaveLength(0);
		expect(view.monogram).toBe("Ai");
	});

	it("gives the neutral fallback distinct monograms for similarly spelled harnesses", () => {
		expect(getHarnessGlyphView("auggie").monogram).not.toBe(getHarnessGlyphView("autohand").monogram);
	});

	it("resolves an unrecognised harness id to the neutral fallback", () => {
		const view = getHarnessGlyphView("brand-new-agent");
		expect(view.tile).toBe(NEUTRAL_HARNESS_TILE);
		expect(view.paths).toHaveLength(0);
		expect(view.monogram).toBe("BN");
		expect(view.label).toBe("Brand New Agent");
	});

	it("resolves a single-word unrecognised harness id", () => {
		const view = getHarnessGlyphView("wombat");
		expect(view.monogram).toBe("Wo");
		expect(view.label).toBe("Wombat");
	});

	it("renders something for every harness the client knows about", () => {
		for (const provider of GLYPH_TEST_PROVIDERS) {
			const view = getHarnessGlyphView(provider);
			expect(view.label, `${provider} needs a label`).not.toHaveLength(0);
			const renderable = view.paths.length > 0 || Boolean(view.monogram);
			expect(renderable, `${provider} renders neither a mark nor a monogram`).toBe(true);
		}
	});

	it("never returns an empty indicator for an empty or missing harness", () => {
		for (const provider of ["", undefined, null]) {
			const view = getHarnessGlyphView(provider);
			expect(view.tile).toBe(NEUTRAL_HARNESS_TILE);
			expect(view.monogram).not.toHaveLength(0);
			expect(view.label).not.toHaveLength(0);
		}
	});

	// A plain-object lookup table answers for every key on Object.prototype, so
	// "constructor" would resolve to a truthy non-entry and strip the label the
	// indicator's accessible name depends on.
	it("does not resolve inherited Object.prototype keys as harnesses", () => {
		for (const key of ["constructor", "toString", "hasOwnProperty", "__proto__", "valueOf"]) {
			const view = getHarnessGlyphView(key);
			expect(view.label, `${key} must not leak a prototype member`).not.toHaveLength(0);
			expect(view.tile).toBe(NEUTRAL_HARNESS_TILE);
			expect(view.monogram).not.toHaveLength(0);
			expect(view.paths).toHaveLength(0);
		}
	});
});
