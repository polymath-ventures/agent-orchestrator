import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const workflow = readFileSync(".github/workflows/macos-electron-titlebar-smoke.yml", "utf8");
const playwrightConfig = readFileSync("frontend/playwright.electron.config.ts", "utf8");
const smoke = readFileSync("frontend/e2e-electron/titlebar-chrome.electron.ts", "utf8");
const docs = readFileSync("docs/macos-electron-titlebar-smoke.md", "utf8");

test("macOS Electron titlebar smoke stays manual, hosted, and read-only", () => {
	assert.match(workflow, /workflow_dispatch:/);
	assert.doesNotMatch(workflow, /^\s*pull_request:/m);
	assert.match(workflow, /ref:/);
	assert.match(workflow, /runs-on:\s*macos-15/);
	assert.match(workflow, /permissions:\s*\n\s*contents:\s*read/);
	assert.doesNotMatch(workflow, /\$\{\{\s*secrets\./);
});

test("workflow packages the real Electron app and uploads evidence", () => {
	assert.match(workflow, /npm run package/);
	assert.match(workflow, /\/Applications\//);
	assert.match(workflow, /chown -R "\$\(id -un\):\$\(id -gn\)"/);
	assert.doesNotMatch(workflow, /:staff/);
	assert.match(workflow, /npm run test:electron-titlebar/);
	assert.match(workflow, /actions\/upload-artifact@v4/);
	assert.match(workflow, /if:\s*always\(\)/);
	assert.match(workflow, /electron-titlebar-smoke/);
});

test("workflow keeps the merged harness separate from the requested target ref", () => {
	assert.equal((workflow.match(/actions\/checkout@v4/g) ?? []).length, 2);
	assert.match(workflow, /ref:\s*\$\{\{\s*github\.sha\s*\}\}[\s\S]*?path:\s*harness/);
	assert.match(workflow, /ref:\s*\$\{\{\s*inputs\.ref\s*\}\}[\s\S]*?path:\s*target/);
	assert.match(workflow, /go-version-file:\s*target\/backend\/go\.mod/);
	assert.match(workflow, /working-directory:\s*target\/frontend[\s\S]*?npm run package/);
	assert.match(workflow, /working-directory:\s*harness\/frontend[\s\S]*?npm run test:electron-titlebar/);
	assert.match(workflow, /find target\/frontend\/out/);
});

test("workflow creates dispatch evidence before the native smoke can fail", () => {
	assert.match(workflow, /mkdir -p "\$output_dir"/);
	assert.match(workflow, /git -C harness rev-parse HEAD/);
	assert.match(workflow, /git -C target rev-parse HEAD/);
	assert.match(workflow, /dispatch\.json/);
	assert.match(workflow, /Diagnose direct packaged-app launch/);
	assert.match(workflow, /direct-app-stdout\.log/);
	assert.match(workflow, /direct-app-stderr\.log/);
});

test("workflow waits for a real CDP page target before running Playwright", () => {
	assert.match(workflow, /grep -q '"type":\s\*"page"' "\$AO_MAC_SMOKE_OUTPUT_DIR\/direct-app-targets\.json"/);
	assert.doesNotMatch(workflow, /if \[\[ ! -s "\$AO_MAC_SMOKE_OUTPUT_DIR\/direct-app-targets\.json" \]\]/);
});

test("workflow captures launched-app logs, resolves its main PID, and always stops it", () => {
	assert.match(workflow, /--stdout "\$AO_MAC_SMOKE_OUTPUT_DIR\/direct-app-stdout\.log"/);
	assert.match(workflow, /--stderr "\$AO_MAC_SMOKE_OUTPUT_DIR\/direct-app-stderr\.log"/);
	assert.match(workflow, /CFBundleIdentifier/);
	assert.match(workflow, /bundle identifier is bundleId/);
	assert.doesNotMatch(workflow, /pgrep -f "\$executable"/);
	assert.match(workflow, /name: Stop staged app/);
	assert.match(workflow, /pkill -TERM -f "\$process_pattern"/);
	assert.match(workflow, /pkill -KILL -f "\$process_pattern"/);
});

test("Playwright output cannot erase dispatch or native evidence", () => {
	assert.match(playwrightConfig, /path\.join\(evidenceDir,\s*"playwright"\)/);
	assert.doesNotMatch(playwrightConfig, /outputDir:\s*process\.env\.AO_MAC_SMOKE_OUTPUT_DIR/);
});

test("smoke verifies bridge, native buttons, cluster geometry, and screenshots", () => {
	assert.match(smoke, /connectOverCDP/);
	assert.match(smoke, /window\.ao/);
	assert.match(smoke, /\.titlebar-nav/);
	assert.match(smoke, /osascript/);
	assert.match(smoke, /nativeButtons/);
	assert.match(smoke, /nativeButtonLane/);
	assert.match(smoke, /screenshot/);
	assert.match(smoke, /geometry\.json/);
});

test("smoke compares native and renderer geometry in the same window-local frame", () => {
	assert.match(smoke, /const wins=p\.windows\(\)/);
	assert.match(smoke, /\.find\(w=>btns\(w\)\.some/);
	assert.match(smoke, /const wp=win\.position\(\)/);
	assert.match(smoke, /b\.position\(\)\[0\]-wp\[0\]/);
	assert.match(smoke, /b\.position\(\)\[1\]-wp\[1\]/);
	assert.match(smoke, /nativeWindow: nativeMeasurement\.window/);
	assert.doesNotMatch(smoke, /p\.windows\[0\]\.buttons/);
});

test("smoke creates Electron userData before the remote-debugging launch", () => {
	assert.match(workflow, /\$HOME\/\.ao\/electron/);
});

test("docs include dispatch instructions and isolated ARM64 self-hosted fallback", () => {
	assert.match(docs, /workflow_dispatch/);
	assert.match(docs, /macos-15/);
	assert.match(docs, /separate.+harness.+target/is);
	assert.match(docs, /self-hosted,\s*macOS,\s*ARM64,\s*ao-mac-ui/);
	assert.match(docs, /public repository/i);
	assert.match(docs, /isolated/i);
});

test("docs name the native accessibility mechanism and real evidence files", () => {
	assert.match(docs, /System Events accessibility/);
	assert.match(docs, /direct-app-stdout\.log/);
	assert.match(docs, /direct-app-stderr\.log/);
	assert.doesNotMatch(docs, /launch-failure\.json/);
	assert.doesNotMatch(docs, /getWindowButtonPosition/);
});
