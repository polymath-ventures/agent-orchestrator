import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test.beforeEach(async ({ page }) => {
	await installBrowserModeApiFixtures(page);
});

test("CI auto-injection policy is visible before a PR exists", async ({ page }) => {
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");

	const inspector = page.locator("#inspector");
	const toggle = inspector.getByRole("switch", { name: "Automatically send CI failures" });
	await expect(toggle).toBeVisible();
	await expect(toggle).toBeChecked();
	await expect(inspector.getByText("No pull request opened yet.")).toBeVisible();
});

test("a failing PR captured with injection disabled keeps its checks visible", async ({ page }) => {
	await page.goto("/#/projects/api-gateway/sessions/demo-ci-failed");

	const inspector = page.locator("#inspector");
	await expect(inspector.getByRole("switch", { name: "Automatically send CI failures" })).not.toBeChecked();
	await expect(inspector.getByText("CI failures not injected")).toBeVisible();
	await expect(inspector.getByText("renderer smoke", { exact: true })).toBeVisible();
});
