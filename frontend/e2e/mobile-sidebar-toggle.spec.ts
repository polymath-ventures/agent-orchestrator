import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

// Adversarial UI verification for GH #54. Drives the real production web bundle
// at an iPhone viewport + iOS user agent (browser mode over the tailnet is the
// reported surface), proves macOS/Electron titlebar chrome is absent, and proves
// the replacement ShellTopbar opener still controls the mobile sidebar Sheet.
test.use({
	viewport: { width: 390, height: 844 },
	hasTouch: true,
	isMobile: true,
	userAgent:
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
});

test("mobile: browser topbar toggle opens the sidebar without macOS titlebar chrome", async ({ page }, testInfo) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/");

	await expect(page.locator(".titlebar-nav")).toHaveCount(0);
	await expect(page.locator(".dashboard-app-header")).not.toHaveClass(/pl-titlebar-content-offset/);

	// The mobile sidebar Sheet is closed on load: the "Projects" nav label lives
	// inside it, so it must not be visible yet.
	const projectsLabel = page.getByText("Projects", { exact: true });
	await expect(projectsLabel).toBeHidden();

	// The persistent ShellTopbar owns the mobile-browser opener. There are no
	// back/forward buttons here; those remain desktop Electron titlebar chrome.
	const toggle = page.getByRole("button", { name: "Open sidebar" });
	await expect(toggle).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath("mobile-browser-chrome-closed.png") });
	await toggle.click();

	const sheet = page.getByRole("dialog");
	await expect(sheet).toBeVisible();
	await expect(projectsLabel).toBeVisible();

	await page.screenshot({ path: testInfo.outputPath("mobile-sidebar-open.png") });

	// The topbar is inert while the modal Sheet is open; use the visible
	// in-Sheet collapse toggle to prove the sidebar still closes via a real
	// sidebar control (the overlay/Escape remain available too).
	await sheet.getByRole("button", { name: "Collapse sidebar" }).click();
	await expect(sheet).toBeHidden();
	await expect(projectsLabel).toBeHidden();
});

test("mobile: settings route hides the topbar but still exposes the browser sidebar opener", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	// Settings is a hideShellTopbar route: ShellTopbar (and its inline opener) is
	// gone, so the floating opener must keep the Sheet reachable (GH #54). The
	// app uses hash routing, so the settings deep link is /#/settings.
	await page.goto("/#/settings");

	// Confirm we are actually on the topbar-less settings route (not the board):
	// ShellTopbar's header must be absent, so the opener under test is the
	// floating one, not the topbar's inline copy.
	await expect(page.getByRole("heading", { name: /settings/i }).first()).toBeVisible();
	await expect(page.locator(".dashboard-app-header")).toHaveCount(0);
	await expect(page.locator(".titlebar-nav")).toHaveCount(0);
	const projectsLabel = page.getByText("Projects", { exact: true });
	await expect(projectsLabel).toBeHidden();

	const toggle = page.getByRole("button", { name: "Open sidebar" });
	await expect(toggle).toBeVisible();
	await toggle.click();

	const sheet = page.getByRole("dialog");
	await expect(sheet).toBeVisible();
	await expect(projectsLabel).toBeVisible();
});
