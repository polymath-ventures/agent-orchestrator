import { lstat, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, it } from "node:test";

import { checkDrift, isValidProject, parseArgs, refreshSnapshot, trackedProjects } from "./config-drift-check.mjs";

let dir;
const cleanup = [];

beforeEach(async () => {
	dir = await mkdtemp(path.join(os.tmpdir(), "config-drift-"));
	cleanup.push(() => rm(dir, { recursive: true, force: true }));
});

afterEach(async () => {
	await Promise.all(cleanup.splice(0).map((fn) => fn()));
});

async function seedSnapshot(project, body = "{}\n") {
	await writeFile(path.join(dir, `${project}.json`), body);
}

// A fake `run` that returns a scripted result keyed by project, and records
// every invocation so tests can assert exactly what was called.
function fakeRun(script) {
	const calls = [];
	const run = async (args) => {
		calls.push(args);
		const project = args[3];
		const scripted = script[project] ?? { code: 0, stdout: "", stderr: "" };
		if (typeof scripted === "function") return scripted(args);
		return scripted;
	};
	run.calls = calls;
	return run;
}

describe("trackedProjects", () => {
	it("is exactly the *.json files in the snapshot dir, sorted, and nothing else", async () => {
		await seedSnapshot("beta");
		await seedSnapshot("alpha");
		await writeFile(path.join(dir, "README.md"), "not a snapshot");
		await writeFile(path.join(dir, "notes.txt"), "not a snapshot");

		const projects = await trackedProjects(dir);
		assert.deepEqual(
			projects.map((p) => p.project),
			["alpha", "beta"],
		);
		for (const p of projects) {
			assert.equal(p.file, path.join(dir, `${p.project}.json`));
		}
	});

	it("returns empty for a dir with no snapshots (and does not throw on a missing dir)", async () => {
		assert.deepEqual(await trackedProjects(path.join(dir, "does-not-exist")), []);
	});

	it("skips flag-like or path-unsafe snapshot names so they can't smuggle a CLI flag", async () => {
		await seedSnapshot("alpha");
		await writeFile(path.join(dir, "--help.json"), "{}\n");
		await writeFile(path.join(dir, "..json"), "{}\n");

		const projects = await trackedProjects(dir);
		assert.deepEqual(
			projects.map((p) => p.project),
			["alpha"],
		);
	});
});

describe("isValidProject / parseArgs", () => {
	it("mirrors the daemon project-id grammar (rejects flag-like, path-unsafe, whitespace)", () => {
		assert.equal(isValidProject("alpha"), true);
		assert.equal(isValidProject("my-project_1.2"), true);
		assert.equal(isValidProject("--help"), false);
		assert.equal(isValidProject("-x"), false);
		assert.equal(isValidProject("a/b"), false);
		assert.equal(isValidProject("a\\b"), false);
		assert.equal(isValidProject(".."), false);
		assert.equal(isValidProject("."), false);
		assert.equal(isValidProject("a..b"), false);
		assert.equal(isValidProject(".hidden"), false);
		assert.equal(isValidProject("a b"), false);
		assert.equal(isValidProject("a\tb"), false);
		assert.equal(isValidProject(""), false);
	});

	it("accepts only [] or [--refresh, <valid project>] and rejects the rest", () => {
		assert.deepEqual(parseArgs([]), { mode: "check" });
		assert.deepEqual(parseArgs(["--refresh", "alpha"]), { mode: "refresh", project: "alpha" });
		// `=` form, missing project, extra args, and flag-like project all fail closed:
		assert.equal(parseArgs(["--refresh=alpha"]).mode, "usage");
		assert.equal(parseArgs(["--refresh"]).mode, "usage");
		assert.equal(parseArgs(["--refresh", "alpha", "beta"]).mode, "usage");
		assert.equal(parseArgs(["--refresh", "--help"]).mode, "usage");
		assert.equal(parseArgs(["bogus"]).mode, "usage");
	});
});

describe("checkDrift", () => {
	it("exits zero and reports every project in sync when all diffs pass", async () => {
		await seedSnapshot("alpha");
		await seedSnapshot("beta");
		const run = fakeRun({ alpha: { code: 0 }, beta: { code: 0 } });

		const { exitCode, results } = await checkDrift({ snapshotDir: dir, run });

		assert.equal(exitCode, 0);
		assert.deepEqual(
			results.map((r) => r.status),
			["ok", "ok"],
		);
	});

	it("invokes `project config diff <project> <snapshot>` once per snapshot", async () => {
		await seedSnapshot("alpha");
		await seedSnapshot("beta");
		const run = fakeRun({});

		await checkDrift({ snapshotDir: dir, run });

		assert.equal(run.calls.length, 2);
		assert.deepEqual(run.calls[0], ["project", "config", "diff", "alpha", path.join(dir, "alpha.json")]);
		assert.deepEqual(run.calls[1], ["project", "config", "diff", "beta", path.join(dir, "beta.json")]);
	});

	it("exits nonzero, names each drifted project, and carries the diff output", async () => {
		await seedSnapshot("alpha");
		await seedSnapshot("beta");
		const run = fakeRun({
			alpha: { code: 0 },
			beta: { code: 1, stdout: "maxConcurrency: spec=2 live=5\n" },
		});

		const { exitCode, results } = await checkDrift({ snapshotDir: dir, run });

		assert.notEqual(exitCode, 0);
		const beta = results.find((r) => r.project === "beta");
		assert.equal(beta.status, "drift");
		assert.match(beta.detail, /maxConcurrency/);
		const alpha = results.find((r) => r.project === "alpha");
		assert.equal(alpha.status, "ok");
	});

	it("checks every project even when an early one drifts (aggregate, not fail-fast)", async () => {
		await seedSnapshot("alpha");
		await seedSnapshot("beta");
		await seedSnapshot("gamma");
		const run = fakeRun({
			alpha: { code: 1, stdout: "drift\n" },
			beta: { code: 0 },
			gamma: { code: 1, stdout: "drift\n" },
		});

		const { exitCode, results } = await checkDrift({ snapshotDir: dir, run });

		assert.notEqual(exitCode, 0);
		assert.equal(run.calls.length, 3);
		assert.deepEqual(
			results.map((r) => `${r.project}:${r.status}`),
			["alpha:drift", "beta:ok", "gamma:drift"],
		);
	});

	it("reports a setup/usage error (exit 2) distinctly from drift, and aggregates to exit 2", async () => {
		await seedSnapshot("alpha");
		const run = fakeRun({ alpha: { code: 2, stderr: "usage error\n" } });

		const { exitCode, results } = await checkDrift({ snapshotDir: dir, run });

		assert.equal(exitCode, 2);
		assert.equal(results[0].status, "error");
		assert.notEqual(results[0].status, "drift");
	});

	it("lets genuine drift (exit 1) win over a setup/infra error (exit 2) in the aggregate", async () => {
		await seedSnapshot("alpha");
		await seedSnapshot("beta");
		const run = fakeRun({
			alpha: { code: 2, stderr: "usage error\n" },
			beta: { code: 1, stdout: "drift\n" },
		});

		const { exitCode } = await checkDrift({ snapshotDir: dir, run });

		assert.equal(exitCode, 1);
	});

	it("catches a run() rejection (e.g. ao missing) as a per-project error and keeps going", async () => {
		await seedSnapshot("alpha");
		await seedSnapshot("beta");
		const run = fakeRun({
			alpha: () => Promise.reject(new Error("spawn ao ENOENT")),
			beta: { code: 0 },
		});

		const { exitCode, results } = await checkDrift({ snapshotDir: dir, run });

		// alpha errored, but beta was still checked; error-only-vs-clean aggregates to exit 2.
		assert.equal(run.calls.length, 2);
		assert.equal(exitCode, 2);
		const alpha = results.find((r) => r.project === "alpha");
		assert.equal(alpha.status, "error");
		assert.match(alpha.detail, /ENOENT/);
		assert.equal(results.find((r) => r.project === "beta").status, "ok");
	});

	it("never invokes apply and never mutates a snapshot file", async () => {
		await seedSnapshot("alpha", '{"before":true}\n');
		const run = fakeRun({ alpha: { code: 1, stdout: "drift\n" } });

		await checkDrift({ snapshotDir: dir, run });

		for (const call of run.calls) {
			assert.equal(call[2], "diff");
			assert.notEqual(call[2], "apply");
		}
		assert.equal(await readFile(path.join(dir, "alpha.json"), "utf8"), '{"before":true}\n');
	});

	it("exits 0 with no results for a present-but-empty snapshot dir (legit bootstrap state)", async () => {
		const run = fakeRun({});
		const { exitCode, results } = await checkDrift({ snapshotDir: dir, run });
		assert.equal(exitCode, 0);
		assert.equal(results.length, 0);
		assert.equal(run.calls.length, 0);
	});

	it("surfaces a MISSING snapshot dir as an error (exit 2), not a silent clean run", async () => {
		const run = fakeRun({});
		const { exitCode, results } = await checkDrift({ snapshotDir: path.join(dir, "nope"), run });
		assert.equal(exitCode, 2);
		assert.equal(results.length, 1);
		assert.equal(results[0].status, "error");
		assert.match(results[0].detail, /does not exist/);
	});

	it("surfaces an invalid-named or non-regular *.json entry as an error, never a silent skip or a CLI arg", async () => {
		await seedSnapshot("alpha");
		await writeFile(path.join(dir, "--help.json"), "{}\n"); // invalid project id
		const target = path.join(dir, "beta-data");
		await writeFile(target, "{}\n");
		await symlink(target, path.join(dir, "beta.json")); // non-regular entry
		const run = fakeRun({ alpha: { code: 0 } });

		const { exitCode, results } = await checkDrift({ snapshotDir: dir, run });

		assert.equal(exitCode, 2);
		// alpha (the only valid regular snapshot) is the only thing diff runs on.
		assert.deepEqual(
			run.calls.map((c) => c[3]),
			["alpha"],
		);
		const errors = results.filter((r) => r.status === "error").map((r) => r.project);
		assert.ok(errors.includes("--help.json"), "invalid name surfaced");
		assert.ok(errors.includes("beta.json"), "symlink surfaced");
		assert.equal(results.find((r) => r.project === "alpha").status, "ok");
	});
});

describe("refreshSnapshot", () => {
	it("writes the fresh export to the snapshot file so a later diff is in sync", async () => {
		await seedSnapshot("alpha", '{"stale":true}\n');
		const exported = '{"fresh":true}\n';
		const run = fakeRun({ alpha: { code: 0, stdout: exported } });

		const result = await refreshSnapshot({ snapshotDir: dir, project: "alpha", run });

		assert.equal(result.changed, true);
		assert.equal(run.calls[0][2], "export");
		assert.equal(await readFile(path.join(dir, "alpha.json"), "utf8"), exported);
	});

	it("leaves the file byte-unchanged when live config already matches (no spurious diff)", async () => {
		const current = '{"same":true}\n';
		await seedSnapshot("alpha", current);
		const run = fakeRun({ alpha: { code: 0, stdout: current } });

		const result = await refreshSnapshot({ snapshotDir: dir, project: "alpha", run });

		assert.equal(result.changed, false);
		assert.equal(await readFile(path.join(dir, "alpha.json"), "utf8"), current);
	});

	it("never invokes apply (refresh is export-only, no live-config mutation)", async () => {
		await seedSnapshot("alpha");
		const run = fakeRun({ alpha: { code: 0, stdout: "{}\n" } });

		await refreshSnapshot({ snapshotDir: dir, project: "alpha", run });

		for (const call of run.calls) {
			assert.notEqual(call[2], "apply");
		}
	});

	it("surfaces a failed export as an error and does not overwrite the snapshot", async () => {
		await seedSnapshot("alpha", '{"kept":true}\n');
		const run = fakeRun({ alpha: { code: 1, stderr: "daemon down\n" } });

		await assert.rejects(refreshSnapshot({ snapshotDir: dir, project: "alpha", run }), /export failed/i);
		assert.equal(await readFile(path.join(dir, "alpha.json"), "utf8"), '{"kept":true}\n');
	});

	it("writes atomically and leaves no temp file behind (tracked set unchanged)", async () => {
		await seedSnapshot("alpha", '{"stale":true}\n');
		const run = fakeRun({ alpha: { code: 0, stdout: '{"fresh":true}\n' } });

		await refreshSnapshot({ snapshotDir: dir, project: "alpha", run });

		// No `.alpha.json.<pid>.tmp` leaked, and enumeration still sees exactly alpha.
		assert.deepEqual(
			(await trackedProjects(dir)).map((p) => p.project),
			["alpha"],
		);
	});

	it("flags a non-empty env block (or an unparseable export) as secret-bearing to warn before commit", async () => {
		await seedSnapshot("alpha");
		const withSecret = fakeRun({ alpha: { code: 0, stdout: '{"env":{"TOKEN":"abc"}}\n' } });
		const withoutSecret = fakeRun({ alpha: { code: 0, stdout: '{"env":{}}\n' } });
		const notJson = fakeRun({ alpha: { code: 0, stdout: "not json at all\n" } });

		assert.equal((await refreshSnapshot({ snapshotDir: dir, project: "alpha", run: withSecret })).hasSecrets, true);
		assert.equal((await refreshSnapshot({ snapshotDir: dir, project: "alpha", run: withoutSecret })).hasSecrets, false);
		// Fail safe: if the export is not JSON we cannot rule out a secret, so warn.
		assert.equal((await refreshSnapshot({ snapshotDir: dir, project: "alpha", run: notJson })).hasSecrets, true);
	});

	it("normalizes a symlink snapshot into a regular file so it can't become an unchecked false-green", async () => {
		// A snapshot that is a symlink to matching content would be silently skipped
		// by trackedProjects (regular-files-only). Refresh must replace it.
		const body = '{"name":"alpha"}\n';
		const target = path.join(dir, "elsewhere.json.data");
		await writeFile(target, body);
		await symlink(target, path.join(dir, "alpha.json"));
		// Before refresh: the symlinked snapshot is not tracked.
		assert.deepEqual(
			(await trackedProjects(dir)).map((p) => p.project),
			[],
		);

		const run = fakeRun({ alpha: { code: 0, stdout: body } });
		const result = await refreshSnapshot({ snapshotDir: dir, project: "alpha", run });

		assert.equal(result.changed, true);
		const st = await lstat(path.join(dir, "alpha.json"));
		assert.equal(st.isSymbolicLink(), false);
		assert.equal(st.isFile(), true);
		// Now it is a tracked regular-file snapshot with the right content.
		assert.deepEqual(
			(await trackedProjects(dir)).map((p) => p.project),
			["alpha"],
		);
		assert.equal(await readFile(path.join(dir, "alpha.json"), "utf8"), body);
	});

	it("rejects an invalid project id without invoking the CLI", async () => {
		const run = fakeRun({});
		await assert.rejects(refreshSnapshot({ snapshotDir: dir, project: "--help", run }), /invalid project/i);
		assert.equal(run.calls.length, 0);
	});
});
