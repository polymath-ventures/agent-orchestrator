import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const workflow = readFileSync(".github/workflows/macos-electron-titlebar-smoke.yml", "utf8");
const smoke = readFileSync("frontend/e2e-electron/titlebar-chrome.spec.ts", "utf8");
const docs = readFileSync("docs/macos-electron-titlebar-smoke.md", "utf8");

test("macOS Electron titlebar smoke stays manual, hosted, and read-only", () => {
	assert.match(workflow, /workflow_dispatch:/);
	assert.doesNotMatch(workflow, /^\s*pull_request:/m);
	assert.match(workflow, /ref:/);
	assert.match(workflow, /runs-on:\s*blacksmith-6vcpu-macos-15/);
	assert.match(workflow, /permissions:\s*\n\s*contents:\s*read/);
	assert.doesNotMatch(workflow, /\$\{\{\s*secrets\./);
});

test("workflow packages the real Electron app and uploads evidence", () => {
	assert.match(workflow, /npm run package/);
	assert.match(workflow, /\/Applications\//);
	assert.match(workflow, /npm run test:electron-titlebar/);
	assert.match(workflow, /actions\/upload-artifact@v4/);
	assert.match(workflow, /if:\s*always\(\)/);
	assert.match(workflow, /electron-titlebar-smoke/);
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

test("docs include dispatch instructions and isolated ARM64 self-hosted fallback", () => {
	assert.match(docs, /workflow_dispatch/);
	assert.match(docs, /blacksmith-6vcpu-macos-15/);
	assert.match(docs, /self-hosted,\s*macOS,\s*ARM64,\s*ao-mac-ui/);
	assert.match(docs, /public repository/i);
	assert.match(docs, /isolated/i);
});
