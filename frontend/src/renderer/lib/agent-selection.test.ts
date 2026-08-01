import { describe, expect, it } from "vitest";
import { type AgentInventory, effectiveDisplayHarness } from "./agent-selection";

const catalog: AgentInventory = {
	supported: [
		{ id: "claude-code", label: "Claude Code", reviewerCapable: true },
		{ id: "codex", label: "Codex", reviewerCapable: true },
	],
	installed: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
	authorized: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
};

describe("effectiveDisplayHarness", () => {
	it("returns the configured harness unchanged when it exists anywhere in the catalog", () => {
		expect(effectiveDisplayHarness("codex", catalog)).toBe("codex");
	});

	it("treats a supported-only harness as present and leaves it displayed", () => {
		// claude-code is only in `supported`, not installed/authorized — still present.
		expect(effectiveDisplayHarness("claude-code", catalog)).toBe("claude-code");
	});

	it("falls back to claude-code when the configured harness is absent from every catalog list", () => {
		expect(effectiveDisplayHarness("ghost-harness", catalog)).toBe("claude-code");
	});

	it("leaves an empty configured harness empty (the automatic/default sentinel)", () => {
		expect(effectiveDisplayHarness("", catalog)).toBe("");
	});

	it("does not fall back while the catalog is still unpolled (undefined)", () => {
		expect(effectiveDisplayHarness("codex", undefined)).toBe("codex");
	});
});
