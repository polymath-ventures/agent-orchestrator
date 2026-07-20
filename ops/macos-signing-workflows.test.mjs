import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const REQUIRED_SECRET_CHECKS = [
	"secrets.CSC_LINK != ''",
	"secrets.CSC_KEY_PASSWORD != ''",
	"secrets.APPLE_API_KEY_BASE64 != ''",
	"secrets.APPLE_API_KEY_ID != ''",
	"secrets.APPLE_API_ISSUER != ''",
	"secrets.APPLE_SIGNING_IDENTITY != ''",
];
const SIGNING_ENV = "HAS_MACOS_SIGNING_SECRETS";
const SIGNING_ENV_CHECK = `env.${SIGNING_ENV} == 'true'`;

describe("macOS signing workflows", () => {
	for (const workflow of ["frontend-nightly.yml", "frontend-release.yml"]) {
		it(`${workflow} skips signing setup when signing secrets are absent`, async () => {
			const body = await readFile(path.join(REPO_ROOT, ".github/workflows", workflow), "utf8");
			const steps = signingSetupSteps(body);
			const envDefinitions = signingEnvDefinitions(body);

			assert.equal(steps.length, 2, `${workflow} should have matrix and Intel signing setup steps`);
			assert.equal(envDefinitions.length, 2, `${workflow} should define ${SIGNING_ENV} for both signing jobs`);
			for (const definition of envDefinitions) {
				for (const check of REQUIRED_SECRET_CHECKS) {
					assert.ok(definition.includes(check), `${SIGNING_ENV} should include ${check}`);
				}
			}
			for (const step of steps) {
				const condition = ifCondition(step);
				assert.ok(condition, "macOS signing setup should have an if condition");
				assert.ok(condition.includes(SIGNING_ENV_CHECK), `condition should include ${SIGNING_ENV_CHECK}`);
				assert.equal(condition.includes("secrets."), false, "step-level if should not read the secrets context");
			}
		});
	}
});

function signingEnvDefinitions(workflow) {
	return workflow
		.split("\n")
		.filter((line) => line.includes(`${SIGNING_ENV}:`))
		.map((line) => line.trim());
}

function signingSetupSteps(workflow) {
	const lines = workflow.split("\n");
	const steps = [];

	for (let index = 0; index < lines.length; index += 1) {
		if (!lines[index].includes("- name: macOS signing setup")) continue;
		const indent = leadingSpaces(lines[index]);
		const stepLines = [lines[index]];
		for (let next = index + 1; next < lines.length; next += 1) {
			if (leadingSpaces(lines[next]) === indent && lines[next].trimStart().startsWith("- ")) break;
			stepLines.push(lines[next]);
		}
		steps.push(stepLines.join("\n"));
	}

	return steps;
}

function ifCondition(step) {
	const line = step.split("\n").find((candidate) => candidate.trimStart().startsWith("if:"));
	return line?.replace(/^\s*if:\s*/, "").trim() ?? "";
}

function leadingSpaces(line) {
	return line.length - line.trimStart().length;
}
