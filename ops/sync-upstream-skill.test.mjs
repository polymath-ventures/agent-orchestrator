import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const skillPath = new URL("../skills/sync-upstream/SKILL.md", import.meta.url);
const claudeSkillPath = new URL("../.claude/skills/sync-upstream/SKILL.md", import.meta.url);
const codexSkillPath = new URL("../.agents/skills/sync-upstream/SKILL.md", import.meta.url);

test("sync-upstream client copies match the canonical skill", async () => {
	const canonical = await readFile(skillPath, "utf8");
	const [claude, codex] = await Promise.all([readFile(claudeSkillPath, "utf8"), readFile(codexSkillPath, "utf8")]);

	assert.equal(claude, canonical);
	assert.equal(codex, canonical);
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
	assert.match(skill, /anchor(?: path)?\s+existence alone is (?:not|never) sufficient/i);
	assert.match(skill, /every named behavioral guard/i);
	assert.match(skill, /frontend\/e2e\/browser-mode\.spec\.ts/);
	assert.match(skill, /frontend\/e2e\/mobile-sidebar-toggle\.spec\.ts/);
	assert.match(skill, /frontend\/e2e\/terminal-focus\.spec\.ts/);
	assert.match(skill, /failure.*STOP condition|STOP condition.*failure/is);
});
