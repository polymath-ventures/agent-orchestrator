import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, statSync, utimesSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel) => readFileSync(join(root, rel), "utf8");

test("package scripts expose the repo-owned agent-ci entrypoints", () => {
	const pkg = JSON.parse(read("package.json"));
	assert.equal(pkg.scripts["agent-ci"], "bash scripts/ci/agent-ci.sh");
	assert.equal(pkg.scripts["agent-ci:clean"], "bash scripts/ci/agent-ci-clean.sh");
});

test("agent-ci wrapper defaults durable state outside /tmp and honors overrides", () => {
	const src = read("scripts/ci/agent-ci.sh");
	assert.match(src, /AGENT_CI_WORKING_DIR="\$\{AGENT_CI_WORKING_DIR:-\$default_cache_home\/agent-ci\/\$repo_slug\}"/);
	assert.match(src, /default_cache_home="\$\{XDG_CACHE_HOME:-\$HOME\/\.cache\}"/);
	assert.doesNotMatch(src, /AGENT_CI_WORKING_DIR=.*\/tmp/);
	assert.match(src, /set -- run --all/);
});

test("agent-ci cleanup is dry-run by default and refuses tmp roots", () => {
	const src = read("scripts/ci/agent-ci-clean.sh");
	assert.match(src, /mode=dry-run/);
	assert.match(src, /\/tmp/);
	assert.match(src, /refusing to clean unsafe agent-ci workdir/);
	assert.match(src, /--docker-root-helper/);
	assert.match(src, /docker run --rm -v "\$workdir:\/workdir"/);
});

test("agent-ci cleanup selects stale state while preserving paused and recent runs", () => {
	const workdir = mkdtempSync(join(root, ".cache-test-agent-ci-clean-"));
	try {
		const staleRun = join(workdir, "runs", "agent-ci-1-j1");
		const recentRun = join(workdir, "runs", "agent-ci-2-j1");
		const pausedRun = join(workdir, "runs", "agent-ci-3-j1");
		const staleToolcache = join(workdir, "cache", "toolcache");
		mkdirSync(staleRun, { recursive: true });
		mkdirSync(recentRun, { recursive: true });
		mkdirSync(join(pausedRun, "signals"), { recursive: true });
		mkdirSync(staleToolcache, { recursive: true });
		mkdirSync(join(pausedRun, "signals", "paused"), { recursive: true });

		const old = new Date(Date.now() - 16 * 24 * 60 * 60 * 1000);
		utimesSync(staleRun, old, old);
		utimesSync(staleToolcache, old, old);

		const result = spawnSync("bash", ["scripts/ci/agent-ci-clean.sh", "--dry-run", "--older-than", "14"], {
			cwd: root,
			env: { ...process.env, AGENT_CI_WORKING_DIR: workdir },
			encoding: "utf8",
		});

		assert.equal(result.status, 0, result.stderr);
		assert.match(result.stdout, /selected for cleanup:/);
		assert.match(result.stdout, /agent-ci-1-j1/);
		assert.match(result.stdout, /cache\/toolcache/);
		assert.match(result.stdout, /agent-ci-2-j1 \(recent\)/);
		assert.match(result.stdout, /agent-ci-3-j1 \(paused retry state\)/);
		assert.ok(statSync(staleRun).isDirectory(), "dry-run must not delete stale run");
	} finally {
		rmSync(workdir, { recursive: true, force: true });
	}
});

test("agent-ci cleanup force removes only selected stale paths", () => {
	const workdir = mkdtempSync(join(root, ".cache-test-agent-ci-clean-"));
	try {
		const staleRun = join(workdir, "runs", "agent-ci-1-j1");
		const pausedRun = join(workdir, "runs", "agent-ci-2-j1");
		mkdirSync(staleRun, { recursive: true });
		mkdirSync(join(pausedRun, "signals", "paused"), { recursive: true });

		const old = new Date(Date.now() - 16 * 24 * 60 * 60 * 1000);
		utimesSync(staleRun, old, old);
		utimesSync(pausedRun, old, old);

		const result = spawnSync("bash", ["scripts/ci/agent-ci-clean.sh", "--force", "--older-than", "14"], {
			cwd: root,
			env: { ...process.env, AGENT_CI_WORKING_DIR: workdir },
			encoding: "utf8",
		});

		assert.equal(result.status, 0, result.stderr);
		assert.match(result.stdout, /deleted: .*agent-ci-1-j1/);
		assert.throws(() => statSync(staleRun));
		assert.ok(statSync(pausedRun).isDirectory(), "paused retry state must be preserved");
	} finally {
		rmSync(workdir, { recursive: true, force: true });
	}
});
