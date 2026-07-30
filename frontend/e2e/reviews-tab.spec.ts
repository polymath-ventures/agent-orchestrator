import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

const activeElementClass = (page: import("@playwright/test").Page) =>
	page.evaluate(() => document.activeElement?.className ?? "");

test.beforeEach(async ({ page }) => {
	await installBrowserModeApiFixtures(page);
});

test("the Reviews tab renders the reviewer panel for a session that owns PRs", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open auth stack" }).click();
	await expect(page).toHaveURL(/sessions\/stacked-auth/);
	await page.getByRole("button", { name: "Open inspector panel" }).click();

	const inspector = page.locator("#inspector");
	await expect(inspector).toBeVisible();

	await inspector.getByRole("tab", { name: "Reviews" }).click();

	// The reviewer card surfaces the harness, its approved verdict, and both
	// actions — never the empty state, since this session owns a PR. Upstream
	// renders the verdict as a badge inside the (default-open) per-PR review row
	// rather than an aggregate testid.
	await expect(inspector.getByText("No pull request opened yet.")).toHaveCount(0);
	const reviewsSection = inspector.getByTestId("inspector-section").filter({ hasText: "Reviews" });
	await expect(reviewsSection.getByText("codex")).toBeVisible();
	await expect(reviewsSection.getByText("Approved", { exact: true })).toBeVisible();
	await expect(reviewsSection.getByRole("button", { name: "Re-run review" })).toBeVisible();
	await expect(reviewsSection.getByRole("button", { name: "Open terminal" })).toBeVisible();
});

test("the Reviews tab shows the empty state for a session with no PRs", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open Split terminal mux responsibilities" }).click();
	await expect(page).toHaveURL(/sessions\/refactor-mux/);
	await page.getByRole("button", { name: "Open inspector panel" }).click();

	const inspector = page.locator("#inspector");
	await expect(inspector).toBeVisible();

	await inspector.getByRole("tab", { name: "Reviews" }).click();
	await expect(inspector.getByText("No pull request opened yet.")).toBeVisible();
});

test("reviewer terminal activation and back-to-agent activation focus the selected terminal", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open auth stack" }).click();
	await expect(page).toHaveURL(/sessions\/stacked-auth/);
	await page.getByRole("button", { name: "Open inspector panel" }).click();

	const inspector = page.locator("#inspector");
	await inspector.getByRole("tab", { name: "Reviews" }).click();
	await inspector.getByRole("button", { name: "Open terminal" }).click();

	await expect(page.getByRole("button", { name: "Back to agent terminal" })).toBeVisible();
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");

	await page.getByRole("button", { name: "Back to agent terminal" }).click();
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});
