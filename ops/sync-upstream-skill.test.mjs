import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import test from "node:test";

const skillPath = new URL("../skills/sync-upstream/SKILL.md", import.meta.url);
const forkDocPath = new URL("../docs/fork.md", import.meta.url);

test("sync-upstream client install copies are not tracked", () => {
	const tracked = execFileSync("git", ["ls-files", ".claude/skills/sync-upstream", ".agents/skills/sync-upstream"], {
		encoding: "utf8",
	});
	assert.equal(tracked, "");
});

test("sync-upstream treats migration collisions as routine reconciliation", async () => {
	const skill = await readFile(skillPath, "utf8");

	assert.match(skill, /preserve[\s\S]{0,80}applied[\s\S]{0,80}migration/i);
	assert.match(skill, /already[- ]ported.*upstream.*migration/i);
	assert.match(skill, /rename only.*new.*upstream.*migration/i);
	assert.doesNotMatch(skill, /STOP conditions[\s\S]*Migration-version collision[\s\S]*operator decision/i);
});

test("sync-upstream preserves merge ancestry instead of rebasing", async () => {
	const skill = await readFile(skillPath, "utf8");

	assert.match(skill, /Never rebase a sync branch — this overrides the generic rule\./);
	assert.match(skill, /Do not reconcile the two by rebasing "just this once\."/);
	assert.match(skill, /merge origin\/main/i);
	assert.match(skill, /This overrides Rule 3's generic "rebase against the default branch" instruction/);
});

test("sync-upstream inspects merge commits and runs behavioral guards", async () => {
	const skill = await readFile(skillPath, "utf8");

	assert.match(skill, /git log -m --merges/);
	assert.match(skill, /"origin\/main\.\.HEAD"/);
	assert.doesNotMatch(skill, /\$DEFAULT_BRANCH/);
	assert.match(skill, /anchor(?: path)?\s+existence alone is (?:not|never) sufficient/i);
	assert.match(skill, /every named behavioral guard/i);
	assert.match(skill, /frontend\/e2e\/browser-mode\.spec\.ts/);
	assert.match(skill, /frontend\/e2e\/mobile-sidebar-toggle\.spec\.ts/);
	assert.match(skill, /frontend\/e2e\/terminal-focus\.spec\.ts/);
	assert.match(skill, /frontend\/e2e\/fork-features\.spec\.ts/);
	assert.match(skill, /AO_E2E_PORT/);
	assert.match(skill, /failure.*STOP condition|STOP condition.*failure/is);
});

test("fork checklist distinguishes anchors from behavioral guards", async () => {
	const forkDoc = await readFile(forkDocPath, "utf8");
	const checklist = forkDoc.match(
		/## Fork Features To Preserve \(sync checklist\)([\s\S]*?)\*\*Explicitly NOT fork-specific/,
	)?.[1];
	assert.ok(checklist, "fork sync checklist is present");
	assert.match(checklist, /Anchors say where a feature lives; behavioral guards prove it still works\./);
	assert.match(checklist, /sync is not complete until every named behavioral guard passes/i);

	const starts = [...checklist.matchAll(/^(\d+)\. \*\*/gm)];
	assert.deepEqual(
		starts.map((match) => Number(match[1])),
		[1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
	);
	const entries = new Map(
		starts.map((match, index) => [
			Number(match[1]),
			checklist.slice(match.index, starts[index + 1]?.index ?? checklist.length),
		]),
	);
	for (const number of [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]) {
		assert.match(entries.get(number), /\*\*Behavioral guards?:\*\*/, `item ${number} names a behavioral guard`);
	}
	for (const number of [1, 2, 3, 4, 5, 6, 10]) {
		assert.match(entries.get(number), /screenshots\/fork-features\//, `item ${number} links reference evidence`);
	}
});
