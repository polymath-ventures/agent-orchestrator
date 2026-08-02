import { describe, expect, it, vi } from "vitest";
import { createProjectConfig, ensureProjectCreateDaemonReady } from "../routes/_shell";

describe("createProjectConfig", () => {
	it("persists selected worker and orchestrator agents without tracker intake by default", () => {
		expect(
			createProjectConfig({
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
				reviewerAgent: "",
				permissions: "",
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
			permissions: "",
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

	it("persists an explicit permission mode alongside selected harness defaults", () => {
		expect(
			createProjectConfig({
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
				reviewerAgent: "",
				permissions: "bypass-permissions",
				modelDefaults: {
					codex: { model: "gpt-5-codex", effort: "high" },
				},
			}),
		).toEqual({
			worker: { agent: "codex" },
			orchestrator: { agent: "claude-code" },
			agentConfig: {
				permissions: "bypass-permissions",
				modelByHarness: {
					codex: { model: "gpt-5-codex", effort: "high" },
				},
			},
		});
	});

	it("omits the reviewer config when automatic independent reviewer is selected", () => {
		expect(
			createProjectConfig({
				workerAgent: "codex",
				orchestratorAgent: "codex",
				reviewerAgent: "",
				permissions: "",
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
				permissions: "",
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

	it("does not start the Electron daemon from browser mode", async () => {
		const bridge = window.ao;
		delete window.ao;
		try {
			const refreshStatus = vi.fn().mockResolvedValue({ state: "starting" });
			const startDaemon = vi.fn();

			await expect(
				ensureProjectCreateDaemonReady({
					refreshStatus,
					startDaemon,
					applyStatus: vi.fn(),
				}),
			).rejects.toThrow("AO daemon is not ready.");

			expect(startDaemon).not.toHaveBeenCalled();
		} finally {
			window.ao = bridge;
		}
	});

	it("starts the Electron daemon when ready status has no bridge port", async () => {
		const refreshStatus = vi.fn().mockResolvedValue({ state: "ready", pid: 41 });
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
