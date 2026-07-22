import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const workflowsDir = ".github/workflows";
const workflows = readdirSync(workflowsDir)
	.filter((name) => name.endsWith(".yml") || name.endsWith(".yaml"))
	.map((name) => ({
		name,
		body: readFileSync(path.join(workflowsDir, name), "utf8"),
	}));

test("public-repo workflows use free GitHub-hosted runners, never Blacksmith", () => {
	const offenders = workflows.flatMap(({ name, body }) =>
		body
			.split("\n")
			.map((line, index) => ({ file: name, line: index + 1, text: line.trim() }))
			.filter(({ text }) => /blacksmith/i.test(text)),
	);

	assert.deepEqual(offenders, [], `Blacksmith workflow references found:\n${JSON.stringify(offenders, null, 2)}`);
});
