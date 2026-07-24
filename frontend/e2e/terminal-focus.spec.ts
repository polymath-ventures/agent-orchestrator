import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

// #131. Switching to the terminals screen left DOM focus on whatever nav
// control the user clicked, so the attached terminal ignored keystrokes until a
// second click landed inside the pane. xterm reads keys through a hidden helper
// textarea, so "is the terminal focused" is exactly "is that textarea the
// active element" — asserted here against a real xterm in a real browser.
const activeElementClass = (page: import("@playwright/test").Page) =>
	page.evaluate(() => document.activeElement?.className ?? "");

test("terminals screen hands keyboard focus to the terminal with no extra click", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/terminals");
	await expect(page.getByTestId("session-terminal")).toBeVisible();

	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});

// Selecting another shell tab puts focus on the tab button and remounts the
// pane (TerminalPane keys mounts by terminal handle); focus must follow the
// newly selected terminal rather than stay on the button. Asserted against
// aria-current so it cannot pass while some other tab is the active one.
test("focus follows the terminal when another shell tab is selected", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/terminals");
	await page.getByRole("button", { name: "New terminal" }).click();
	await expect(page.getByRole("button", { name: /^Close terminal / })).toHaveCount(2);

	// Tabs are scoped by their working-dir title: the sidebar project button
	// carries the same accessible name as the first tab. Asserting which tab is
	// current before and after the click keeps this from passing while some other
	// terminal is the one on screen.
	const opened = page.locator('button[title="/Users/me/api-gateway"]');
	const unopened = page.locator('button[title="/Users/me/.ao"]');
	await expect(opened).toHaveAttribute("aria-current", "true");

	await unopened.click();
	await expect(unopened).toHaveAttribute("aria-current", "true");

	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});

// The same shells also live in a session's tab strip, reached by the same
// controls; #131 applies there identically.
test("focus follows a shell selected from a session's tab strip", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.getByTestId("session-terminal")).toBeVisible();

	const shellTab = page.getByRole("button", {
		name: "api-gateway",
		exact: true,
		description: "/Users/me/api-gateway",
	});
	await shellTab.click();
	await expect(shellTab).toHaveAttribute("aria-current", "true");

	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});

// Deliberate scope: only the terminals screen opts in. A session pane also
// mounts behind pop-out overlays and re-keys when a background poll assigns a
// starting session its terminal handle, so it must keep waiting for a click.
test("a session terminal does not grab focus on arrival", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.getByTestId("session-terminal")).toBeVisible();

	expect(await activeElementClass(page)).not.toContain("xterm-helper-textarea");
});
