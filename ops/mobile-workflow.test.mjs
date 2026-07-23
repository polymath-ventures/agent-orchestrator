import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const workflow = readFileSync(".github/workflows/mobile.yml", "utf8");

test("mobile TestFlight job never runs on pull requests", () => {
	const job = jobBlock(workflow, "ios-testflight");

	assert.match(workflow, /^\s*pull_request:/m);
	assert.match(
		job,
		/^\s*if:\s*\$\{\{\s*github\.event_name == 'push' \|\| github\.event_name == 'workflow_dispatch'\s*\}\}/m,
	);
	assert.doesNotMatch(job, /github\.event_name == 'pull_request'/);
	assert.match(job, /^\s*runs-on:\s*\[self-hosted,\s*macOS,\s*ao-mac-mini\]/m);
});

test("mobile TestFlight secrets stay scoped to credentialed steps", () => {
	const job = jobBlock(workflow, "ios-testflight");
	const allowedSecretSteps = [
		stepBlock(job, "Preflight - App Store Connect credentials"),
		stepBlock(job, "Build and upload to TestFlight"),
	];
	const jobWithoutAllowedSteps = allowedSecretSteps.reduce((remaining, step) => remaining.replace(step, ""), job);

	assert.doesNotMatch(jobWithoutAllowedSteps, /\$\{\{\s*secrets\./);
	for (const secret of ["ASC_KEY_ID", "ASC_ISSUER_ID", "ASC_API_KEY_P8", "APPLE_TEAM_ID"]) {
		assert.ok(allowedSecretSteps.join("\n").includes(`secrets.${secret}`));
	}
});

function jobBlock(body, jobName) {
	const lines = body.split("\n");
	const start = lines.findIndex((line) => line === `  ${jobName}:`);
	assert.notEqual(start, -1, `missing ${jobName} job`);

	let end = lines.length;
	for (let index = start + 1; index < lines.length; index += 1) {
		if (/^  [A-Za-z0-9_-]+:/.test(lines[index])) {
			end = index;
			break;
		}
	}

	return lines.slice(start, end).join("\n");
}

function stepBlock(job, stepName) {
	const lines = job.split("\n");
	const start = lines.findIndex((line) => line.trim() === `- name: ${stepName}`);
	assert.notEqual(start, -1, `missing ${stepName} step`);
	const indent = leadingSpaces(lines[start]);

	let end = lines.length;
	for (let index = start + 1; index < lines.length; index += 1) {
		if (leadingSpaces(lines[index]) === indent && lines[index].trimStart().startsWith("- ")) {
			end = index;
			break;
		}
	}

	return lines.slice(start, end).join("\n");
}

function leadingSpaces(line) {
	return line.length - line.trimStart().length;
}
