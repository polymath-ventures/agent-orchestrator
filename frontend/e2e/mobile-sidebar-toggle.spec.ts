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

	// The topbar is inert while the modal Sheet is open (its overlay intercepts
	// the opener), so dismiss the way a mobile user does — tapping the backdrop
	// outside the Sheet — and prove the sidebar closes.
	await page.mouse.click(375, 420);
	await expect(sheet).toBeHidden();
	await expect(projectsLabel).toBeHidden();
});

test("mobile: settings deep link returns to a topbar with the browser sidebar opener", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	// Settings now opens as a modal over the board. Close it before interacting
	// with the app chrome, then prove the shared topbar still exposes the mobile
	// sidebar opener.
	await page.goto("/#/settings");

	const settings = page.getByRole("dialog", { name: "General" });
	await expect(settings).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(settings).toBeHidden();
	await expect(page.locator(".dashboard-app-header")).toBeVisible();
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

test("mobile: first-launch welcome board (no projects) still exposes the browser sidebar opener", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	// Empty the projects list so "/" resolves to the first-launch welcome board —
	// a distinct hideShellTopbar predicate (isWelcomeBoard) from settings, and the
	// canonical first-run mobile surface for GH #54. A later route wins in
	// Playwright, so this overrides the fixture's populated projects response.
	await page.route("**/api/v1/projects", (route) => {
		if (route.request().method() !== "GET") return route.fallback();
		return route.fulfill({ json: { projects: [] } });
	});
	await page.goto("/");

	await expect(page.locator(".dashboard-app-header")).toHaveCount(0);
	await expect(page.locator(".titlebar-nav")).toHaveCount(0);

	const toggle = page.getByRole("button", { name: "Open sidebar" });
	await expect(toggle).toBeVisible();
	await toggle.click();
	await expect(page.getByRole("dialog")).toBeVisible();
});
