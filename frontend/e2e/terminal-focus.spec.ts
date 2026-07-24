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

// Opening a shell moves focus to the button that was clicked and remounts the
// pane (TerminalPane keys mounts by terminal handle); focus must follow the new
// terminal rather than stay on the button.
test("focus follows the terminal when a new shell tab is opened", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/terminals");
	await expect(page.getByTestId("session-terminal")).toBeVisible();

	await page.getByRole("button", { name: "New terminal" }).click();
	await expect(page.getByRole("button", { name: /^Close terminal / })).toHaveCount(2);

	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});
