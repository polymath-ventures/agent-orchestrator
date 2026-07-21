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
  // The public check must exercise the browser-mode API path with an Origin.
  assert.match(text, /-H "Origin: \$public_url"/);
  // All dependencies are checked before any mutation happens.
  assert.match(text, /missing dependency/);
});
