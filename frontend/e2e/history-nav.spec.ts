import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test.use({
	userAgent:
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
});

// GH #54: the titlebar back/forward arrows are macOS Electron chrome and must
// never appear in browser mode (even under a Mac desktop UA), because there is
// no Electron bridge. Browsers rely on the browser's own history controls; the
// TitlebarNav arrow behavior is exercised for Electron by TitlebarNav's unit
// coverage, which stubs the bridge.
test("browser mode exposes no titlebar history arrows", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/");
	await expect(page.getByText("Projects")).toBeVisible();

	// Navigate home → session view so a real history entry exists; in Electron
	// this is where the forward arrow would light up.
	await page.getByRole("button", { name: "Open Split terminal mux responsibilities" }).click();
	await expect(page).toHaveURL(/sessions\/refactor-mux/);

	await expect(page.locator(".titlebar-nav")).toHaveCount(0);
	await expect(page.getByRole("button", { name: "Go back" })).toHaveCount(0);
	await expect(page.getByRole("button", { name: "Go forward" })).toHaveCount(0);
});
