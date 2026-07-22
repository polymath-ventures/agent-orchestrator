import { defineConfig } from "@playwright/test";

// Dedicated config for the macOS Electron titlebar smoke (GH #76). It launches
// the REAL packaged Electron app (no dev server, no browser), so it has its own
// testDir and no `webServer`. Kept separate from playwright.config.ts so the
// browser e2e suite and this native smoke never run together by accident.
export default defineConfig({
	testDir: "e2e-electron",
	outputDir: process.env.AO_MAC_SMOKE_OUTPUT_DIR || "test-results/electron-titlebar-smoke",
	// Packaging + first-boot daemon startup on a cold CI runner is slow.
	timeout: 240_000,
	expect: { timeout: 15_000 },
	workers: 1,
	retries: 0,
	reporter: "line",
	use: { trace: "retain-on-failure" },
});
