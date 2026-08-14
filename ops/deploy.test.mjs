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
	// imports healthz-gate.mjs and runs it against fixture payloads. What that
	// suite CANNOT see is the shell wiring around the module — it calls the gate
	// directly — so the wiring is pinned here instead.
	//
	// Streams must stay apart. Merging stderr into stdout (`2>&1`) makes the
	// legacy-rollback warning indistinguishable from the port, and the numeric
	// success test below then rejects the one case that path exists to allow.
	// This shipped broken once; the behavioral suite could not catch it.
	assert.match(text, /2>"\$probe_err"/);
	// The gate is a node module, not an inline interpreter blob: ops/ is node,
	// npm is already a preflight dependency, and a real module can be imported
	// by its tests instead of extracted from this file.
	assert.match(text, /node "\$gate"/);
	assert.doesNotMatch(text, /python/);
	// Success is a bare numeric stdout and nothing else.
	assert.match(text, /''\|\*\[!0-9\]\*\)/);
	// Each entry point must pass its OWN expected revision, and only rollback may
	// relax the provenance requirement. A rollback verifying against the incoming
	// sha, or a deploy that tolerated a silent daemon, would both be invisible to
	// the behavioral suite.
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
	// Headless installs must package the same locked ACP runtime as Electron and
	// expose it at the install-prefix path the backend already probes. The build
	// and both required-file checks must finish before the active release flips.
	assert.match(text, /ACP_RUNTIME_TARGET="\$HOME\/\.local\/acp-runtime"/);
	assert.match(text, /npm --prefix frontend run build:acp-runtime --silent/);
	assert.match(text, /mv "\$rel\/source\/frontend\/resources\/acp-runtime" "\$rel\/acp-runtime"/);
	assert.match(text, /"\$rel\/acp-runtime\/node\/bin\/node"/);
	assert.match(text, /"\$rel\/acp-runtime\/node_modules\/@agentclientprotocol\/claude-agent-acp\/dist\/index\.js"/);
	assert.match(text, /ln -sfn "\$CURRENT\/acp-runtime" "\$ACP_RUNTIME_TARGET"/);
	assert.match(text, /WARN: active release has no ACP runtime; Claude Code chat is unavailable/);
	assert.match(text, /rm -f "\$ACP_RUNTIME_TARGET"/);
	assert.ok(
		text.indexOf("npm --prefix frontend run build:acp-runtime --silent") < text.indexOf('ln -sfn "$rel" "$CURRENT"'),
		"ACP runtime must be built before the active release flips",
	);
	assert.ok(
		text.indexOf('ln -sfn "$rel" "$CURRENT"') < text.lastIndexOf("\n  sync_acp_runtime_link\n"),
		"runtime link must be installed after the newly active release flips",
	);
	const rollback = text.slice(text.indexOf("rollback() {"), text.indexOf("\ndeploy() {"));
	assert.match(
		rollback,
		/ln -sfn "\$prev" "\$CURRENT"[\s\S]*sync_acp_runtime_link/,
		"rollback must refresh or remove the runtime link for its own release",
	);
});
