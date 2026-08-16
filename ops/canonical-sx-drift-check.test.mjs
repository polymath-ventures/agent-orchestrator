import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { FAIL_OPEN_STUBS } from "./canonical-sx-drift-check.mjs";

const scriptPath = fileURLToPath(new URL("./canonical-sx-drift-check.mjs", import.meta.url));
const repoRoot = fileURLToPath(new URL("..", import.meta.url));
const formerVaultSlug = ["agent", "vault"].join("-");
const currentVaultSlug = ["polymath", "agent", "assets"].join("-");
const managedMarker = ["@sx", "managed"].join("-");
function git(cwd, ...args) {
	const result = spawnSync("git", args, { cwd, encoding: "utf8" });
	if (result.status !== 0) throw new Error(`git ${args.join(" ")} failed: ${result.stderr}`);
	return result.stdout;
}

function write(root, rel, contents) {
	const full = join(root, rel);
	mkdirSync(dirname(full), { recursive: true });
	writeFileSync(full, contents);
}

function setupRepo(files) {
	const root = mkdtempSync(join(tmpdir(), "sx-drift-"));
	git(root, "init", "-q");
	git(root, "config", "user.email", "gate@example.com");
	git(root, "config", "user.name", "gate");
	git(root, "config", "commit.gpgsign", "false");
	for (const [rel, contents] of Object.entries(files)) write(root, rel, contents);
	git(root, "add", "--", ...Object.keys(files));
	git(root, "commit", "-qm", "fixture");
	return root;
}

function runGuard(cwd) {
	return spawnSync("node", [scriptPath], { cwd, encoding: "utf8" });
}

test("canonical sx guard allows managed markers under .github", () => {
	const root = setupRepo({
		".github/workflows/one-closing-issue.yml": `# ${managedMarker}: polypowers-init-one-closing-issue\n`,
		".github/scripts/valid-final-review.cjs": `// ${managedMarker}: polypowers-init-valid-final-review\n`,
		"agent-instructions/source/00-product.md": "# Repo-owned product guidance\n",
	});
	try {
		const result = runGuard(root);
		assert.equal(result.status, 0, `expected pass\nstdout:${result.stdout}\nstderr:${result.stderr}`);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects tracked managed files outside .github", () => {
	const root = setupRepo({
		"agent-instructions/README.md": `<!--\n${managedMarker}: agent-instructions-readme\n-->\n`,
	});
	try {
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /tracked sx-managed marker outside \.github/);
		assert.match(result.stderr, /agent-instructions\/README\.md/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects former and current vault slug references", () => {
	const root = setupRepo({
		"docs/old.md": `legacy clone URL mentions ${formerVaultSlug}\n`,
		"docs/current.md": `current slug ${currentVaultSlug} must stay out of repos\n`,
	});
	try {
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /vault slug reference in tracked file/);
		assert.match(result.stderr, /docs\/old\.md/);
		assert.match(result.stderr, /docs\/current\.md/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard checks the repository root when invoked from a subdirectory", () => {
	const root = setupRepo({
		"docs/readme.md": "# Docs\n",
		"agent-instructions/README.md": `${managedMarker}: agent-instructions-readme\n`,
	});
	try {
		const result = runGuard(join(root, "docs"));
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /agent-instructions\/README\.md/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects tracked generated shared instructions", () => {
	const root = setupRepo({
		"AGENTS.shared.md": "generated output\n",
	});
	try {
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /generated agent instruction output is tracked/);
		assert.match(result.stderr, /AGENTS\.shared\.md/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects stale tracked fail-open instruction outputs", () => {
	const root = setupRepo({
		"AGENTS.md": "# hand edited\n",
	});
	try {
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /tracked fail-open instruction output is stale/);
		assert.match(result.stderr, /AGENTS\.md/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard accepts current tracked fail-open instruction outputs", () => {
	const root = setupRepo({
		"AGENTS.md": FAIL_OPEN_STUBS["AGENTS.md"],
		"CLAUDE.md": FAIL_OPEN_STUBS["CLAUDE.md"],
	});
	try {
		const result = runGuard(root);
		assert.equal(result.status, 0, `expected pass\nstdout:${result.stdout}\nstderr:${result.stderr}`);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects missing tracked fail-open instruction outputs", () => {
	const root = setupRepo({
		"AGENTS.md": FAIL_OPEN_STUBS["AGENTS.md"],
	});
	try {
		git(root, "rm", "-q", "--cached", "AGENTS.md");
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /tracked fail-open instruction output is stale/);
		assert.match(result.stderr, /AGENTS\.md \(not tracked\)/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects tracked polyscribe copies anywhere", () => {
	const root = setupRepo({
		"tools/polyscribe.sh": "#!/usr/bin/env bash\n",
	});
	try {
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /repo-local polyscribe copy is forbidden/);
		assert.match(result.stderr, /tools\/polyscribe\.sh/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects repo-local polyscribe copies even when untracked", () => {
	const root = setupRepo({ "package.json": '{"private":true}\n' });
	try {
		write(root, "scripts/polyscribe.sh", "#!/usr/bin/env bash\n");
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /repo-local polyscribe copy is forbidden/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects untracked polyscribe copies outside scripts", () => {
	const root = setupRepo({ "package.json": '{"private":true}\n' });
	try {
		write(root, "tools/polyscribe.sh", "#!/usr/bin/env bash\n");
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /repo-local polyscribe copy is forbidden/);
		assert.match(result.stderr, /tools\/polyscribe\.sh/);
		assert.doesNotMatch(result.stderr, /scripts\/polyscribe\.sh/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("canonical sx guard rejects ignored polyscribe copies", () => {
	const root = setupRepo({
		".gitignore": ".claude/\n",
		"package.json": '{"private":true}\n',
	});
	try {
		write(root, ".claude/hooks/polyscribe.sh", "#!/usr/bin/env bash\n");
		const result = runGuard(root);
		assert.notEqual(result.status, 0, `expected failure\nstdout:${result.stdout}\nstderr:${result.stderr}`);
		assert.match(result.stderr, /repo-local polyscribe copy is forbidden/);
		assert.match(result.stderr, /\.claude\/hooks\/polyscribe\.sh/);
	} finally {
		rmSync(root, { recursive: true, force: true });
	}
});

test("current repository conforms to the canonical sx guard", () => {
	const result = runGuard(repoRoot);
	assert.equal(result.status, 0, `expected pass\nstdout:${result.stdout}\nstderr:${result.stderr}`);
});
