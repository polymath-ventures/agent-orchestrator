import { describe, expect, it } from "vitest";
import { buildIntake, intakeNeedsRule, type IntakeForm } from "./IntakeFields";

function form(overrides: Partial<IntakeForm> = {}): IntakeForm {
	return { enabled: false, repo: "", assignee: "", optOutLabel: "", ...overrides };
}

describe("buildIntake", () => {
	it("omits an entirely blank intake", () => {
		expect(buildIntake(form())).toBeUndefined();
	});

	it("emits the enabled intake fields", () => {
		expect(buildIntake(form({ enabled: true, assignee: "alice" }))).toEqual({
			enabled: true,
			provider: "github",
			repo: undefined,
			assignee: "alice",
			optOutLabel: undefined,
		});
	});

	// repo and optOutLabel have no UI input, so a value an operator set through
	// the CLI has to survive a settings save rather than be silently wiped —
	// wiping optOutLabel would quietly re-enable intake on opted-out issues.
	it("preserves CLI-only fields through a save", () => {
		expect(
			buildIntake(
				form({ enabled: true, assignee: "alice", repo: "acme/tracker", optOutLabel: "none" }),
			),
		).toEqual({
			enabled: true,
			provider: "github",
			repo: "acme/tracker",
			assignee: "alice",
			optOutLabel: "none",
		});
	});
});

describe("intakeNeedsRule", () => {
	it("requires an assignee only when intake is enabled", () => {
		expect(intakeNeedsRule(form({ enabled: true }))).toBe(true);
		expect(intakeNeedsRule(form({ enabled: true, assignee: "alice" }))).toBe(false);
		expect(intakeNeedsRule(form())).toBe(false);
	});
});
