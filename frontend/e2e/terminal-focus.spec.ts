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
	await expect(page.getByTestId("session-terminal-slot").getByTestId("session-terminal")).toBeVisible();

	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});

test("returning to the terminals screen focuses the selected terminal", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/terminals");
	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");

	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.locator(".xterm-helper-textarea")).toBeAttached();

	await page.goto("/#/terminals");
	await expect(page.getByTestId("session-terminal-slot").getByTestId("session-terminal")).toBeVisible();
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
// shells to that session, so open one from the pane's New-terminal button, hand
// the pane back to the agent, then reselect the shell and prove focus follows it.
test("focus follows a shell selected from a session's tab strip", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.getByTestId("session-terminal")).toBeVisible();

	// Upstream replaced the Add-tab dropdown with a button that opens a terminal
	// directly (#3275); the focus contract this test guards is unchanged.
	await page.getByRole("button", { name: "New terminal" }).click();

	// Scope tab lookups to the terminal pane so the sidebar's same-named controls
	// do not collide.
	const topbar = page.getByTestId("session-workspace-topbar");
	const sessionTab = topbar.getByRole("tab", { name: /Split terminal mux responsibilities/ });
	const shellTab = topbar.getByRole("tab", { name: "api-gateway", exact: true });

	await sessionTab.click();
	await expect(sessionTab).toHaveAttribute("aria-current", "true");

	await shellTab.click();
	await expect(shellTab).toHaveAttribute("aria-current", "true");

	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});

// Reopened #131. The previous fix covered the standalone terminals screen and
// shell tabs, but opening a project's orchestrator session still left focus on
// the route control instead of the xterm helper textarea.
test("opening an orchestrator session focuses its terminal on arrival", async ({ page }) => {
	await installBrowserModeApiFixtures(page);
	await page.goto("/#/projects/api-gateway/sessions/orch-api-gateway");
	await expect(page.locator(".xterm-helper-textarea")).toBeAttached();

	await expect.poll(() => activeElementClass(page)).toContain("xterm-helper-textarea");
});

test("a pending session terminal does not focus behind a browser pop-out when its handle arrives", async ({ page }) => {
	test.setTimeout(35_000);
	const fixtures = await installBrowserModeApiFixtures(page);
	let terminalHandleReady = false;
	await page.route("**/api/v1/sessions", (route) => {
		if (route.request().method() !== "GET") return route.fallback();
		return route.fulfill({
			json: {
				sessions: [
					{
						activity: { lastActivityAt: "2026-07-20T18:00:00.000Z", state: "active" },
						branch: "refactor/mux",
						createdAt: "2026-07-20T18:00:00.000Z",
						displayName: "Split terminal mux responsibilities",
						harness: "codex",
						id: "refactor-mux",
						isTerminated: false,
						kind: "worker",
						projectId: "api-gateway",
						// Upstream now gates the pop-out control on the browser actually
						// having a page (`canPopOut = poppedOut || navState.url`). In web
						// mode the session's preview target is what puts one there, so the
						// session carries one; the focus assertion below is unchanged.
						previewUrl: "http://127.0.0.1:4173/",
						previewRevision: 1,
						prs: [],
						status: "working",
						terminalHandleId: terminalHandleReady ? "term-refactor-mux" : undefined,
						updatedAt: "2026-07-20T18:00:00.000Z",
					},
				],
			},
		});
	});

	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.getByText("Preparing the worker terminal")).toBeVisible();
	await page.getByRole("tab", { name: "Browser" }).click();
	await page.getByRole("button", { name: "Pop out" }).click();
	await expect(page.getByTestId("browser-panel").getByRole("button", { name: "Return to panel" })).toBeVisible();

	terminalHandleReady = true;
	await expect.poll(() => fixtures.muxConnections, { timeout: 20_000 }).toBeGreaterThan(0);
	expect(await activeElementClass(page)).not.toContain("xterm-helper-textarea");
});
