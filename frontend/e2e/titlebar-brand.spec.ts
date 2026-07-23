import { expect, test, type Locator, type Page } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

// Regression guard for #366: the sidebar's "Agent Orchestrator" brand must never
// sit under the top titlebar band, and the wordmark must stay readable. The
// original bug was board routes (`/` and `/projects/:id`) having no topbar, so
// the sidebar stayed at top-0 and the brand landed in the titlebar lane. The
// structural fix renders the shell topbar on every route, so the sidebar always
// hangs below the header — these tests lock that invariant in.
//
// GH #54 additionally makes the macOS TitlebarNav cluster Electron-only, so it
// never renders in browser mode. A Mac UA keeps the topbar on board routes
// (Linux would otherwise drop it) and lets us assert, in one place, that browser
// mode carries no Electron titlebar chrome while the brand still clears the band.
test.use({
	userAgent:
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
});

const brand = (page: Page) => page.getByText("Agent Orchestrator", { exact: true });

// The brand <span> has `truncate` (overflow:hidden), so it stays "visible" even
// when clipped to nothing. Compare scroll vs client width to prove the wordmark
// is actually fully rendered, not just present-but-clipped.
async function isTruncated(span: Locator) {
	return span.evaluate((el) => el.scrollWidth > el.clientWidth + 1);
}

async function expectBrandClearsHeader(page: Page) {
	// Browser mode must never render the macOS Electron titlebar cluster.
	await expect(page.locator(".titlebar-nav")).toHaveCount(0);

	const header = page.locator(".dashboard-app-header");
	await expect(header).toBeVisible();
	const span = brand(page);
	await expect(span).toBeVisible();

	const headerBox = await header.boundingBox();
	const brandBox = await span.boundingBox();
	expect(headerBox).not.toBeNull();
	expect(brandBox).not.toBeNull();

	// The sidebar (and its brand) hangs below the topbar band, never in it.
	expect(brandBox!.y).toBeGreaterThanOrEqual(headerBox!.y + headerBox!.height - 1);
	expect(await isTruncated(span)).toBe(false);
}

test("home board route: brand clears the titlebar band and stays readable", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/");
	await expect(page.getByText("Projects")).toBeVisible();
	await expectBrandClearsHeader(page);
});

test("project board route: brand clears the titlebar band and stays readable", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/");
	await expect(page.getByText("Projects")).toBeVisible();

	// In-app nav to /projects/:id (a hard load boots the router at the board).
	await page.getByRole("button", { name: "Open api-gateway dashboard" }).click();
	// The active project row marks itself aria-current=page once navigation lands.
	await expect(page.locator('[aria-current="page"]')).toBeVisible();

	await expectBrandClearsHeader(page);
});

test("brand stays put and readable when navigating board → session", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/");
	await expect(page.getByText("Projects")).toBeVisible();

	const boardBrandBox = await brand(page).boundingBox();
	expect(boardBrandBox).not.toBeNull();

	await page.getByRole("button", { name: "Open Split terminal mux responsibilities" }).click();
	await expect(page.locator(".dashboard-app-header")).toBeVisible();

	const sessionBrandBox = await brand(page).boundingBox();
	expect(sessionBrandBox).not.toBeNull();
	// Persistent shell element: no vertical/horizontal jump across the transition.
	expect(Math.abs(sessionBrandBox!.x - boardBrandBox!.x)).toBeLessThanOrEqual(1);
	expect(Math.abs(sessionBrandBox!.y - boardBrandBox!.y)).toBeLessThanOrEqual(1);
	await expectBrandClearsHeader(page);
});
