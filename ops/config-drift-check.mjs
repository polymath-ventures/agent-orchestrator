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
import { readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const DEFAULT_SNAPSHOT_DIR = path.join(HERE, "project-config");

// trackedProjects enumerates the snapshot dir: exactly the `*.json` files,
// sorted by project id. A missing dir yields no projects rather than throwing.
export async function trackedProjects(snapshotDir) {
	let entries;
	try {
		entries = await readdir(snapshotDir, { withFileTypes: true });
	} catch (err) {
		if (err.code === "ENOENT") return [];
		throw err;
	}
	return entries
		.filter((e) => e.isFile() && e.name.endsWith(".json"))
		.map((e) => ({
			project: e.name.slice(0, -".json".length),
			file: path.join(snapshotDir, e.name),
		}))
		.sort((a, b) => a.project.localeCompare(b.project));
}

// checkDrift runs `ao project config diff <project> <snapshot>` for every
// tracked snapshot and aggregates. `run(args)` executes the ao CLI and resolves
// { code, stdout, stderr }. Exit-code contract (from `config diff`):
//   0        -> in sync
//   2        -> setup/usage error (reported distinctly, not as drift)
//   other    -> drift OR an infra failure (daemon down); both warrant a look, so
//               they are surfaced together as attention-worthy "drift".
// Every project is checked before returning (aggregate, never fail-fast), and
// only `diff` is ever invoked — never `apply`, and no snapshot file is written.
export async function checkDrift({ snapshotDir, run }) {
	const projects = await trackedProjects(snapshotDir);
	const results = [];
	for (const { project, file } of projects) {
		const { code, stdout = "", stderr = "" } = await run(["project", "config", "diff", project, file]);
		if (code === 0) {
			results.push({ project, status: "ok", code });
		} else if (code === 2) {
			results.push({ project, status: "error", code, detail: stderr.trim() });
		} else {
			results.push({
				project,
				status: "drift",
				code,
				detail: (stdout || stderr).trim(),
			});
		}
	}
	const exitCode = results.some((r) => r.status !== "ok") ? 1 : 0;
	return { exitCode, results };
}

// refreshSnapshot regenerates one project's snapshot from a fresh
// `ao project config export`. It writes only when the export differs from the
// current file (no spurious git diff), never invokes `apply`, and never mutates
// the snapshot when the export fails.
export async function refreshSnapshot({ snapshotDir, project, run }) {
	const file = path.join(snapshotDir, `${project}.json`);
	const { code, stdout = "", stderr = "" } = await run(["project", "config", "export", project]);
	if (code !== 0) {
		throw new Error(`export failed for project ${project} (exit ${code})${stderr.trim() ? `: ${stderr.trim()}` : ""}`);
	}
	let current = null;
	try {
		current = await readFile(file, "utf8");
	} catch (err) {
		if (err.code !== "ENOENT") throw err;
	}
	if (current === stdout) return { project, changed: false };
	await writeFile(file, stdout);
	return { project, changed: true };
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
			lines.push(`  ERROR ${r.project} (exit ${r.code})`);
			for (const line of (r.detail || "").split("\n").filter(Boolean)) {
				lines.push(`          ${line}`);
			}
		}
	}
	return lines.join("\n");
}

async function main(argv) {
	const aoBin = process.env.AO_BIN || "ao";
	const snapshotDir = process.env.AO_CONFIG_SNAPSHOT_DIR || DEFAULT_SNAPSHOT_DIR;
	const run = defaultRun(aoBin);

	const refreshIdx = argv.indexOf("--refresh");
	if (refreshIdx !== -1) {
		const project = argv[refreshIdx + 1];
		if (!project) {
			process.stderr.write("usage: config-drift-check.mjs --refresh <project>\n");
			return 2;
		}
		const { changed } = await refreshSnapshot({ snapshotDir, project, run });
		process.stdout.write(
			changed ? `refreshed snapshot for ${project}\n` : `snapshot for ${project} already in sync (unchanged)\n`,
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
