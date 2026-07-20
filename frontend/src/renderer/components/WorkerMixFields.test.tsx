import { describe, expect, it } from "vitest";
import { buildWorkerMix, parseMaxLiveWorkers, workerMixInvalid, workerMixTotal } from "./WorkerMixFields";

describe("WorkerMixFields parsing helpers", () => {
	it("reads exponent-notation weights by value, not parseInt truncation", () => {
		// A number input can legally hold "5e1"; parseInt would read 5 and silently
		// corrupt a mix that looks like it sums to 100.
		const buckets = [
			{ agent: "claude-code", model: "", weight: "5e1" }, // 50
			{ agent: "codex", model: "", weight: "50" },
		];
		expect(workerMixTotal(buckets)).toBe(100);
		expect(workerMixInvalid(buckets)).toBe(false);
		expect(buildWorkerMix(buckets)).toEqual([
			{ agent: "claude-code", weight: 50 },
			{ agent: "codex", weight: 50 },
		]);
	});

	it("treats non-integer weight input as 0", () => {
		expect(workerMixTotal([{ agent: "a", model: "", weight: "12.5" }])).toBe(0);
		expect(workerMixTotal([{ agent: "a", model: "", weight: "abc" }])).toBe(0);
		expect(workerMixTotal([{ agent: "a", model: "", weight: "" }])).toBe(0);
	});

	it("flags a mix whose real weights do not sum to 100", () => {
		expect(workerMixInvalid([{ agent: "a", model: "", weight: "5e1" }])).toBe(true); // 50 != 100
	});

	it("parses exponent-notation cap by value, not parseInt truncation", () => {
		expect(parseMaxLiveWorkers("1e2")).toBe(100); // parseInt would give 1
		expect(parseMaxLiveWorkers("8")).toBe(8);
	});

	it("treats blank, zero, and non-positive cap as unbounded (undefined)", () => {
		expect(parseMaxLiveWorkers("")).toBeUndefined();
		expect(parseMaxLiveWorkers("   ")).toBeUndefined();
		expect(parseMaxLiveWorkers("0")).toBeUndefined();
		expect(parseMaxLiveWorkers("-4")).toBeUndefined();
		expect(parseMaxLiveWorkers("2.5")).toBeUndefined();
	});
});
