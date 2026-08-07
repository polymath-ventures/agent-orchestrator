import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

test.beforeEach(async ({ page }) => {
	await installBrowserModeApiFixtures(page);
});

// Standalone shell terminals (#2822): shells the user opens by hand, with no
// agent session behind them. They render as tabs beside the session's own pane.
// The real pane needs a daemon-spawned PTY, so the preview build stands in with
// an in-memory shell list — enough to cover the parts that live in the renderer:
// which tab is current, and that opening/closing updates the strip.
test("opens, selects, and closes standalone shell terminals from the tab strip", async ({ page }) => {
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.getByRole("button", { name: "New terminal" })).toBeVisible();

	const closeButtons = page.getByRole("button", { name: /^Close terminal / });
	const initialCount = await closeButtons.count();

	// The button at the end of the tab strip opens a shell and makes it the
	// active pane. It creates a terminal directly — no menu, no session picker.
	await page.getByRole("button", { name: "New terminal" }).click();
	await expect(page.getByRole("menu")).toHaveCount(0);
	await expect(closeButtons).toHaveCount(initialCount + 1);

	// Selecting the session tab hands the pane back to the agent. Matched by the
	// session's own title: the tab's accessible name is that title, and its
	// title attribute falls back to the label once the strip truncates it.
	// Scoped to the terminal tablist: the sidebar carries the same session name.
	const terminalTabs = page.getByRole("tablist", { name: "Open terminals" });
	const sessionTab = terminalTabs.getByRole("tab", { name: /^Split terminal mux responsibilities/ });
	await sessionTab.click();
	await expect(sessionTab).toHaveAttribute("aria-selected", "true");

	// Return to the shell so its active-tab close control is visible, then close
	// it and verify that only that tab was removed.
	await terminalTabs.getByRole("tab", { name: "api-gateway", exact: true }).click();
	await closeButtons.last().click();
	await expect(closeButtons).toHaveCount(initialCount);
});

// Regression: the open request used to be consumed by the session view, so the
// standalone terminals route could not open another shell. The shell layout
// owns it now even when no session view is mounted.
test("opens another terminal from the standalone view", async ({ page }) => {
	await page.goto("/#/terminals");
	await expect(page.getByRole("button", { name: "New terminal" })).toBeVisible();

	const closeButtons = page.getByRole("button", { name: /^Close terminal / });
	await expect(closeButtons).not.toHaveCount(0);
	const initialCount = await closeButtons.count();
	await page.getByRole("button", { name: "New terminal" }).click();

	await expect(page).toHaveURL(/#\/terminals$/);
	await expect(closeButtons).toHaveCount(initialCount + 1);
});

test("shows an empty state once every standalone terminal is closed", async ({ page }) => {
	await page.goto("/#/terminals");

	// Wait for the strip to render before counting — a count taken mid-mount
	// reads 0 and would skip the loop entirely, leaving the terminals open.
	const closeButtons = page.getByRole("button", { name: /^Close terminal / });
	await expect(closeButtons).not.toHaveCount(0);

	// Close one at a time, asserting the strip shrank before the next click: the
	// close is async, so clicking on a stale count would race the re-render.
	for (let remaining = await closeButtons.count(); remaining > 0; remaining--) {
		await closeButtons.first().click();
		await expect(closeButtons).toHaveCount(remaining - 1);
	}

	await expect(page.getByText("No terminals open")).toBeVisible();
});
