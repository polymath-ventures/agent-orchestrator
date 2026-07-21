#!/usr/bin/env node
// Fork-only convenience layer over `ao project config` (#14/#42): keep each
// tracked project's exported config as a committed snapshot, and flag drift
// between that snapshot and live config on a schedule. Drift is SURFACED to the
// operator (nonzero exit + report); it is never auto-applied. See
// openspec/changes/add-committed-config-drift-check.
//
// The set of `<project>.json` files in the snapshot dir IS the tracked-project
// registry (no separate list). Drift detection delegates entirely to
// `ao project config diff`; refresh delegates to `ao project config export`.

import { spawn } from "node:child_process";
import { lstat, readdir, readFile, rename, unlink, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const DEFAULT_SNAPSHOT_DIR = path.join(HERE, "project-config");

// Mirror the daemon's authoritative project-id grammar
// (backend/internal/service/project/service.go: projectIDPattern + the "." /
// ".." / separator rejections). Keeping the same grammar means a name that the
// daemon would accept is the only name treated as a project here, and a stray
// file like `--help.json` (leading `-`), a name with whitespace/control chars,
// or a path-traversal name can never smuggle a flag into the CLI or escape the
// snapshot dir and produce a false "in sync".
const PROJECT_ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
export function isValidProject(project) {
	return (
		typeof project === "string" &&
		project !== "" &&
		project !== "." &&
		!project.includes("..") &&
		!project.includes("/") &&
		!project.includes("\\") &&
		PROJECT_ID_PATTERN.test(project)
	);
}

// listSnapshotEntries classifies every `*.json` entry in the snapshot dir into
// `projects` (valid, regular-file snapshots to check) and `anomalies` (a `*.json`
// entry that is not a usable snapshot: an invalid project-id name that could not
// be passed to the CLI, or a non-regular file such as a symlink). `dirMissing`
// is true when the directory itself does not exist. Anomalies and a missing dir
// are surfaced by checkDrift as errors rather than silently dropped, so a
// misconfigured or tampered snapshot set can never read as a clean "no drift".
export async function listSnapshotEntries(snapshotDir) {
	let entries;
	try {
		entries = await readdir(snapshotDir, { withFileTypes: true });
	} catch (err) {
		if (err.code === "ENOENT") return { projects: [], anomalies: [], dirMissing: true };
		throw err;
	}
	const projects = [];
	const anomalies = [];
	for (const e of entries) {
		if (!e.name.endsWith(".json")) continue;
		const project = e.name.slice(0, -".json".length);
		if (!isValidProject(project)) {
			anomalies.push({ name: e.name, reason: "not a valid project id" });
		} else if (!e.isFile()) {
			anomalies.push({ name: e.name, reason: "not a regular file (e.g. symlink)" });
		} else {
			projects.push({ project, file: path.join(snapshotDir, e.name) });
		}
	}
	projects.sort((a, b) => a.project.localeCompare(b.project));
	anomalies.sort((a, b) => a.name.localeCompare(b.name));
	return { projects, anomalies, dirMissing: false };
}

// trackedProjects returns just the valid, regular-file snapshots (used by
// refresh to resolve the project set). Enumeration anomalies are surfaced by
// checkDrift, not here.
export async function trackedProjects(snapshotDir) {
	return (await listSnapshotEntries(snapshotDir)).projects;
}

// checkDrift runs `ao project config diff <project> <snapshot>` for every tracked
// snapshot and aggregates. `run(args)` executes the ao CLI and resolves
// { code, stdout, stderr }. Per-project exit-code contract (from `config diff`):
//   0        -> in sync
//   2        -> setup/usage error (reported distinctly, not as drift)
//   other    -> drift OR an infra failure (daemon down); both warrant a look.
// A `run()` rejection (e.g. `ao` missing -> spawn ENOENT) is caught and recorded
// as a per-project error so enumeration still covers every project (aggregate,
// never fail-fast). Only `diff` is ever invoked — never `apply`, and no snapshot
// file is written.
//
// Aggregate exit code preserves the CLI's exit-code meaning: any genuine drift
// -> 1; otherwise any setup/infra error -> 2; all in sync -> 0. So a scheduled
// run distinguishes "config actually drifted" (1) from "the check itself is
// misconfigured" (2) from "clean" (0).
export async function checkDrift({ snapshotDir, run }) {
	const { projects, anomalies, dirMissing } = await listSnapshotEntries(snapshotDir);
	const results = [];
	if (dirMissing) {
		results.push({
			project: snapshotDir,
			status: "error",
			code: null,
			detail: "snapshot directory does not exist",
		});
	}
	// A `*.json` entry that is not a usable snapshot is an error, not a silent
	// skip — otherwise a symlinked or mis-named snapshot would go unchecked and
	// the run would falsely read as clean.
	for (const { name, reason } of anomalies) {
		results.push({ project: name, status: "error", code: null, detail: reason });
	}
	for (const { project, file } of projects) {
		let code;
		let stdout = "";
		let stderr = "";
		try {
			({ code, stdout = "", stderr = "" } = await run(["project", "config", "diff", project, file]));
		} catch (err) {
			results.push({ project, status: "error", code: null, detail: `invocation failed: ${err.message}` });
			continue;
		}
		if (code === 0) {
			results.push({ project, status: "ok", code });
		} else if (code === 2) {
			results.push({ project, status: "error", code, detail: stderr.trim() });
		} else {
			results.push({ project, status: "drift", code, detail: (stdout || stderr).trim() });
		}
	}
	const hasDrift = results.some((r) => r.status === "drift");
	const hasError = results.some((r) => r.status === "error");
	const exitCode = hasDrift ? 1 : hasError ? 2 : 0;
	return { exitCode, results };
}

// refreshSnapshot regenerates one project's snapshot from a fresh
// `ao project config export`. It writes only when the export differs from the
// current file (no spurious git diff), writes atomically (temp file + rename, so
// a concurrent drift check never sees a torn snapshot and an existing symlink at
// the path is replaced rather than followed), never invokes `apply`, and never
// mutates the snapshot when the export fails. Returns `hasSecrets` when the
// exported config carries a non-empty `env` block so the caller can warn before
// the operator commits credentials to git.
export async function refreshSnapshot({ snapshotDir, project, run }) {
	if (!isValidProject(project)) {
		throw new Error(`invalid project id: ${JSON.stringify(project)}`);
	}
	const file = path.join(snapshotDir, `${project}.json`);
	const { code, stdout = "", stderr = "" } = await run(["project", "config", "export", project]);
	if (code !== 0) {
		throw new Error(`export failed for project ${project} (exit ${code})${stderr.trim() ? `: ${stderr.trim()}` : ""}`);
	}
	const hasSecrets = exportCarriesSecrets(stdout);
	let current = null;
	let isRegularFile = false;
	try {
		const st = await lstat(file);
		isRegularFile = st.isFile();
		if (isRegularFile) current = await readFile(file, "utf8");
	} catch (err) {
		if (err.code !== "ENOENT") throw err;
	}
	// Only skip the write when the existing entry is a regular file that already
	// matches. A symlink (or any non-regular entry) is normalized to a regular
	// file via the rename below — otherwise it would linger and be silently
	// skipped by trackedProjects (which only counts regular files), a false-green.
	if (isRegularFile && current === stdout) return { project, changed: false, hasSecrets };
	const tmp = path.join(snapshotDir, `.${project}.json.${process.pid}.tmp`);
	try {
		await writeFile(tmp, stdout, { mode: 0o644, flag: "wx" });
		await rename(tmp, file);
	} catch (err) {
		await unlink(tmp).catch(() => {});
		throw err;
	}
	return { project, changed: true, hasSecrets };
}

// exportCarriesSecrets reports whether an exported config JSON has a non-empty
// `env` block — the field the CLI documents as credential-bearing and redacts in
// diff output. A non-JSON export is unexpected, so fail safe and warn: the
// snapshot is going into git either way and we cannot rule out a secret.
function exportCarriesSecrets(exported) {
	try {
		const parsed = JSON.parse(exported);
		return Boolean(parsed && typeof parsed.env === "object" && parsed.env && Object.keys(parsed.env).length > 0);
	} catch {
		return true;
	}
}

// defaultRun spawns the real `ao` CLI, buffering stdout/stderr and resolving the
// exit code. Injected in tests so the core logic runs without a real binary.
export function defaultRun(aoBin) {
	return (args) =>
		new Promise((resolve, reject) => {
			const child = spawn(aoBin, args, { stdio: ["ignore", "pipe", "pipe"] });
			let stdout = "";
			let stderr = "";
			child.stdout.on("data", (d) => {
				stdout += d;
			});
			child.stderr.on("data", (d) => {
				stderr += d;
			});
			child.on("error", reject);
			child.on("close", (code) => resolve({ code: code ?? 1, stdout, stderr }));
		});
}

// parseArgs enforces the runner's tiny, strict CLI grammar: exactly `[]` (check
// all snapshots) or `["--refresh", <valid-project>]`. Everything else — an `=`
// form like `--refresh=alpha`, a missing/invalid project, or any stray argument
// — is a usage error, so a malformed invocation can never be silently
// misinterpreted as a clean check.
export function parseArgs(argv) {
	if (argv.length === 0) return { mode: "check" };
	if (argv[0] === "--refresh" && argv.length === 2 && isValidProject(argv[1])) {
		return { mode: "refresh", project: argv[1] };
	}
	return { mode: "usage" };
}

function formatReport(results) {
	const lines = [];
	for (const r of results) {
		if (r.status === "ok") {
			lines.push(`  ok    ${r.project}`);
		} else if (r.status === "drift") {
			lines.push(`  DRIFT ${r.project}`);
			for (const line of (r.detail || "").split("\n").filter(Boolean)) {
				lines.push(`          ${line}`);
			}
		} else {
			lines.push(`  ERROR ${r.project}${r.code != null ? ` (exit ${r.code})` : ""}`);
			for (const line of (r.detail || "").split("\n").filter(Boolean)) {
				lines.push(`          ${line}`);
			}
		}
	}
	return lines.join("\n");
}

async function main(argv) {
	const aoBin = process.env.AO_BIN || "ao";
	const snapshotDir = DEFAULT_SNAPSHOT_DIR;
	const run = defaultRun(aoBin);

	const parsed = parseArgs(argv);
	if (parsed.mode === "usage") {
		process.stderr.write("usage: config-drift-check.mjs [--refresh <project>]\n");
		return 2;
	}

	if (parsed.mode === "refresh") {
		const { changed, hasSecrets } = await refreshSnapshot({ snapshotDir, project: parsed.project, run });
		if (hasSecrets) {
			process.stderr.write(
				`warning: snapshot for ${parsed.project} contains a non-empty 'env' block that may hold credentials; ` +
					`review before committing it to git (see ops/project-config/README.md)\n`,
			);
		}
		process.stdout.write(
			changed
				? `refreshed snapshot for ${parsed.project}\n`
				: `snapshot for ${parsed.project} already in sync (unchanged)\n`,
		);
		return 0;
	}

	const { exitCode, results } = await checkDrift({ snapshotDir, run });
	if (results.length === 0) {
		process.stdout.write(`no committed project snapshots under ${snapshotDir}\n`);
		return 0;
	}
	process.stdout.write(`${formatReport(results)}\n`);
	if (exitCode !== 0) {
		const flagged = results.filter((r) => r.status !== "ok").map((r) => r.project);
		process.stderr.write(`config drift check: ${flagged.length} project(s) need attention: ${flagged.join(", ")}\n`);
	}
	return exitCode;
}

function isMainModule(moduleUrl) {
	const entry = process.argv[1];
	if (!entry) return false;
	try {
		return fileURLToPath(moduleUrl) === path.resolve(entry);
	} catch {
		return false;
	}
}

if (isMainModule(import.meta.url)) {
	main(process.argv.slice(2))
		.then((code) => {
			process.exitCode = code;
		})
		.catch((err) => {
			process.stderr.write(`${err.stack || err}\n`);
			process.exitCode = 1;
		});
}
