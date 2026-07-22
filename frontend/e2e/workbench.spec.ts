import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test.beforeEach(async ({ page }) => {
	await installBrowserModeApiFixtures(page);
});

test("renders the orchestrator-first workbench shell", async ({ page }) => {
	await page.goto("/");
	// The single pinned Orchestrator anchor + the Projects group + a name-only worker row.
	await expect(page.getByRole("button", { name: "Orchestrator board", exact: true })).toBeVisible();
	await expect(page.getByText("Projects")).toBeVisible();
	await expect(page.getByRole("button", { name: "Open fix-webgl-fallback" })).toBeVisible();
	await expect(page.getByText("Board", { exact: true })).toBeVisible();
});

test("deep-links into a worker session", async ({ page }) => {
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	// Worker view = emdash three-pane with the Git review rail.
	await page.getByRole("button", { name: "Open inspector panel" }).click();
	await expect(page.getByRole("tab", { name: "Summary" })).toBeVisible();
	await expect(page.getByTestId("terminal").getByText("Split terminal mux responsibilities")).toBeVisible();
});

test("drilling into a worker opens its Git review rail", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open Split terminal mux responsibilities" }).click();
	await page.getByRole("button", { name: "Open inspector panel" }).click();
	await page.getByRole("tab", { name: "Files" }).click();
	await expect(page.getByText("internal/mux/terminal_mux.go")).toBeVisible();
});
