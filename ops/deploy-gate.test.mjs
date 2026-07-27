// Behavioral tests for deploy.sh's healthz identity gate.
//
// deploy.test.mjs asserts source-text invariants, which prove the checks are
// still written down but not that they still work. These import the gate and
// run it, so a check that stops functioning fails here rather than in production.
import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { execFile } from "node:child_process";
import { mkdtemp, writeFile, symlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { checkHealth, GateError } from "./healthz-gate.mjs";

const GATE = new URL("./healthz-gate.mjs", import.meta.url).pathname;

// Temp dirs are registered for removal so a full run does not litter os.tmpdir()
// with gate-* directories.
async function scratch(t, prefix) {
	const dir = await mkdtemp(join(tmpdir(), prefix));
	t.after(() => rm(dir, { recursive: true, force: true }));
	return dir;
}

function spawnGate(args, entry = GATE) {
	return new Promise((resolve) => {
		execFile(process.execPath, [entry, ...args], (err, stdout, stderr) =>
			resolve({ code: err?.code ?? 0, stdout, stderr }),
		);
	});
}

// Runs the real CLI against a fixture daemon, and judges success exactly as
// deploy.sh does: a bare-numeric stdout, nothing else.
async function runCli({ t, health, provenanceRequired = "1", entry = GATE }) {
	const server = createServer((_q, r) => {
		r.writeHead(200, { "Content-Type": "application/json" });
		r.end(JSON.stringify(health));
	});
	await new Promise((res) => server.listen(0, "127.0.0.1", res));
	try {
		const dir = await scratch(t, "gate-cli-");
		const runFile = join(dir, "running.json");
		await writeFile(runFile, JSON.stringify({ port: server.address().port, pid: health.pid }));
		const { code, stdout, stderr } = await spawnGate([runFile, BIN, SHA, provenanceRequired], entry);
		const out = stdout.replace(/\n+$/, "");
		return { ok: code === 0 && /^[0-9]+$/.test(out), code, stdout: out, stderr };
	} finally {
		await new Promise((res) => server.close(res));
	}
}

const SHA = "a".repeat(40);
const OTHER_SHA = "b".repeat(40);
const BIN = "/home/example/.local/bin/ao";
const RUN = { pid: 4242, port: 41963 };

const clean = (over = {}) => ({
	status: "ok",
	pid: RUN.pid,
	executablePath: BIN,
	buildRevision: SHA,
	buildModified: false,
	...over,
});

const gate = (health, provenanceRequired = true) =>
	checkHealth({ health, run: RUN, binTarget: BIN, expectedSha: SHA, provenanceRequired });

// Returns the rejection message, or null if the gate accepted.
const rejection = (health, provenanceRequired = true) => {
	try {
		gate(health, provenanceRequired);
		return null;
	} catch (err) {
		assert.ok(err instanceof GateError, `expected a GateError, got ${err}`);
		return err.message;
	}
};

test("a clean build of the expected commit passes", () => {
	assert.equal(gate(clean()), null, "the gate rejected the daemon it just deployed");
});

test("a daemon built from a different commit is refused", () => {
	assert.match(rejection(clean({ buildRevision: OTHER_SHA })) ?? "", /not the expected/);
});

test("an unstamped build is refused, and says why", () => {
	assert.match(rejection(clean({ buildRevision: "unknown" })) ?? "", /unstamped build/);
});

test("a modified build of the right commit is refused", () => {
	assert.match(rejection(clean({ buildModified: true })) ?? "", /modified build/);
});

// Fail closed: an absent flag never reads as "clean".
test("a daemon that omits buildModified is refused", () => {
	const health = clean();
	delete health.buildModified;
	assert.match(rejection(health) ?? "", /does not report a clean build/);
});

test("the pre-existing identity checks still hold", () => {
	assert.match(rejection(clean({ executablePath: "/usr/bin/ao" })) ?? "", /responder is/);
	assert.match(rejection(clean({ status: "degraded" })) ?? "", /status/);
	assert.match(rejection(clean({ pid: 9999 })) ?? "", /pid/);
});

// The forward/rollback split. Deploy always builds an artifact that can report
// its provenance, so silence there is fatal; rollback may legitimately target a
// release older than the field and must not be blocked in an emergency.
test("a daemon predating buildRevision fails a forward deploy", () => {
	const health = clean();
	delete health.buildRevision;
	delete health.buildModified;
	assert.match(rejection(health, true) ?? "", /predates buildRevision/);
});

test("a daemon predating buildRevision does not block a rollback", () => {
	const health = clean();
	delete health.buildRevision;
	delete health.buildModified;
	const warning = gate(health, false);
	assert.match(warning ?? "", /predates buildRevision/, "the unverified provenance must still be announced");
});

// The relaxation is for artifacts that cannot ANSWER, never for ones that answer
// badly.
test("a rollback still refuses a daemon that reports a modified build", () => {
	const health = clean({ buildModified: true });
	delete health.buildRevision;
	assert.match(rejection(health, false) ?? "", /modified build/);
});

test("a rollback still refuses the wrong commit and an unstamped build", () => {
	assert.match(rejection(clean({ buildRevision: OTHER_SHA }), false) ?? "", /not the expected/);
	assert.match(rejection(clean({ buildRevision: "unknown" }), false) ?? "", /unstamped build/);
});

// The tests above exercise the decision logic. These exercise the shipped CLI
// boundary — entrypoint detection, file reading, stream separation, exit codes —
// which the pure tests cannot see, and which is where the symlink defect lived.
test("the CLI prints only the port on stdout, and warns on stderr", async (t) => {
	const { ok, stdout, stderr } = await runCli({ t, health: clean() });
	assert.ok(ok, `gate rejected a clean deploy: ${stderr}`);
	assert.match(stdout, /^[0-9]+$/, "stdout must carry the port and nothing else");

	const legacy = clean();
	delete legacy.buildRevision;
	delete legacy.buildModified;
	const rollback = await runCli({ t, health: legacy, provenanceRequired: "0" });
	assert.ok(rollback.ok, "rollback to a pre-provenance release was blocked");
	assert.match(rollback.stdout, /^[0-9]+$/);
	assert.match(rollback.stderr, /predates buildRevision/);
});

// $DEPLOY_ROOT/current is a symlink to the active release, so the gate can be
// invoked through one. Comparing import.meta.url to argv[1] raw made the CLI
// block silently not run — exit 0, no output, no verification at all.
test("the CLI still runs when invoked through a symlink", async (t) => {
	const dir = await scratch(t, "gate-link-");
	const link = join(dir, "healthz-gate.mjs");
	await symlink(GATE, link);
	const { ok, stdout } = await runCli({ t, health: clean(), entry: link });
	assert.ok(ok, "the gate did not verify anything when reached through a symlink");
	assert.match(stdout, /^[0-9]+$/);
});

test("the CLI fails loudly, and silently, on an unreachable daemon", async (t) => {
	const dir = await scratch(t, "gate-dead-");
	const runFile = join(dir, "running.json");
	await writeFile(runFile, JSON.stringify({ port: 1, pid: 4242 }));
	const { code, stdout, stderr } = await spawnGate([runFile, BIN, SHA, "1"]);
	assert.notEqual(code, 0, "an unreachable daemon must not pass the gate");
	assert.equal(stdout.trim(), "", "stdout must stay empty so the shell cannot read it as a port");
	assert.ok(stderr.trim().length > 0, "a failure must say why");
});
