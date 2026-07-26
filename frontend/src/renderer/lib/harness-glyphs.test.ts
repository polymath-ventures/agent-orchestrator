import { describe, expect, it } from "vitest";
import { AGENT_OPTIONS } from "./agent-options";
import { NEUTRAL_HARNESS_TILE, getHarnessGlyphView } from "./harness-glyphs";
import type { AgentProvider } from "../types/workspace";

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
		const providers: AgentProvider[] = [...AGENT_OPTIONS, "codex-fugu"];
		for (const provider of providers) {
			const view = getHarnessGlyphView(provider);
			expect(view.label, `${provider} needs a label`).not.toHaveLength(0);
			const renderable = view.paths.length > 0 || Boolean(view.monogram);
			expect(renderable, `${provider} renders neither a mark nor a monogram`).toBe(true);
		}
	});

	it("never returns an empty indicator for an empty or missing harness", () => {
		for (const provider of ["", undefined]) {
			const view = getHarnessGlyphView(provider as string);
			expect(view.tile).toBe(NEUTRAL_HARNESS_TILE);
			expect(view.monogram).not.toHaveLength(0);
			expect(view.label).not.toHaveLength(0);
		}
	});
});
