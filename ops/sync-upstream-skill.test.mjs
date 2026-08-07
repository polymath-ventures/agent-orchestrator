import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const skillPath = new URL("../skills/sync-upstream/SKILL.md", import.meta.url);

test("sync-upstream treats migration collisions as routine reconciliation", async () => {
	const skill = await readFile(skillPath, "utf8");

	assert.match(skill, /preserve[\s\S]{0,80}applied[\s\S]{0,80}migration/i);
	assert.match(skill, /already[- ]ported.*upstream.*migration/i);
	assert.match(skill, /rename only.*new.*upstream.*migration/i);
	assert.doesNotMatch(skill, /STOP conditions[\s\S]*Migration-version collision[\s\S]*operator decision/i);
});

test("sync-upstream preserves merge ancestry instead of rebasing", async () => {
	const skill = await readFile(skillPath, "utf8");

	assert.match(skill, /never rebase/i);
	assert.match(skill, /merge origin\/main/i);
});
