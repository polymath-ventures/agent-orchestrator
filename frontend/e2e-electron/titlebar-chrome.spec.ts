import { _electron as electron, expect, test } from "@playwright/test";
import { execFile } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// main.ts sets trafficLightPosition.x=14. The three native controls occupy the
// measured x=14..66 lane documented beside .titlebar-nav in styles.css.
const NATIVE_BUTTON_LANE_WIDTH = 52;
const NATIVE_BUTTON_LANE_HEIGHT = 16;
const EXPECTED_GROUP_GAP = { min: 10, max: 16 }; // intended value: 13px

test.skip(process.platform !== "darwin", "macOS native window-button smoke");

test("macOS Electron titlebar cluster clears the native traffic lights", async () => {
	const appPath = process.env.AO_MAC_SMOKE_APP_PATH;
	const outputDir = process.env.AO_MAC_SMOKE_OUTPUT_DIR;
	if (!appPath || !outputDir) {
		throw new Error("AO_MAC_SMOKE_APP_PATH and AO_MAC_SMOKE_OUTPUT_DIR are required");
	}

	await mkdir(outputDir, { recursive: true });
	const executablePath = path.join(appPath, "Contents", "MacOS", "agent-orchestrator");
	const dataDir = path.join(process.env.RUNNER_TEMP || outputDir, `ao-titlebar-smoke-data-${process.pid}`);

	const electronApp = await electron.launch({
		executablePath,
		env: {
			...process.env,
			AO_DATA_DIR: dataDir,
			AO_RUN_FILE: path.join(dataDir, "running.json"),
		},
	});

	try {
		const appWindow = await electronApp.firstWindow();
		await appWindow.waitForLoadState("domcontentloaded");

		const readEnvironment = () =>
			appWindow.evaluate(() => ({
				hasBridge: Boolean(window.ao),
				userAgent: navigator.userAgent,
				maxTouchPoints: navigator.maxTouchPoints,
			}));
		await expect
			.poll(readEnvironment, { timeout: 180_000, message: "wait for the real Electron preload bridge" })
			.toMatchObject({ hasBridge: true });
		const environment = await readEnvironment();

		const cluster = appWindow.locator(".titlebar-nav");
		await expect(cluster).toBeVisible({ timeout: 180_000 });

		const clusterBox = await cluster.boundingBox();
		expect(clusterBox, "TitlebarNav must have measurable renderer geometry").not.toBeNull();

		const nativePosition = await electronApp.evaluate(({ BrowserWindow }) => {
			const mainWindow = BrowserWindow.getAllWindows()[0];
			return mainWindow?.getWindowButtonPosition() ?? null;
		});
		expect(nativePosition, "macOS BrowserWindow must expose native window-button position").not.toBeNull();

		const nativeButtonLane = {
			left: nativePosition!.x,
			right: nativePosition!.x + NATIVE_BUTTON_LANE_WIDTH,
			top: nativePosition!.y,
			bottom: nativePosition!.y + NATIVE_BUTTON_LANE_HEIGHT,
		};
		const gap = clusterBox!.x - nativeButtonLane.right;

		expect(clusterBox!.x, "renderer cluster must not overlap the native traffic-light lane").toBeGreaterThan(
			nativeButtonLane.right,
		);
		expect(gap, "renderer cluster should keep the intended traffic-light group gap").toBeGreaterThanOrEqual(
			EXPECTED_GROUP_GAP.min,
		);
		expect(gap).toBeLessThanOrEqual(EXPECTED_GROUP_GAP.max);

		const rendererScreenshot = path.join(outputDir, "renderer-titlebar.png");
		await appWindow.screenshot({ path: rendererScreenshot });

		// Best-effort whole-desktop evidence. On hosted macOS this may be denied by
		// Screen Recording privacy controls; renderer + geometry evidence remains
		// authoritative and the failure is recorded instead of failing the smoke.
		const desktopScreenshot = path.join(outputDir, "macos-desktop.png");
		let desktopScreenshotError: string | null = null;
		try {
			await execFileAsync("/usr/sbin/screencapture", ["-x", desktopScreenshot], { timeout: 10_000 });
		} catch (error) {
			desktopScreenshotError = error instanceof Error ? error.message : String(error);
		}

		await writeFile(
			path.join(outputDir, "geometry.json"),
			`${JSON.stringify(
				{
					appPath,
					executablePath,
					platform: process.platform,
					arch: process.arch,
					environment,
					nativePosition,
					nativeButtonLane,
					clusterBox,
					gap,
					expectedGap: EXPECTED_GROUP_GAP,
					rendererScreenshot,
					desktopScreenshot,
					desktopScreenshotError,
				},
				null,
				2,
			)}\n`,
		);
	} finally {
		await electronApp.close();
	}
});
