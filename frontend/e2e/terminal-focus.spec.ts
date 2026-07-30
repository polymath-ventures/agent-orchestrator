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

test("returning to the terminals screen focuses the selected terminal", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/terminals");
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");

	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.locator(".xterm-helper-textarea")).toBeAttached();

	await page.goto("/#/terminals");
	await expect(page.getByTestId("session-terminal")).toBeVisible();
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});

test("Ctrl+F6 exits terminal focus while bare Escape remains terminal input", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/terminals");
	await expect(page.getByTestId("session-terminal")).toBeVisible();
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");

	await page.keyboard.press("Escape");
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");

	await page.keyboard.press("Control+F6");
	await expect(page.locator('[data-terminal-tab="true"][aria-current="true"]')).toBeFocused();
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

	// Tabs carry data-terminal-tab; the sidebar project button shares the first
	// tab's accessible name, so scope to the tab marker and distinguish by label
	// (existing shell "api-gateway", opened shell "shell"). Asserting which tab is
	// current before and after the click keeps this from passing while some other
	// terminal is the one on screen.
	const existing = page.locator('[data-terminal-tab="true"]').filter({ hasText: "api-gateway" });
	const opened = page.locator('[data-terminal-tab="true"]').filter({ hasText: "shell" });
	await expect(opened).toHaveAttribute("aria-current", "true");

	await existing.click();
	await expect(existing).toHaveAttribute("aria-current", "true");

	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});

// The same shells also live in a session's tab strip, reached by the same
// controls; #131 applies there identically. Upstream scopes a session pane's
// shells to that session, so open one from the pane's Add-tab menu, hand the
// pane back to the agent, then reselect the shell and prove focus follows it.
test("focus follows a shell selected from a session's tab strip", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.getByTestId("session-terminal")).toBeVisible();

	await page.getByRole("button", { name: "Add tab" }).click();
	await page.getByRole("menuitem", { name: "Terminal" }).click();

	// Scope tab lookups to the terminal pane so the sidebar's same-named controls
	// do not collide.
	const pane = page.locator(".terminal-pane-frame");
	const sessionTab = pane.getByRole("button", { name: "Split terminal mux responsibilities", exact: true });
	const shellTab = pane.getByRole("button", { name: "api-gateway", exact: true });

	await sessionTab.click();
	await expect(sessionTab).toHaveAttribute("aria-current", "true");

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
	// Wait for the helper textarea itself, not just the pane: the pane's testid
	// exists before xterm's mount effect runs, so asserting on it alone would
	// pass in the window before the effect that could have taken focus.
	await expect(page.locator(".xterm-helper-textarea")).toBeAttached();

	expect(await activeElementClass(page)).not.toContain("xterm-helper-textarea");
});
