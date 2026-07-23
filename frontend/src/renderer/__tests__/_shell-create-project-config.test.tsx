import { describe, expect, it, vi } from "vitest";
import { createProjectConfig, ensureProjectCreateDaemonReady } from "../routes/_shell";

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

describe("ensureProjectCreateDaemonReady", () => {
	it("accepts browser-mode readiness without an Electron daemon port", async () => {
		const bridge = window.ao;
		delete window.ao;
		try {
			const refreshStatus = vi.fn().mockResolvedValue({ state: "ready", pid: 42 });
			const startDaemon = vi.fn();

			await expect(
				ensureProjectCreateDaemonReady({
					refreshStatus,
					startDaemon,
					applyStatus: vi.fn(),
				}),
			).resolves.toEqual({ state: "ready", pid: 42 });

			expect(startDaemon).not.toHaveBeenCalled();
		} finally {
			window.ao = bridge;
		}
	});

	it("waits for a starting daemon and applies the ready port before creating", async () => {
		const refreshStatus = vi.fn().mockResolvedValue({ state: "starting" });
		const startDaemon = vi.fn().mockResolvedValue({ state: "ready", port: 3037, pid: 42 });
		const applyStatus = vi.fn();

		await expect(
			ensureProjectCreateDaemonReady({
				refreshStatus,
				startDaemon,
				applyStatus,
			}),
		).resolves.toEqual({ state: "ready", port: 3037, pid: 42 });

		expect(startDaemon).toHaveBeenCalledTimes(1);
		expect(applyStatus).toHaveBeenCalledWith({ state: "ready", port: 3037, pid: 42 });
	});

	it("surfaces the daemon start failure when it still cannot become ready", async () => {
		const refreshStatus = vi.fn().mockResolvedValue({ state: "starting" });
		const startDaemon = vi.fn().mockResolvedValue({
			state: "error",
			code: "not_ready",
			message: "An AO daemon is already running, but it is not ready yet.",
		});

		await expect(
			ensureProjectCreateDaemonReady({
				refreshStatus,
				startDaemon,
				applyStatus: vi.fn(),
			}),
		).rejects.toThrow("An AO daemon is already running, but it is not ready yet.");
	});
});
