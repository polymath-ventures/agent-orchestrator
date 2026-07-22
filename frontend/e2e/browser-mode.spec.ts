import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test("browser mode uses daemon-shaped data and hides desktop updater actions", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/");
	await expect(page.getByRole("button", { name: "Orchestrator board", exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Open fix-webgl-fallback" })).toBeVisible();

	await page.goto("/#/settings");
	await expect(page.getByText("Desktop updates are managed outside browser mode.")).toBeVisible();
	await expect(page.getByRole("button", { name: "Check for updates" })).toHaveCount(0);
});

test("browser mode opens a real mux websocket for terminal panes", async ({ page }) => {
	const fixtures = await installBrowserModeApiFixtures(page);
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await page.getByRole("button", { name: "Open inspector panel" }).click();
	await expect(page.getByRole("tab", { name: "Summary" })).toBeVisible();
	await expect.poll(() => fixtures.muxConnections).toBeGreaterThan(0);
});
