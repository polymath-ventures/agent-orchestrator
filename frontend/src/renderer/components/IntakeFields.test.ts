import { describe, expect, it } from "vitest";
import { buildIntake, deriveIntakeRepo, intakeNeedsRule, type IntakeForm } from "./IntakeFields";

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

	// Pausing intake must not lose the tracker it was configured against, or
	// re-enabling it later would quietly point a GitLab project at GitHub.
	it("keeps a configured provider while intake is disabled", () => {
		expect(buildIntake(form({ enabled: false, provider: "gitlab", assignee: "alice" }))?.provider).toBe("gitlab");
		expect(buildIntake(form({ enabled: false, assignee: "alice" }))?.provider).toBeUndefined();
	});
});

describe("intakeNeedsRule", () => {
	it("requires an assignee only when intake is enabled", () => {
		expect(intakeNeedsRule(form({ enabled: true }))).toBe(true);
		expect(intakeNeedsRule(form({ enabled: true, assignee: "alice" }))).toBe(false);
		expect(intakeNeedsRule(form())).toBe(false);
	});
});

describe("deriveIntakeRepo", () => {
	it("keeps a GitHub repo at owner/repo and links to its own host", () => {
		expect(deriveIntakeRepo("https://github.com/acme/demo.git", "github")).toEqual({
			path: "acme/demo",
			url: "https://github.com/acme/demo",
		});
		expect(deriveIntakeRepo("git@github.com:acme/demo.git", "github")).toEqual({
			path: "acme/demo",
			url: "https://github.com/acme/demo",
		});
	});

	// Truncating a GitLab namespace names a different project, and the link has
	// to go to the project's own instance rather than github.com.
	it("keeps a GitLab namespace whole and links to its instance", () => {
		expect(deriveIntakeRepo("https://gitlab.internal/group/sub/proj.git", "gitlab")).toEqual({
			path: "group/sub/proj",
			url: "https://gitlab.internal/group/sub/proj",
		});
	});

	it("returns nothing for an origin it cannot read", () => {
		expect(deriveIntakeRepo(undefined)).toBeUndefined();
		expect(deriveIntakeRepo("   ")).toBeUndefined();
		expect(deriveIntakeRepo("https://github.com/acme")).toBeUndefined();
	});
});
