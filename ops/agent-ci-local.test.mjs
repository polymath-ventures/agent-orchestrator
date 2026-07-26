import test from "node:test";
import assert from "node:assert/strict";
import {
	chmodSync,
	existsSync,
	mkdtempSync,
	mkdirSync,
	readFileSync,
	rmSync,
	statSync,
	utimesSync,
	writeFileSync,
} from "node:fs";
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
	assert.match(src, /git rev-parse --git-common-dir/);
	assert.match(src, /AGENT_CI_WORKING_DIR="\$\{AGENT_CI_WORKING_DIR:-\$default_cache_home\/agent-ci\/\$repo_slug\}"/);
	assert.match(src, /default_cache_home="\$\{XDG_CACHE_HOME:-\$HOME\/\.cache\}"/);
	assert.doesNotMatch(src, /AGENT_CI_WORKING_DIR=.*\/tmp/);
	assert.match(src, /set -- run --all/);
});

test("agent-ci wrapper uses the repo slug from plain checkouts and subdirectories", () => {
	const scratch = mkdtempSync(join(root, ".cache-test-agent-ci-clean-"));
	try {
		const repo = join(scratch, "plain-repo");
		const subdir = join(repo, "nested");
		const fakeBin = join(scratch, "bin");
		const cacheHome = join(scratch, "cache-home");
		mkdirSync(subdir, { recursive: true });
		mkdirSync(fakeBin, { recursive: true });
		const gitInit = spawnSync("git", ["init", "--quiet", repo], { encoding: "utf8" });
		assert.equal(gitInit.status, 0, gitInit.stderr);

		const fakeNpx = join(fakeBin, "npx");
		writeFileSync(fakeNpx, "#!/usr/bin/env bash\nprintf '%s\\n' \"$AGENT_CI_WORKING_DIR\"\nprintf '%s\\n' \"$*\"\n");
		chmodSync(fakeNpx, 0o755);

		const result = spawnSync("bash", [join(root, "scripts/ci/agent-ci.sh")], {
			cwd: subdir,
			env: {
				...process.env,
				AGENT_CI_WORKING_DIR: "",
				XDG_CACHE_HOME: cacheHome,
				PATH: `${fakeBin}:${process.env.PATH}`,
			},
			encoding: "utf8",
		});

		assert.equal(result.status, 0, result.stderr);
		assert.match(result.stdout, new RegExp(`${cacheHome.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}/agent-ci/plain-repo`));
		assert.match(result.stdout, /run --all/);
	} finally {
		rmSync(scratch, { recursive: true, force: true });
	}
});

test("agent-ci cleanup is dry-run by default and refuses tmp roots", () => {
	const src = read("scripts/ci/agent-ci-clean.sh");
	assert.match(src, /mode=dry-run/);
	assert.match(src, /git rev-parse --git-common-dir/);
	assert.match(src, /--docker-root-helper/);
	assert.match(src, /docker run --rm --network none -v "\$workdir:\/workdir" alpine:3\.20@sha256:/);

	const refused = "/tmp/ao-agent-ci-clean-refuse-behavior";
	rmSync(refused, { recursive: true, force: true });
	const result = spawnSync("bash", ["scripts/ci/agent-ci-clean.sh", "--dry-run"], {
		cwd: root,
		env: { ...process.env, AGENT_CI_WORKING_DIR: refused },
		encoding: "utf8",
	});

	assert.equal(result.status, 2);
	assert.match(result.stderr, /refusing to clean unsafe agent-ci workdir/);
	assert.equal(existsSync(refused), false, "refused /tmp workdir must not be created");
});

test("agent-ci cleanup default cache slug is stable across task worktrees", () => {
	const cacheHome = mkdtempSync(join(root, ".cache-test-agent-ci-clean-home-"));
	try {
		const result = spawnSync("bash", ["scripts/ci/agent-ci-clean.sh", "--dry-run"], {
			cwd: root,
			env: { ...process.env, XDG_CACHE_HOME: cacheHome, AGENT_CI_WORKING_DIR: "" },
			encoding: "utf8",
		});

		assert.equal(result.status, 0, result.stderr);
		assert.match(result.stdout, /\/agent-ci\/agent-orchestrator\n/);
		assert.doesNotMatch(result.stdout, /\/agent-ci\/issue-169-agent-ci-workdir\n/);
	} finally {
		rmSync(cacheHome, { recursive: true, force: true });
	}
});

test("agent-ci cleanup uses the repo slug from plain checkout subdirectories", () => {
	const scratch = mkdtempSync(join(root, ".cache-test-agent-ci-clean-"));
	try {
		const repo = join(scratch, "plain-repo");
		const subdir = join(repo, "nested");
		const cacheHome = join(scratch, "cache-home");
		mkdirSync(subdir, { recursive: true });
		const gitInit = spawnSync("git", ["init", "--quiet", repo], { encoding: "utf8" });
		assert.equal(gitInit.status, 0, gitInit.stderr);

		const result = spawnSync("bash", [join(root, "scripts/ci/agent-ci-clean.sh"), "--dry-run"], {
			cwd: subdir,
			env: { ...process.env, XDG_CACHE_HOME: cacheHome, AGENT_CI_WORKING_DIR: "" },
			encoding: "utf8",
		});

		assert.equal(result.status, 0, result.stderr);
		assert.match(result.stdout, /agent-ci workdir: .*\/agent-ci\/plain-repo/);
		assert.doesNotMatch(result.stdout, /agent-ci workdir: .*cache-home\n/);
		assert.equal(
			existsSync(join(cacheHome, "agent-ci", "plain-repo")),
			false,
			"dry-run must not create missing workdir",
		);
	} finally {
		rmSync(scratch, { recursive: true, force: true });
	}
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

test("agent-ci cleanup does not treat detached metadata as paused state", () => {
	const workdir = mkdtempSync(join(root, ".cache-test-agent-ci-clean-"));
	try {
		const detachedRun = join(workdir, "runs", "agent-ci-detached-j1");
		mkdirSync(join(detachedRun, "detached.json"), { recursive: true });

		const old = new Date(Date.now() - 16 * 24 * 60 * 60 * 1000);
		utimesSync(detachedRun, old, old);

		const result = spawnSync("bash", ["scripts/ci/agent-ci-clean.sh", "--dry-run", "--older-than", "14"], {
			cwd: root,
			env: { ...process.env, AGENT_CI_WORKING_DIR: workdir },
			encoding: "utf8",
		});

		assert.equal(result.status, 0, result.stderr);
		assert.match(result.stdout, /selected for cleanup:/);
		assert.match(result.stdout, /agent-ci-detached-j1/);
		assert.doesNotMatch(result.stdout, /agent-ci-detached-j1 \(paused retry state\)/);
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

test("agent-ci cleanup preserves cache roots with recent descendant files", () => {
	const workdir = mkdtempSync(join(root, ".cache-test-agent-ci-clean-"));
	try {
		const staleToolcache = join(workdir, "cache", "toolcache");
		const recentDescendant = join(staleToolcache, "node", "bin");
		mkdirSync(recentDescendant, { recursive: true });

		const old = new Date(Date.now() - 16 * 24 * 60 * 60 * 1000);
		utimesSync(staleToolcache, old, old);

		const result = spawnSync("bash", ["scripts/ci/agent-ci-clean.sh", "--dry-run", "--older-than", "14"], {
			cwd: root,
			env: { ...process.env, AGENT_CI_WORKING_DIR: workdir },
			encoding: "utf8",
		});

		assert.equal(result.status, 0, result.stderr);
		assert.match(result.stdout, /cache\/toolcache \(recent cache\)/);
		assert.doesNotMatch(result.stdout, /selected for cleanup:[\s\S]*cache\/toolcache/);
	} finally {
		rmSync(workdir, { recursive: true, force: true });
	}
});
