import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test.beforeEach(async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.route("**/api/v1/agents", (route) =>
		route.fulfill({
			json: {
				supported: [
					{ id: "claude-code", label: "Claude Code", reviewerCapable: true },
					{ id: "codex", label: "Codex", reviewerCapable: true },
				],
				installed: [
					{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
					{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true },
				],
				authorized: [
					{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
					{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true },
				],
			},
		}),
	);
	await page.route("**/api/v1/agents/models**", (route) =>
		route.fulfill({
			json: {
				checkedAt: "2026-07-23T12:08:55Z",
				harnesses: [
					{
						id: "codex",
						label: "Codex",
						reviewerCapable: true,
						catalogSource: "adapter",
						catalogVerified: true,
						models: [
							{
								model: "gpt-5.5",
								label: "gpt-5.5",
								efforts: ["high"],
								defaultEffort: "high",
								verified: false,
								status: "unknown",
								reason: "not probed; only configured pins are live-validated",
								reasonCode: "not-probed",
							},
							{
								model: "retired-model",
								label: "Retired model",
								efforts: ["high"],
								defaultEffort: "high",
								verified: false,
								status: "unreachable",
								reason: "model rejected by provider",
							},
						],
					},
					{
						id: "claude-code",
						label: "Claude Code",
						reviewerCapable: true,
						catalogSource: "adapter",
						catalogVerified: true,
						models: [
							{
								model: "opus",
								label: "opus",
								efforts: ["high"],
								defaultEffort: "high",
								verified: false,
								status: "unknown",
								reason: "not probed; only configured pins are live-validated",
								reasonCode: "not-probed",
							},
						],
					},
				],
			},
		}),
	);
});

test("project setup hides advisory not-probed model status", async ({ page }) => {
	await page.goto("/");
	await page.getByLabel("New project").first().click();
	await page.getByRole("button", { name: "Project", exact: true }).click();
	await page.getByRole("textbox", { name: "Path" }).fill("/home/orchestrator/coachclaw");
	await page.getByRole("button", { name: "Continue" }).click();

	await expect(page.getByRole("dialog", { name: "Project harnesses" })).toBeVisible();
	await page.getByRole("combobox", { name: "Worker harness" }).click();
	await page.getByRole("option", { name: "Codex" }).click();
	await page.locator("#newProjectModel-codex-model").fill("gpt-5.5");

	await expect(page.getByText(/Status: unknown/)).toHaveCount(0);
	await expect(page.getByText(/not probed/)).toHaveCount(0);
	await expect(page.getByText(/Manual model IDs are allowed/).first()).toBeVisible();

	await page.locator("#newProjectModel-codex-model").fill("retired-model");
	await expect(page.getByText(/Status: unreachable/)).toBeVisible();
	await expect(page.getByText(/model rejected by provider/)).toBeVisible();
});
