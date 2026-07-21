import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

// Adversarial UI verification for GH #46. Drives the real production web bundle
// at an iPhone viewport + iOS user agent (browser mode over the tailnet is the
// reported surface) and proves the titlebar toggle opens the mobile sidebar
// Sheet — the symptom was that tapping it did nothing.
test.use({
	viewport: { width: 390, height: 844 },
	hasTouch: true,
	isMobile: true,
	userAgent:
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
});

test("mobile: titlebar toggle opens the sidebar sheet", async ({ page }, testInfo) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/");

	// The mobile sidebar Sheet is closed on load: the "Projects" nav label lives
	// inside it, so it must not be visible yet.
	const projectsLabel = page.getByText("Projects", { exact: true });
	await expect(projectsLabel).toBeHidden();

	// Tap the sidebar toggle (renders in the non-mac chrome now that iOS is
	// excluded from isMacDesktop).
	await page
		.getByRole("button", { name: /expand sidebar/i })
		.first()
		.click();

	const sheet = page.getByRole("dialog");
	await expect(sheet).toBeVisible();
	await expect(projectsLabel).toBeVisible();

	await page.screenshot({ path: testInfo.outputPath("mobile-sidebar-open.png") });

	// The left Sheet is 3/4 width; tap the dimmed overlay at the right edge.
	await page.mouse.click(370, 100);
	await expect(sheet).toBeHidden();
	await expect(projectsLabel).toBeHidden();
});
