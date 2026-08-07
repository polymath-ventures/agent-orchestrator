import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test("browser mode uses daemon-shaped data and opens tabbed settings", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/");
	await expect(page.getByRole("button", { name: "Orchestrator board", exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Open fix-webgl-fallback" })).toBeVisible();

	await page.goto("/#/settings");
	const settings = page.getByRole("dialog");
	await expect(settings).toBeVisible();
	await settings.getByRole("button", { name: "Updates", exact: true }).click();
	await expect(settings.getByRole("button", { name: "Check for updates" })).toBeVisible();
});

test("browser mode opens a real mux websocket for terminal panes", async ({ page }) => {
	const fixtures = await installBrowserModeApiFixtures(page);
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	// Upstream's worker inspector rail defaults open, so its Summary tab is
	// already mounted; the terminal pane opens the mux socket regardless.
	await expect(page.getByRole("tab", { name: "Summary" })).toBeVisible();
	await expect.poll(() => fixtures.muxConnections).toBeGreaterThan(0);
});
