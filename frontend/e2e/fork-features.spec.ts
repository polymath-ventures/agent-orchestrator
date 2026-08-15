import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { installBrowserModeApiFixtures } from "./fixtures";

const captureEvidence = process.env.CAPTURE_FORK_SCREENSHOTS === "1";
const evidenceDir = "../docs/screenshots/fork-features";
let savedProjectConfig: unknown;

test.beforeEach(async ({ page }) => {
	savedProjectConfig = undefined;
	await installBrowserModeApiFixtures(page, { includePrimeSession: true });
	const agents = [
		{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
		{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true },
		{ id: "codex-fugu", label: "Codex Fugu", authStatus: "authorized", reviewerCapable: true },
	];
	await page.route("**/api/v1/agents", (route) =>
		route.fulfill({ json: { supported: agents, installed: agents, authorized: agents } }),
	);
	await page.route("**/api/v1/agents/models**", (route) =>
		route.fulfill({
			json: {
				checkedAt: "2026-08-14T00:00:00Z",
				harnesses: [
					{
						id: "codex",
						label: "Codex",
						reviewerCapable: true,
						catalogSource: "adapter",
						catalogVerified: true,
						models: [
							{
								model: "gpt-5.5",
								label: "GPT-5.5",
								efforts: ["low", "medium", "high"],
								defaultEffort: "medium",
								verified: true,
								status: "reachable",
							},
						],
					},
				],
			},
		}),
	);
	await page.route("**/api/v1/fleet", (route) => route.fulfill({ json: { paused: false } }));
	await page.route("**/api/v1/metrics", (route) =>
		route.fulfill({
			json: {
				history: [],
				latest: { quotas: [] },
				probeStatuses: [
					{
						harness: "codex",
						hasData: true,
						probedAt: "2026-08-14T00:00:00Z",
						snapshots: [
							{ used: 42, windowEnd: "2026-08-16T00:00:00Z", windowName: "primary" },
							{ used: 7, windowEnd: "2026-08-21T00:00:00Z", windowName: "secondary" },
						],
						state: "ok",
					},
				],
			},
		}),
	);
	await page.route("**/api/v1/prime/settings", (route) =>
		route.fulfill({
			json: {
				settings: {
					enabled: true,
					displayName: "AO Prime",
					agent: "codex",
					agentConfig: {},
					rules: "",
					rulesFile: "",
					wakeInterval: "15m",
				},
			},
		}),
	);
	await page.route("**/api/v1/projects/api-gateway", async (route) => {
		if (route.request().method() === "PUT") {
			const body = route.request().postDataJSON() as { config?: unknown };
			savedProjectConfig = body.config;
			return route.fulfill({ json: { status: "ok" } });
		}
		return route.fulfill({
			json: {
				status: "ok",
				project: {
					id: "api-gateway",
					kind: "git",
					name: "api-gateway",
					path: "/Users/me/api-gateway",
					repo: "me/api-gateway",
					defaultBranch: "main",
					sessionPrefix: "ao",
					config: {
						orchestrator: {
							agent: "codex",
							agentConfig: { modelByHarness: { codex: { model: "gpt-5.5", effort: "low" } } },
						},
						worker: {
							agent: "codex",
							agentConfig: { modelByHarness: { codex: { model: "gpt-5.5", effort: "high" } } },
						},
						reviewers: [{ harness: "codex-fugu" }],
						workerMix: [{ agent: "codex", model: "gpt-5.5", effort: "high", weight: 100 }],
						workerTaskPrompt: "/address-issue {issue}",
					},
				},
			},
		});
	});
});

test("fork UI features stay mounted from the application shell", async ({ page }) => {
	await page.goto("/");
	const workerRow = page.getByRole("button", { name: "Open fix-webgl-fallback" });
	await expect(workerRow.locator("[data-harness-glyph]")).toHaveAttribute("title", "Codex");
	await expect(workerRow).toHaveAttribute("aria-describedby", /sidebar-harness-/);
	const primeRow = page.getByRole("button", { name: "Open AO Prime" });
	await expect(primeRow.locator("[data-harness-glyph]")).toHaveAttribute("title", "Codex");
	await expect(primeRow).toHaveAttribute("aria-describedby", /sidebar-harness-/);
	if (captureEvidence) {
		await mkdir(evidenceDir, { recursive: true });
		await page.screenshot({ path: `${evidenceDir}/web-client.png`, fullPage: true });
		const quotaPanel = page.getByText("Quota", { exact: true }).locator("../..");
		await expect(quotaPanel).toBeVisible();
		await quotaPanel.screenshot({ path: `${evidenceDir}/quota-usage.png` });
		await workerRow.screenshot({ path: `${evidenceDir}/sidebar-harness-glyph.png` });
		await primeRow.screenshot({ path: `${evidenceDir}/prime-harness-glyph.png` });
		await page.goto("/#/terminals");
		await expect(page.getByTestId("session-terminal")).toBeVisible();
		await page.screenshot({ path: `${evidenceDir}/terminal-focus.png`, fullPage: true });
		await page.setViewportSize({ width: 1280, height: 1200 });
	}

	await page.goto("/#/settings");
	const globalSettings = page.getByRole("dialog");
	const fleetTitle = globalSettings.getByText("Fleet", { exact: true });
	const primeTitle = globalSettings.getByText("Prime", { exact: true });
	await expect(fleetTitle).toBeVisible();
	await expect(primeTitle).toBeVisible();
	if (captureEvidence) {
		await fleetTitle.locator("../..").screenshot({ path: `${evidenceDir}/fleet-controls.png` });
		await primeTitle.evaluate((title) => title.scrollIntoView({ block: "start" }));
		await globalSettings.screenshot({ path: `${evidenceDir}/prime-controls.png` });
	}

	await page.goto("/#/projects/api-gateway/settings");
	const projectSettings = page.getByRole("dialog");
	await expect(projectSettings).toBeVisible();

	await projectSettings.getByRole("button", { name: "Workflow", exact: true }).click();
	const reviewer = projectSettings.getByRole("button", { name: "Default reviewer agent" });
	await reviewer.click();
	await expect(page.getByRole("menuitem", { name: "Codex Fugu" })).toBeVisible();
	if (captureEvidence) {
		await page.screenshot({ path: `${evidenceDir}/harness-selection.png`, fullPage: true });
	}
	await page.keyboard.press("Escape");

	await projectSettings.getByRole("button", { name: "Agents", exact: true }).click();
	const workerEffort = projectSettings.getByLabel("Worker effort");
	const orchestratorEffort = projectSettings.getByLabel("Orchestrator effort");
	await expect(workerEffort).toBeVisible();
	await expect(workerEffort).toHaveValue("high");
	await expect(orchestratorEffort).toBeVisible();
	await expect(orchestratorEffort).toHaveValue("low");
	await expect(workerEffort).toHaveJSProperty("tagName", "SELECT");
	await expect(orchestratorEffort).toHaveJSProperty("tagName", "SELECT");
	await workerEffort.selectOption("medium");
	await orchestratorEffort.selectOption("high");
	if (captureEvidence) {
		await page.screenshot({ path: `${evidenceDir}/project-settings-agents-effort.png`, fullPage: true });
	}
	await projectSettings.getByRole("button", { name: "Save changes" }).click();
	await expect
		.poll(() => savedProjectConfig)
		.toMatchObject({
			worker: { agentConfig: { model: "gpt-5.5", effort: "medium" } },
			orchestrator: { agentConfig: { model: "gpt-5.5", effort: "high" } },
		});

	await projectSettings.getByRole("button", { name: "Instructions", exact: true }).click();
	await expect(projectSettings.getByRole("textbox", { name: "Worker task prompt template" })).toHaveValue(
		"/address-issue {issue}",
	);
	if (captureEvidence) {
		await page.screenshot({ path: `${evidenceDir}/worker-task-prompt.png`, fullPage: true });
	}

	await projectSettings.getByRole("button", { name: "Workers", exact: true }).click();
	await expect(projectSettings.getByText("Bucket 1", { exact: true })).toBeVisible();
	await expect(projectSettings.getByRole("spinbutton", { name: "Weight" })).toHaveValue("100");
});
