import { readFile } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import test from "node:test";
import assert from "node:assert/strict";

const run = promisify(execFile);
const script = new URL("./deploy.sh", import.meta.url).pathname;

test("deploy.sh parses (bash -n)", async () => {
	await run("bash", ["-n", script]);
});

test("deploy.sh --help prints usage without touching the system", async () => {
	const { stdout } = await run("bash", [script, "--help"]);
	assert.match(stdout, /Usage:/);
	assert.match(stdout, /--rollback/);
});

test("deploy.sh keeps its load-bearing invariants", async () => {
	const text = await readFile(script, "utf8");
	// Strict mode: an unset var or failed build must never half-deploy.
	assert.match(text, /^set -euo pipefail$/m);
	// Drop-ins carry operator switches (prime, ports, advertised host) and
	// must survive deploys — the script may only sync top-level units.
	assert.doesNotMatch(text, /(cp|rm)[^\n]*\.service\.d/);
	// Rollback must reinstall the previous binary, not just re-point the link.
	assert.match(text, /install -m 755 "\$prev\/bin\/ao"/);
	// The verify step must gate on the daemon's own run file, not a guessed port.
	assert.match(text, /running\.json/);
	// Boot-log findings must be surfaced, never swallowed.
	assert.match(text, /Boot-log findings/);
	// Rollback must also roll units back, not just the binary and symlink.
	assert.match(text, /sync_units "\$prev"/);
	// The health gate must verify identity (pid + executablePath), not mere 200s.
	assert.match(text, /executablePath/);
	// The gate's own logic is covered behaviorally in deploy-gate.test.mjs, which
	// runs the extracted probe against fixture payloads. Only the wiring that the
	// probe cannot see from the inside is asserted here: each entry point must
	// pass its OWN expected revision, and only rollback may relax the
	// provenance requirement. A rollback verifying against the incoming sha, or a
	// deploy that tolerated a silent daemon, would both be invisible to the
	// behavioral suite.
	assert.match(text, /restart_and_verify "\$sha" 1/);
	assert.match(text, /restart_and_verify "\$\(cat "\$prev\/REVISION"\)" 0/);
	// The public check must exercise the browser-mode API path with an Origin.
	assert.match(text, /-H "Origin: \$public_url"/);
	// All dependencies are checked before any mutation happens.
	assert.match(text, /missing dependency/);
	// aong resolves ao as its sibling, so both must be built and installed
	// together — a deploy that ships only one silently pairs mismatched
	// binaries.
	assert.match(text, /go build -trimpath -o "\$rel\/bin\/aong" \.\/cmd\/aong/);
	assert.match(text, /install -m 755 "\$rel\/bin\/aong"/);
	// Rollback restores the outgoing release's aong when it has one, and must
	// never delete an installed binary it did not place there.
	assert.match(text, /install -m 755 "\$prev\/bin\/aong"/);
	assert.doesNotMatch(text, /rm -f "\$AONG_BIN_TARGET"/);
});
