import { describe, expect, it } from "vitest";
import { createProjectConfig } from "../routes/_shell";

describe("createProjectConfig", () => {
	it("persists selected worker and orchestrator agents without tracker intake by default", () => {
		expect(
			createProjectConfig({
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
				reviewerAgent: "",
				modelDefaults: {},
			}),
		).toEqual({
			worker: { agent: "codex" },
			orchestrator: { agent: "claude-code" },
		});
	});

	it("writes selected harness default tuples only for configured selected harnesses", () => {
		const config = createProjectConfig({
			workerAgent: "codex-fugu",
			orchestratorAgent: "claude-code",
			reviewerAgent: "claude-code",
			modelDefaults: {
				"codex-fugu": { model: "fugu", effort: "xhigh" },
				"claude-code": { model: "opus", effort: "" },
				opencode: { model: "openai/gpt-5.4", effort: "medium" },
			},
		});

		expect(config).toEqual({
			worker: { agent: "codex-fugu" },
			orchestrator: { agent: "claude-code" },
			reviewers: [{ harness: "claude-code" }],
			agentConfig: {
				modelByHarness: {
					"codex-fugu": { model: "fugu", effort: "xhigh" },
					"claude-code": { model: "opus" },
				},
			},
		});
		expect(config.agentConfig).not.toHaveProperty("model");
	});

	it("omits the reviewer config when automatic independent reviewer is selected", () => {
		expect(
			createProjectConfig({
				workerAgent: "codex",
				orchestratorAgent: "codex",
				reviewerAgent: "",
				modelDefaults: {
					codex: { model: "", effort: "" },
				},
			}),
		).toEqual({
			worker: { agent: "codex" },
			orchestrator: { agent: "codex" },
		});
	});

	it("preserves tracker intake alongside selected agent defaults", () => {
		expect(
			createProjectConfig({
				workerAgent: "cursor",
				orchestratorAgent: "opencode",
				reviewerAgent: "",
				modelDefaults: {},
				trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
			}),
		).toEqual({
			worker: { agent: "cursor" },
			orchestrator: { agent: "opencode" },
			trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
		});
	});
});
