import { chromium, expect, test } from "@playwright/test";
import { execFile } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// main.ts sets trafficLightPosition.x=14. The three native controls occupy the
// measured x=14..66 lane documented beside .titlebar-nav in styles.css.
const EXPECTED_GROUP_GAP = { min: 10, max: 16 }; // intended value: 13px

test.skip(process.platform !== "darwin", "macOS native window-button smoke");

test("macOS Electron titlebar cluster clears the native traffic lights", async () => {
	const appPath = process.env.AO_MAC_SMOKE_APP_PATH;
	const outputDir = process.env.AO_MAC_SMOKE_OUTPUT_DIR;
	if (!appPath || !outputDir) {
		throw new Error("AO_MAC_SMOKE_APP_PATH and AO_MAC_SMOKE_OUTPUT_DIR are required");
	}

	await mkdir(outputDir, { recursive: true });
	const cdpUrl = process.env.AO_MAC_SMOKE_CDP_URL;
	const appPid = Number(process.env.AO_MAC_SMOKE_APP_PID);
	if (!cdpUrl || !appPid) throw new Error("AO_MAC_SMOKE_CDP_URL and AO_MAC_SMOKE_APP_PID are required");
	const browser = await chromium.connectOverCDP(cdpUrl);

	try {
		const appWindow = browser.contexts().flatMap((context) => context.pages())[0];
		expect(appWindow, "packaged app must expose a renderer page over CDP").toBeTruthy();
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

		const { stdout: nativeButtonsJson } = await execFileAsync("/usr/bin/osascript", [
			"-l",
			"JavaScript",
			"-e",
			`const se=Application('System Events');const p=se.processes.whose({unixId:${appPid}})[0];JSON.stringify(p.windows[0].buttons().map(b=>({description:b.description(),position:b.position(),size:b.size()})))`,
		]);
		const nativeButtons = JSON.parse(nativeButtonsJson) as Array<{
			description: string;
			position: [number, number];
			size: [number, number];
		}>;
		const windowButtons = nativeButtons.filter((button) => /close|minimize|zoom/i.test(button.description));
		expect(windowButtons.length, "macOS accessibility must expose native window buttons").toBeGreaterThanOrEqual(3);
		const nativePosition = {
			x: Math.min(...windowButtons.map((button) => button.position[0])),
			y: Math.min(...windowButtons.map((button) => button.position[1])),
		};

		const nativeButtonLane = {
			left: nativePosition!.x,
			right: Math.max(...windowButtons.map((button) => button.position[0] + button.size[0])),
			top: nativePosition.y,
			bottom: Math.max(...windowButtons.map((button) => button.position[1] + button.size[1])),
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
					platform: process.platform,
					arch: process.arch,
					environment,
					nativePosition,
					nativeButtons,
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
		await browser.close();
	}
});
