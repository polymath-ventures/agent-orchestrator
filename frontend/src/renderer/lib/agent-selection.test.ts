import { describe, expect, it } from "vitest";
import type { AgentModelAvailabilityResponse } from "../hooks/useModelAvailabilityQuery";
import { effectiveDisplayHarness } from "./agent-selection";

function catalogEntry(id: string, label: string) {
	return {
		id,
		label,
		reviewerCapable: false,
		catalogSource: "adapter" as const,
		catalogVerified: true,
		models: [],
	};
}

// The polled /api/v1/agents/models response (already degraded per-harness by the
// daemon). This — not the raw agent inventory — is what the display fallback keys
// off, because a missing-binary harness lingers in the inventory but is omitted
// here.
const availability: AgentModelAvailabilityResponse = {
	checkedAt: "2026-08-01T00:00:00Z",
	harnesses: [catalogEntry("claude-code", "Claude Code"), catalogEntry("codex", "Codex")],
};

describe("effectiveDisplayHarness", () => {
	it("returns the configured harness unchanged when the polled catalog still carries it", () => {
		expect(effectiveDisplayHarness("codex", availability)).toBe("codex");
	});

	it("falls back to claude-code when the harness was omitted from the degraded catalog even though the inventory would still list it", () => {
		// This is the real opencode case: its binary is gone, so the daemon omits
		// it from /agents/models, but it is still in the inventory's `supported` set.
		// Keying off availability is what actually catches the degradation.
		expect(effectiveDisplayHarness("opencode", availability)).toBe("claude-code");
	});

	it("leaves an empty configured harness empty (the automatic/default sentinel)", () => {
		expect(effectiveDisplayHarness("", availability)).toBe("");
	});

	it("does not fall back while the catalog is still unpolled (undefined)", () => {
		expect(effectiveDisplayHarness("opencode", undefined)).toBe("opencode");
	});
});
