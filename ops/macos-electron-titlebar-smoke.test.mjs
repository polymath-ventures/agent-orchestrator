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
	assert.match(workflow, /ref:\s*\$\{\{\s*github\.event\.repository\.default_branch\s*\}\}[\s\S]*?path:\s*harness/);
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
});

test("Playwright output cannot erase dispatch or native evidence", () => {
	assert.match(playwrightConfig, /path\.join\(evidenceDir,\s*"playwright"\)/);
	assert.doesNotMatch(playwrightConfig, /outputDir:\s*process\.env\.AO_MAC_SMOKE_OUTPUT_DIR/);
});

test("smoke verifies bridge, native buttons, cluster geometry, and screenshots", () => {
	assert.match(smoke, /_electron/);
	assert.match(smoke, /window\.ao/);
	assert.match(smoke, /\.titlebar-nav/);
	assert.match(smoke, /getWindowButtonPosition/);
	assert.match(smoke, /nativeButtonLane/);
	assert.match(smoke, /screenshot/);
	assert.match(smoke, /geometry\.json/);
});

test("smoke records native process diagnostics and bounds failed cleanup", () => {
	assert.match(smoke, /app-stdout\.log/);
	assert.match(smoke, /app-stderr\.log/);
	assert.match(smoke, /launch-failure\.json/);
	assert.match(smoke, /process\(\)/);
	assert.match(smoke, /SIGTERM/);
});

test("docs include dispatch instructions and isolated ARM64 self-hosted fallback", () => {
	assert.match(docs, /workflow_dispatch/);
	assert.match(docs, /macos-15/);
	assert.match(docs, /separate.+harness.+target/is);
	assert.match(docs, /self-hosted,\s*macOS,\s*ARM64,\s*ao-mac-ui/);
	assert.match(docs, /public repository/i);
	assert.match(docs, /isolated/i);
});
