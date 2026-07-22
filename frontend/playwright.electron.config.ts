import path from "node:path";
import { defineConfig } from "@playwright/test";

// Playwright wipes its outputDir at startup, so it must NOT be the evidence dir
// the workflow uploads (that dir also holds dispatch.json + our screenshots and
// app logs). Nest Playwright's own output under a subdirectory of it instead.
const evidenceDir = process.env.AO_MAC_SMOKE_OUTPUT_DIR;
const outputDir = evidenceDir ? path.join(evidenceDir, "playwright") : "test-results/electron-titlebar-smoke";

// Dedicated config for the macOS Electron titlebar smoke (GH #76). It launches
// the REAL packaged Electron app (no dev server, no browser), so it has its own
// testDir and no `webServer`. Kept separate from playwright.config.ts so the
// browser e2e suite and this native smoke never run together by accident.
export default defineConfig({
	testDir: "e2e-electron",
	testMatch: "**/*.electron.ts",
	outputDir,
	// Packaging + first-boot daemon startup on a cold CI runner is slow.
	timeout: 240_000,
	expect: { timeout: 15_000 },
	workers: 1,
	retries: 0,
	reporter: "line",
	use: { trace: "retain-on-failure" },
});
