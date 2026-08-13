import { describe, expect, it } from "vitest";
import { KNOWN_REVIEWER_HARNESS_IDS, toReviewerHarnessId } from "./reviewer-harnesses";

describe("reviewer harness vocabulary", () => {
	it("preserves the fork-only codex-fugu reviewer", () => {
		expect(KNOWN_REVIEWER_HARNESS_IDS.has("codex-fugu")).toBe(true);
		expect(toReviewerHarnessId("codex-fugu")).toBe("codex-fugu");
	});
});
