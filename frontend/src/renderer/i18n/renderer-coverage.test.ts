import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import ts from "typescript";
import { describe, expect, it } from "vitest";

const rendererDirectory = path.resolve(process.cwd(), "src/renderer");
const displayAttributes = new Set(["alt", "aria-label", "placeholder", "title"]);

// These are content/data, product names, technical units, or keyboard chords—not
// English UI copy. Keeping this allowlist exact makes newly introduced chrome fail.
const approvedLiterals: Record<string, readonly string[]> = {
	"components/BrowserPanel.tsx": [
		"AO Preview",
		"Demo app preview",
		"The worker exposed a local Vite app with",
		"ao preview",
		"Loaded",
		"$ npm run dev -- --host 127.0.0.1",
		"ready in 418 ms",
		"Local: http://localhost:5173/",
	],
	"components/CenterPane.tsx": ["px"],
	"components/CreateProjectFlow.tsx": ["my-workspace/", "web-app", "main"],
	"components/DaemonStartupLoader.tsx": ["Agent Orchestrator"],
	"components/ProjectSettingsForm.tsx": [
		"main",
		"ao",
		"No workflow settings for scratch projects.",
		"Tracker intake is not available for scratch projects.",
	],
	"components/SessionFilesView.tsx": ["-&gt;"],
	"components/SessionInspector.tsx": ["PR #"],
	"components/Sidebar.tsx": ["Agent Orchestrator", "daemon"],
	"components/WindowTitlebar.tsx": [
		"Alt+F4",
		"Ctrl+Z",
		"Ctrl+Y",
		"Ctrl+X",
		"Ctrl+C",
		"Ctrl+V",
		"Ctrl+A",
		"Ctrl+R",
		"Ctrl+Shift+I",
		"Ctrl+/",
	],
	"components/settings/ConnectMobileSetup.tsx": ["tailscale ip -4"],
	"components/settings/UpdatesSection.tsx": ["PR #"],
};

// The Chat surface predates this coverage gate and is intentionally being
// localized as a follow-up. Keep the deferral scoped to the new surface so
// hardcoded chrome elsewhere in the renderer still fails this test.
const deferredLocalizationFiles = new Set([
	"components/SessionInterfaceSwitch.tsx",
	"components/chat/ActivityRun.tsx",
	"components/chat/ChatComposer.tsx",
	"components/chat/ChatMarkdown.tsx",
	"components/chat/ChatStatusBanners.tsx",
	"components/chat/ChatTimelineItems.tsx",
	"components/chat/ChatWorkspace.tsx",
	"components/chat/ComposerSuggestMenu.tsx",
	"components/chat/ContextMeter.tsx",
	"components/chat/CopyButton.tsx",
	"components/chat/ElicitationCard.tsx",
	"components/chat/SessionChatSurface.tsx",
	"components/chat/TurnPlan.tsx",
	"components/chat/TurnSettingsBar.tsx",
]);

// These fork-only surfaces also predate the incoming upstream coverage gate.
// Keep their deferral literal-exact rather than excluding whole files: follow-up
// localization can remove entries one at a time, while any new hardcoded chrome
// in these components still fails this test.
const deferredLocalizationLiterals: Record<string, readonly string[]> = {
	"components/CreateProjectAgentSheet.tsx": [
		"Close project harnesses dialog",
		"Select reviewer harness",
		"Permission mode",
		"Model catalogs are unavailable. Enter a model ID manually for the selected harness default.",
		"Loading harnesses...",
		"Harness availability is cached.",
		"Retry",
		"If this folder needs Git setup, AO will initialize it and create the first commit before starting.",
		"Cancel",
	],
	"components/CreateProjectFlow.tsx": [
		"Back to import type",
		"Enter a path on the machine running AO.",
		"Close import dialog",
		"Path",
		"/home/me/workspace",
		"/home/me/workspace/project",
		"Cancel",
		"Continue",
	],
	"components/FleetSection.tsx": [
		"Fleet",
		"Pause the whole fleet to stop new work across every project. A soft pause lets live workers finish and drain at idle; a hard fleet pause terminates workers, orchestrators, and prime sessions immediately and loses mid-flight work.",
		"Status",
		"Checking…",
		"Unknown (daemon unreachable)",
		"Running",
		"Resume",
		"Pause",
		"Pause now (hard)",
		"Hard pause the fleet?",
		"This immediately terminates every live worker, orchestrator, and prime session across all projects. In-flight, uncommitted work is discarded. Use a normal pause to let workers drain instead.",
	],
	"components/MobileSidebarOpener.tsx": ["Close sidebar", "Open sidebar"],
	"components/ModelAvailabilityField.tsx": [
		"Refresh model catalog",
		"Harness",
		"Model",
		"Effort",
		"Manual model IDs are allowed; launch may fail if the harness rejects the model.",
		"Status:",
		"Checked",
	],
	"components/PrimeBoard.tsx": [
		"Loading Prime…",
		"Could not load Prime settings, so Prime&apos;s state is unknown. Retry once the daemon is reachable.",
		"Prime is disabled. Enable it in Settings to run a fleet-wide supervisor.",
		"Opening the Prime terminal…",
		"Prime is enabled but not running",
		"No Prime session is active. Relaunch Prime to start a fresh supervisor on the canonical branch — this also clears any paused automatic replacement.",
		"Relaunch Prime",
	],
	"components/PrimeSection.tsx": [
		"Prime",
		"Enable Prime",
		"Permission mode",
		"Model catalogs are unavailable; saved pins remain editable.",
		"Inline instructions",
		"Inline instructions are loaded first. File content is appended after it; the file does not override inline instructions. Use an absolute path for fleet Prime.",
		"Save Prime",
	],
	"components/QuotaPanel.tsx": [
		"Expand quota widget",
		"Collapse quota widget",
		"Quota",
		"Refresh all quota probes",
		"probe failed — retry",
		"updated",
		"no machine-readable source",
		"no usage recorded yet",
		"quota usage",
		"% left",
		"resets",
		"Probe",
		"Probe now",
	],
	"components/Sidebar.tsx": ["Open", "Open Prime", "Prime", "Down"],
	"components/WorkerMixFields.tsx": [
		"Distribute unpinned worker spawns across agent buckets by weight. Weights must sum to 100; an empty mix leaves the feature off.",
		"No worker mix configured.",
		"Bucket",
		"Remove bucket",
		"Select agent",
		"Blank model uses the agent default; blank effort inherits worker configuration.",
		"Add bucket",
		"Total:",
	],
};

function rendererFiles(directory: string): string[] {
	return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
		const absolute = path.join(directory, entry.name);
		if (entry.isDirectory()) return rendererFiles(absolute);
		if (!entry.name.endsWith(".tsx") || entry.name.includes(".test.")) return [];
		return [absolute];
	});
}

function normalized(value: string): string {
	return value.replace(/\s+/g, " ").trim();
}

function literalBranches(expression: ts.Expression): string[] {
	if (ts.isStringLiteralLike(expression)) return [expression.text];
	if (ts.isTemplateExpression(expression)) {
		return [expression.head.text, ...expression.templateSpans.flatMap((span) => [span.literal.text])];
	}
	if (ts.isConditionalExpression(expression)) {
		return [...literalBranches(expression.whenTrue), ...literalBranches(expression.whenFalse)];
	}
	if (ts.isParenthesizedExpression(expression)) return literalBranches(expression.expression);
	if (ts.isBinaryExpression(expression) && expression.operatorToken.kind === ts.SyntaxKind.PlusToken) {
		return [...literalBranches(expression.left), ...literalBranches(expression.right)];
	}
	return [];
}

function potentialDisplayText(value: string): boolean {
	return /[A-Za-z]{2}/.test(value);
}

function approved(file: string, value: string): boolean {
	const relative = path.relative(rendererDirectory, file).replace(/\\/g, "/");
	if (deferredLocalizationFiles.has(relative)) return true;
	return (
		(approvedLiterals[relative]?.includes(value) ?? false) ||
		(deferredLocalizationLiterals[relative]?.includes(value) ?? false)
	);
}

describe("renderer localization coverage", () => {
	it("does not introduce hardcoded English JSX chrome", () => {
		const violations: string[] = [];
		for (const file of rendererFiles(rendererDirectory)) {
			const source = readFileSync(file, "utf8");
			const sourceFile = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
			const record = (node: ts.Node, rawValue: string) => {
				const value = normalized(rawValue);
				if (!value || !potentialDisplayText(value) || approved(file, value)) return;
				const line = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;
				violations.push(`${path.relative(rendererDirectory, file)}:${line} ${JSON.stringify(value)}`);
			};
			const visit = (node: ts.Node) => {
				if (ts.isJsxText(node)) record(node, node.getText(sourceFile));
				if (ts.isJsxAttribute(node) && displayAttributes.has(node.name.getText(sourceFile)) && node.initializer) {
					if (ts.isStringLiteral(node.initializer)) record(node, node.initializer.text);
					if (ts.isJsxExpression(node.initializer) && node.initializer.expression) {
						for (const branch of literalBranches(node.initializer.expression)) record(node, branch);
					}
				}
				ts.forEachChild(node, visit);
			};
			visit(sourceFile);
		}

		expect(violations, violations.join("\n")).toEqual([]);
	});
});
