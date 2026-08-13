import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

const activeElementClass = (page: import("@playwright/test").Page) =>
	page.evaluate(() => document.activeElement?.className ?? "");

test.beforeEach(async ({ page }) => {
	await installBrowserModeApiFixtures(page);
});

test("the Summary tab renders the reviewer panel for a session that owns PRs", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open auth stack" }).click();
	await expect(page).toHaveURL(/sessions\/stacked-auth/);

	// Reviews now live in Summary beside the PR they describe rather than in a
	// separate inspector tab.
	const inspector = page.locator("#inspector");
	await expect(inspector).toBeVisible();
	await expect(inspector.getByRole("tab", { name: "Summary" })).toHaveAttribute("aria-selected", "true");

	// The reviewer card surfaces the harness, its approved verdict, and both
	// actions — never the empty state, since this session owns a PR. Upstream
	// renders the verdict as a badge inside the (default-open) per-PR review row
	// rather than an aggregate testid.
	await expect(inspector.getByText("No pull request opened yet.")).toHaveCount(0);
	const reviewsSection = inspector.getByTestId("inspector-section").filter({ hasText: "Reviews" });
	await expect(reviewsSection.getByText("Approved", { exact: true })).toBeVisible();
	await expect(inspector.getByRole("button", { name: "Select reviewer agent" })).toContainText("codex");
	await expect(inspector.getByRole("button", { name: "Re-run review" })).toBeVisible();
	await expect(inspector.getByRole("button", { name: "Open terminal" })).toHaveCount(0);
});

test("the Summary tab shows the empty state for a session with no PRs", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open Split terminal mux responsibilities" }).click();
	await expect(page).toHaveURL(/sessions\/refactor-mux/);

	// Upstream's worker inspector rail defaults open, so it is already mounted.
	const inspector = page.locator("#inspector");
	await expect(inspector).toBeVisible();

	await expect(inspector.getByText("No pull request opened yet.")).toBeVisible();
	await expect(inspector.getByText("Reviews", { exact: true })).toHaveCount(0);
});

test("reviewer terminal activation and back-to-agent activation focus the selected terminal", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open auth stack" }).click();
	await expect(page).toHaveURL(/sessions\/stacked-auth/);

	// Upstream's worker inspector rail defaults open, so it is already mounted.
	const inspector = page.locator("#inspector");
	await inspector.getByRole("button", { name: "Open terminal" }).click();

	await expect(page.getByRole("button", { name: "Back to agent terminal" })).toBeVisible();
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");

	await page.getByRole("button", { name: "Back to agent terminal" }).click();
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});
