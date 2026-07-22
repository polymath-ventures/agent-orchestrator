import { describe, expect, it } from "vitest";
import { createProjectConfig } from "../routes/_shell";

describe("createProjectConfig", () => {
	it("persists selected worker and orchestrator agents without tracker intake by default", () => {
		expect(
			createProjectConfig({
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
				modelOverride: { harness: "", model: "", effort: "" },
			}),
		).toEqual({
			worker: { agent: "codex" },
			orchestrator: { agent: "claude-code" },
		});
	});

	it("writes an explicit Fugu tuple only to its harness-specific project override", () => {
		const config = createProjectConfig({
			workerAgent: "codex-fugu",
			orchestratorAgent: "claude-code",
			modelOverride: { harness: "codex-fugu", model: "fugu", effort: "xhigh" },
		});

		expect(config).toEqual({
			worker: { agent: "codex-fugu" },
			orchestrator: { agent: "claude-code" },
			agentConfig: {
				modelByHarness: {
					"codex-fugu": { model: "fugu", effort: "xhigh" },
				},
			},
		});
		expect(config.agentConfig).not.toHaveProperty("model");
	});

	it("preserves tracker intake alongside selected agent defaults", () => {
		expect(
			createProjectConfig({
				workerAgent: "cursor",
				orchestratorAgent: "opencode",
				modelOverride: { harness: "", model: "", effort: "" },
				trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
			}),
		).toEqual({
			worker: { agent: "cursor" },
			orchestrator: { agent: "opencode" },
			trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
		});
	});
});
