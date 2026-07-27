// Behavioral tests for deploy.sh's healthz identity gate.
//
// deploy.test.mjs asserts source-text invariants, which prove the checks are
// still written down but not that they still work. These import the gate and
// run it, so a check that stops functioning fails here rather than in production.
import test from "node:test";
import assert from "node:assert/strict";
import { checkHealth, GateError } from "./healthz-gate.mjs";

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
