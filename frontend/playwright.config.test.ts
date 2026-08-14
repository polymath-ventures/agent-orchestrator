import { describe, expect, it } from "vitest";
import { resolveE2EPort } from "./playwright.config";

describe("resolveE2EPort", () => {
	it("uses a valid override so browser guards can coexist with the deployed supervisor", () => {
		expect(resolveE2EPort({ AO_E2E_PORT: "5174" })).toBe(5174);
	});

	it("defaults to the CI port and rejects invalid overrides", () => {
		expect(resolveE2EPort({})).toBe(5173);
		expect(resolveE2EPort({ AO_E2E_PORT: "not-a-port" })).toBe(5173);
		expect(resolveE2EPort({ AO_E2E_PORT: "70000" })).toBe(5173);
	});
});
