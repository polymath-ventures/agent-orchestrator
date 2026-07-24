import { expect, test } from "@playwright/test";
import { installBrowserModeApiFixtures } from "./fixtures";

// #2483 TRM-001, RENDERER SLICE. Under browser mode the fixture owns the daemon
// API and mux websocket, so this proves the renderer attaches the terminal
// surface and paints a stream. The real zellij/PTY process is exercised only in
// the packaged-app pod gate (#2697). Not the canonical T0/P0 gate.

test("renderer: terminal attaches on session detail and renders a stream @T0 @TRM", async ({ page }) => {
	const fixtures = await installBrowserModeApiFixtures(page);
	await page.goto("/#/projects/api-gateway/sessions/refactor-mux");
	await expect(page.getByTestId("session-detail")).toBeVisible();

	const terminal = page.getByTestId("session-terminal");
	await expect(terminal).toBeVisible();
	await expect.poll(() => fixtures.muxConnections).toBeGreaterThan(0);
	await expect(terminal).not.toContainText("Terminal disconnected");
});
