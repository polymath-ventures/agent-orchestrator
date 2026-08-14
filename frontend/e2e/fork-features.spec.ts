import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test.beforeEach(async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	const agents = [
		{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
		{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true },
		{ id: "codex-fugu", label: "Codex Fugu", authStatus: "authorized", reviewerCapable: true },
	];
	await page.route("**/api/v1/agents", (route) =>
		route.fulfill({ json: { supported: agents, installed: agents, authorized: agents } }),
	);
	await page.route("**/api/v1/agents/models**", (route) =>
		route.fulfill({ json: { checkedAt: "2026-08-14T00:00:00Z", harnesses: [] } }),
	);
	await page.route("**/api/v1/fleet", (route) => route.fulfill({ json: { paused: false } }));
	await page.route("**/api/v1/prime/settings", (route) =>
		route.fulfill({
			json: {
				settings: {
					enabled: true,
					displayName: "AO Prime",
					agent: "codex",
					agentConfig: {},
					rules: "",
					rulesFile: "",
					wakeInterval: "15m",
				},
			},
		}),
	);
	await page.route("**/api/v1/projects/api-gateway", (route) =>
		route.fulfill({
			json: {
				status: "ok",
				project: {
					id: "api-gateway",
					kind: "git",
					name: "api-gateway",
					path: "/Users/me/api-gateway",
					repo: "me/api-gateway",
					defaultBranch: "main",
					sessionPrefix: "ao",
					config: {
						orchestrator: { agent: "codex" },
						worker: { agent: "codex" },
						reviewers: [{ harness: "codex-fugu" }],
						workerMix: [{ agent: "codex", model: "gpt-5.5", effort: "high", weight: 100 }],
						workerTaskPrompt: "/address-issue {issue}",
					},
				},
			},
		}),
	);
});

test("fork UI features stay mounted from the application shell", async ({ page }) => {
	await page.goto("/");

	const workerRow = page.getByRole("button", { name: "Open fix-webgl-fallback" });
	await expect(workerRow.locator("[data-harness-glyph]")).toHaveAttribute("title", "Codex");
	await expect(workerRow).toHaveAttribute("aria-describedby", /sidebar-harness-/);

	await page.goto("/#/settings");
	const globalSettings = page.getByRole("dialog");
	await expect(globalSettings.getByText("Fleet", { exact: true })).toBeVisible();
	await expect(globalSettings.getByText("Prime", { exact: true })).toBeVisible();

	await page.goto("/#/projects/api-gateway/settings");
	const projectSettings = page.getByRole("dialog");
	await expect(projectSettings).toBeVisible();

	await projectSettings.getByRole("button", { name: "Workflow", exact: true }).click();
	const reviewer = projectSettings.getByRole("button", { name: "Default reviewer agent" });
	await reviewer.click();
	await expect(page.getByRole("menuitem", { name: "Codex Fugu" })).toBeVisible();
	await page.keyboard.press("Escape");

	await projectSettings.getByRole("button", { name: "Instructions", exact: true }).click();
	await expect(projectSettings.getByRole("textbox", { name: "Worker task prompt template" })).toHaveValue(
		"/address-issue {issue}",
	);

	await projectSettings.getByRole("button", { name: "Workers", exact: true }).click();
	await expect(projectSettings.getByText("Bucket 1", { exact: true })).toBeVisible();
	await expect(projectSettings.getByRole("spinbutton", { name: "Weight" })).toHaveValue("100");
});
