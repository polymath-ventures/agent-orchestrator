import { describe, expect, it } from "vitest";
import { buildIntake, intakeNeedsRule, type IntakeForm } from "./IntakeFields";

function form(overrides: Partial<IntakeForm> = {}): IntakeForm {
	return { enabled: false, provider: "", repo: "", assignee: "", optOutLabel: "", ...overrides };
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

	// provider, repo and optOutLabel have no UI input, so a value an operator set
	// through the CLI has to survive a settings save rather than be silently
	// wiped — wiping optOutLabel would quietly re-enable intake on opted-out
	// issues, and rewriting provider would point intake at the wrong tracker.
	it("preserves CLI-only fields through a save", () => {
		const saved = buildIntake(
			form({
				enabled: true,
				provider: "gitlab",
				assignee: "alice",
				repo: "acme/tracker",
				optOutLabel: "none",
			}),
		);
		expect(saved).toEqual({
			enabled: true,
			provider: "gitlab",
			repo: "acme/tracker",
			assignee: "alice",
			optOutLabel: "none",
		});
	});

	it("defaults an intake being enabled for the first time to github", () => {
		expect(buildIntake(form({ enabled: true, assignee: "alice" }))?.provider).toBe("github");
	});
});

describe("intakeNeedsRule", () => {
	it("requires an assignee only when intake is enabled", () => {
		expect(intakeNeedsRule(form({ enabled: true }))).toBe(true);
		expect(intakeNeedsRule(form({ enabled: true, assignee: "alice" }))).toBe(false);
		expect(intakeNeedsRule(form())).toBe(false);
	});
});
