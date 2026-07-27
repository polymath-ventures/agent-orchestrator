// The deploy's identity gate: proves the daemon answering /healthz is the one
// this deploy installed, and that it was built from the commit being deployed.
//
// executablePath proves WHICH FILE is answering; buildRevision proves WHAT THAT
// FILE IS. Without the second, a stale binary left at the expected path
// satisfies every other check, which is the failure this gate exists to catch.
//
// Node rather than an inline shell heredoc because every other script in ops/ is
// node, `npm` is already a deploy preflight dependency, and being a real module
// means the tests import it instead of extracting it from a shell script.
import { readFile } from "node:fs/promises";

export class GateError extends Error {}

const fail = (message) => {
	throw new GateError(message);
};

/**
 * Checks a /healthz payload against the deploy's expectations.
 *
 * provenanceRequired is false only on rollback: a release built before the
 * daemon reported its own revision cannot answer, and blocking recovery to a
 * known-good older release is worse than the gap. Everything the artifact CAN
 * prove is still enforced there — the relaxation is for artifacts that cannot
 * ANSWER, never for ones that answer badly.
 *
 * Returns any non-fatal warning to surface, or null.
 */
export function checkHealth({ health, run, binTarget, expectedSha, provenanceRequired }) {
	if (health.status !== "ok") fail(`healthz reports status=${JSON.stringify(health.status)}, want ok`);
	if (health.pid !== run.pid) fail(`healthz pid ${health.pid} != run-file pid ${run.pid}`);
	if (health.executablePath !== binTarget) fail(`responder is ${health.executablePath}, not ${binTarget}`);

	const revision = health.buildRevision;
	const modified = health.buildModified;

	// Checked first and unconditionally: a daemon that says its tree was dirty is
	// refused even on the relaxed rollback path.
	if (modified === true) fail("responder reports a modified build; the deployed tree was not clean");

	// Three distinct failures kept distinct: a daemon too old to report at all, a
	// build whose provenance was never recorded, and the wrong commit send an
	// operator to three different places.
	if (revision === undefined || revision === null) {
		if (provenanceRequired) fail("responder predates buildRevision; it cannot prove which commit it is");
		return `responder predates buildRevision; provenance unverified against ${expectedSha}`;
	}
	if (revision === "unknown") {
		fail(
			"responder reports an unstamped build (no VCS metadata at build time, or -buildvcs=false); it cannot prove which commit it is",
		);
	}
	if (revision !== expectedSha) fail(`responder was built from ${revision}, not the expected ${expectedSha}`);
	if (modified !== false) fail(`responder does not report a clean build of ${revision} (buildModified=${modified})`);
	return null;
}

// CLI: <run-file> <bin-target> <expected-sha> <provenance-required 1|0>
// Prints the port on stdout and nothing else; warnings and diagnoses go to
// stderr. deploy.sh treats a bare-numeric stdout as the only success.
if (import.meta.url === `file://${process.argv[1]}`) {
	const [runFile, binTarget, expectedSha, provenanceRequired] = process.argv.slice(2);
	try {
		const run = JSON.parse(await readFile(runFile, "utf8"));
		const response = await fetch(`http://127.0.0.1:${run.port}/healthz`, {
			signal: AbortSignal.timeout(3000),
		});
		if (!response.ok) fail(`healthz returned HTTP ${response.status}`);
		const warning = checkHealth({
			health: await response.json(),
			run,
			binTarget,
			expectedSha,
			provenanceRequired: provenanceRequired === "1",
		});
		if (warning) process.stderr.write(`WARN: ${warning}\n`);
		process.stdout.write(String(run.port));
	} catch (err) {
		process.stderr.write(`${err instanceof GateError ? err.message : String(err)}\n`);
		process.exit(1);
	}
}
