import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test.beforeEach(async ({ page }) => {
	await installBrowserModeApiFixtures(page);
});

test("the inspector rail stacks every PR a session owns, actionable-first", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open auth stack" }).click();
	await expect(page).toHaveURL(/sessions\/stacked-auth/);
	await page.getByRole("button", { name: "Open inspector panel" }).click();

	const inspector = page.locator("#inspector");
	await expect(inspector).toBeVisible();

	// Plural heading reflects the stack size.
	await expect(inspector.getByText("Pull requests (3)")).toBeVisible();

	// One card per PR, ordered open → draft → merged (the merged base sinks).
	// Upstream's PRSummaryCard drops the fork testid; each card leads with a
	// "PR #<n>" label, so assert those in order.
	const prSection = inspector.getByTestId("inspector-section").filter({ hasText: "Pull requests (3)" });
	await expect(prSection.getByText(/^PR #\d+$/)).toHaveText([/PR #41/, /PR #42/, /PR #40/]);
});
