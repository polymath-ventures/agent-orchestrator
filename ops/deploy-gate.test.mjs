// Behavioral tests for deploy.sh's healthz identity gate.
//
// deploy.test.mjs asserts source-text invariants, which prove the checks are
// still written down but not that they still work: a review found two ways the
// gate could pass a bad daemon while every text assertion stayed green (python
// -O stripping asserts, and an over-strict rollback path). These tests run the
// EXACT probe extracted from the script against fixture /healthz payloads, so a
// check that stops functioning fails here rather than in production.
import { readFile, writeFile, mkdtemp } from "node:fs/promises";
import { createServer } from "node:http";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import assert from "node:assert/strict";

const run = promisify(execFile);
const script = new URL("./deploy.sh", import.meta.url).pathname;
const SHA = "a".repeat(40);
const OTHER_SHA = "b".repeat(40);
const BIN = "/home/example/.local/bin/ao";

// Extract the probe from the script rather than copying it, so the tests cannot
// drift into passing against a snapshot of code that no longer ships.
async function probeSource() {
	const text = await readFile(script, "utf8");
	// The heredoc line carries redirections after the delimiter, so anchor on the
	// delimiter and skip to the end of that line.
	const body = text.match(/<<'PY'[^\n]*\n([\s\S]*?)\nPY\n/);
	assert.ok(body, "could not extract the python probe from deploy.sh");
	return body[1];
}

async function withDaemon(healthPayload, fn) {
	const server = createServer((_req, res) => {
		res.writeHead(200, { "Content-Type": "application/json" });
		res.end(JSON.stringify(healthPayload));
	});
	await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
	try {
		return await fn(server.address().port);
	} finally {
		await new Promise((resolve) => server.close(resolve));
	}
}

// Mirrors the script's own success test exactly: the shell keeps the streams
// apart, captures stdout through `$(...)` (which strips trailing newlines), and
// accepts it only if it is entirely digits. Judging success on the merged
// streams instead is what previously let a broken rollback path look green.
async function runGate({ health, expected = SHA, provenanceRequired = "1", env = {}, runFilePid = health.pid }) {
	const source = await probeSource();
	const dir = await mkdtemp(join(tmpdir(), "deploy-gate-"));
	return withDaemon(health, async (port) => {
		const runFile = join(dir, "running.json");
		await writeFile(runFile, JSON.stringify({ port, pid: runFilePid }));
		let stdout = "";
		let stderr = "";
		try {
			({ stdout, stderr } = await run("python3", ["-c", source, runFile, BIN, expected, provenanceRequired], {
				env: { ...process.env, ...env },
			}));
		} catch (err) {
			stdout = err.stdout ?? "";
			stderr = err.stderr ?? "";
		}
		const captured = stdout.replace(/\n+$/, "");
		return { ok: /^[0-9]+$/.test(captured), stdout: captured, stderr, output: `${stdout}${stderr}` };
	});
}

const clean = (over = {}) => ({
	status: "ok",
	pid: 4242,
	executablePath: BIN,
	buildRevision: SHA,
	buildModified: false,
	...over,
});

test("a clean build of the expected commit passes", async () => {
	const { ok } = await runGate({ health: clean() });
	assert.ok(ok, "the gate rejected the daemon it just deployed");
});

test("a daemon built from a different commit is refused", async () => {
	const { ok, output } = await runGate({ health: clean({ buildRevision: OTHER_SHA }) });
	assert.equal(ok, false, "a stale binary at the expected path passed the gate");
	assert.match(output, /not the expected/);
});

test("an unstamped build is refused, and says why", async () => {
	const { ok, output } = await runGate({ health: clean({ buildRevision: "unknown" }) });
	assert.equal(ok, false);
	assert.match(output, /unstamped build/);
});

test("a modified build of the right commit is refused", async () => {
	const { ok, output } = await runGate({ health: clean({ buildModified: true }) });
	assert.equal(ok, false);
	assert.match(output, /modified build/);
});

// Fail closed: an absent flag never reads as "clean".
test("a daemon that omits buildModified is refused", async () => {
	const health = clean();
	delete health.buildModified;
	const { ok } = await runGate({ health });
	assert.equal(ok, false);
});

test("the pre-existing identity checks still hold", async () => {
	const wrongPath = await runGate({ health: clean({ executablePath: "/usr/bin/ao" }) });
	assert.equal(wrongPath.ok, false, "a foreign binary passed the gate");
	const notOk = await runGate({ health: clean({ status: "degraded" }) });
	assert.equal(notOk.ok, false, "a non-ok daemon passed the gate");
	// A responder that is not the process the run file names — exercised with a
	// run-file pid that differs from the one healthz reports.
	const wrongPid = await runGate({ health: clean(), runFilePid: 9999 });
	assert.equal(wrongPid.ok, false, "a foreign process passed the gate");
	assert.match(wrongPid.output, /pid/);
});

// The forward/rollback split. Deploy always builds an artifact that can report
// its provenance, so silence there is fatal; rollback may legitimately target a
// release older than the field and must not be blocked in an emergency.
test("a daemon predating buildRevision fails a forward deploy", async () => {
	const health = clean();
	delete health.buildRevision;
	delete health.buildModified;
	const { ok, output } = await runGate({ health, provenanceRequired: "1" });
	assert.equal(ok, false);
	assert.match(output, /predates buildRevision/);
});

test("a daemon predating buildRevision does not block a rollback", async () => {
	const health = clean();
	delete health.buildRevision;
	delete health.buildModified;
	const { ok, stdout, stderr } = await runGate({ health, provenanceRequired: "0" });
	assert.ok(ok, "rollback to a pre-provenance release was blocked");
	// The port must be alone on stdout — the warning belongs on stderr, or the
	// shell's numeric success test rejects the very case this path allows.
	assert.match(stdout, /^[0-9]+$/);
	assert.match(stderr, /predates buildRevision/, "the unverified provenance must still be announced");
});

// The relaxation is for artifacts that cannot ANSWER, never for ones that answer
// badly: a rollback target that reports a dirty tree is still refused.
test("a rollback still refuses a daemon that reports a modified build", async () => {
	const health = clean({ buildModified: true });
	delete health.buildRevision;
	const { ok, output } = await runGate({ health, provenanceRequired: "0" });
	assert.equal(ok, false, "a dirty rollback target passed the relaxed gate");
	assert.match(output, /modified build/);
});

test("a rollback still refuses the wrong commit and an unstamped build", async () => {
	const wrong = await runGate({ health: clean({ buildRevision: OTHER_SHA }), provenanceRequired: "0" });
	assert.equal(wrong.ok, false, "the relaxed gate accepted the wrong commit");
	const unstamped = await runGate({ health: clean({ buildRevision: "unknown" }), provenanceRequired: "0" });
	assert.equal(unstamped.ok, false, "the relaxed gate accepted an unstamped build");
});

// python -O strips assert statements. An assert-based gate would pass anything
// parseable here, silently, which is why the checks raise instead.
test("PYTHONOPTIMIZE cannot disable the gate", async () => {
	for (const [label, health] of [
		["wrong commit", clean({ buildRevision: OTHER_SHA })],
		["foreign binary", clean({ executablePath: "/usr/bin/ao" })],
	]) {
		const { ok } = await runGate({ health, env: { PYTHONOPTIMIZE: "1" } });
		assert.equal(ok, false, `python -O stripped the ${label} check; checks must raise, not assert`);
	}
	const { ok } = await runGate({ health: clean(), runFilePid: 9999, env: { PYTHONOPTIMIZE: "1" } });
	assert.equal(ok, false, "python -O stripped the pid check; checks must raise, not assert");
});
