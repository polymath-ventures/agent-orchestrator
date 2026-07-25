import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import type { AgentModelAvailabilityResponse } from "../hooks/useModelAvailabilityQuery";
import {
	buildWorkerMix,
	parseMaxLiveWorkers,
	toWorkerMixForm,
	type WorkerMixBucket,
	WorkerMixFields,
	workerMixInvalid,
	workerMixTotal,
} from "./WorkerMixFields";

const longOpenCodeModel = "private-cloud-provider/teams/research/models/experimental-reasoner-2026-07-22";

const availability: AgentModelAvailabilityResponse = {
	checkedAt: "2026-07-22T01:02:03Z",
	harnesses: [
		{
			id: "claude-code",
			label: "Claude Code",
			reviewerCapable: true,
			catalogSource: "adapter",
			catalogVerified: true,
			models: [
				{
					model: "opus",
					label: "Opus",
					efforts: ["low", "high"],
					defaultEffort: "high",
					verified: true,
					status: "reachable",
				},
				{
					model: "sonnet",
					label: "Sonnet",
					efforts: ["low"],
					defaultEffort: "low",
					verified: true,
					status: "reachable",
				},
			],
		},
		{
			id: "codex-fugu",
			label: "Codex Fugu",
			reviewerCapable: false,
			catalogSource: "known-set",
			catalogVerified: false,
			catalogReason: "installed catalog could not be read",
			models: [
				{
					model: "fugu",
					label: "Fugu",
					efforts: ["high", "xhigh"],
					defaultEffort: "high",
					verified: false,
					status: "unknown",
					reason: "not probed",
				},
			],
		},
		{
			id: "opencode",
			label: "OpenCode",
			reviewerCapable: true,
			catalogSource: "adapter",
			catalogVerified: true,
			models: [
				{
					model: longOpenCodeModel,
					label: "Experimental Reasoner",
					efforts: ["medium", "high"],
					defaultEffort: "high",
					verified: true,
					status: "reachable",
				},
			],
		},
	],
};

const agents = [
	{ id: "claude-code", label: "Claude Code", authStatus: "authorized" as const, reviewerCapable: true },
	{ id: "codex-fugu", label: "Codex Fugu", authStatus: "authorized" as const, reviewerCapable: false },
	{ id: "opencode", label: "OpenCode", authStatus: "authorized" as const, reviewerCapable: true },
];

function bucket(overrides: Partial<WorkerMixBucket> = {}): WorkerMixBucket {
	return {
		id: "bucket-a",
		agent: "claude-code",
		model: "",
		effort: "",
		weight: "100",
		selectionsByAgent: {},
		...overrides,
	};
}

function ControlledMix({ initial, onRefresh }: { initial: WorkerMixBucket[]; onRefresh?: () => void }) {
	const [buckets, setBuckets] = useState(initial);
	return (
		<>
			<WorkerMixFields
				buckets={buckets}
				onChange={setBuckets}
				agentCatalog={{ supported: agents, installed: agents, authorized: agents }}
				availability={availability}
				onRefresh={onRefresh}
			/>
			<output data-testid="mix-state">{JSON.stringify(buckets)}</output>
		</>
	);
}

async function chooseAgent(label: string) {
	const user = userEvent.setup();
	await user.click(screen.getByRole("combobox", { name: "Agent" }));
	await user.click(await screen.findByRole("option", { name: label }));
}

describe("WorkerMixFields parsing helpers", () => {
	it("reads exponent-notation weights by value, not parseInt truncation", () => {
		const buckets = [
			bucket({ id: "a", agent: "claude-code", weight: "5e1" }),
			bucket({ id: "b", agent: "codex-fugu", weight: "50" }),
		];
		expect(workerMixTotal(buckets)).toBe(100);
		expect(workerMixInvalid(buckets)).toBe(false);
		expect(buildWorkerMix(buckets)).toEqual([
			{ agent: "claude-code", weight: 50 },
			{ agent: "codex-fugu", weight: 50 },
		]);
	});

	it("treats non-integer weight input as 0", () => {
		expect(workerMixTotal([bucket({ weight: "12.5" })])).toBe(0);
		expect(workerMixTotal([bucket({ weight: "abc" })])).toBe(0);
		expect(workerMixTotal([bucket({ weight: "" })])).toBe(0);
	});

	it("flags a mix whose real weights do not sum to 100", () => {
		expect(workerMixInvalid([bucket({ weight: "5e1" })])).toBe(true);
	});

	it("round-trips explicit effort while leaving blank effort inherited", () => {
		const [hydrated] = toWorkerMixForm([{ agent: "codex-fugu", model: "fugu", effort: "xhigh", weight: 100 }]);
		expect(hydrated).toMatchObject({ agent: "codex-fugu", model: "fugu", effort: "xhigh", weight: "100" });
		expect(hydrated.selectionsByAgent["codex-fugu"]).toEqual({ model: "fugu", effort: "xhigh" });
		expect(buildWorkerMix([hydrated])).toEqual([{ agent: "codex-fugu", model: "fugu", effort: "xhigh", weight: 100 }]);
		expect(buildWorkerMix([bucket({ model: "opus", effort: "" })])).toEqual([
			{ agent: "claude-code", model: "opus", weight: 100 },
		]);
	});

	it("parses exponent-notation cap by value, not parseInt truncation", () => {
		expect(parseMaxLiveWorkers("1e2")).toBe(100);
		expect(parseMaxLiveWorkers("8")).toBe(8);
	});

	it("treats blank, zero, and non-positive cap as unbounded", () => {
		expect(parseMaxLiveWorkers("")).toBeUndefined();
		expect(parseMaxLiveWorkers("   ")).toBeUndefined();
		expect(parseMaxLiveWorkers("0")).toBeUndefined();
		expect(parseMaxLiveWorkers("-4")).toBeUndefined();
		expect(parseMaxLiveWorkers("2.5")).toBeUndefined();
	});
});

describe("WorkerMixFields model selection", () => {
	it("renders dynamic Fugu models, filters native efforts, and refreshes", () => {
		const onRefresh = vi.fn();
		render(
			<ControlledMix
				initial={[bucket({ agent: "codex-fugu", model: "fugu", effort: "xhigh" })]}
				onRefresh={onRefresh}
			/>,
		);

		expect(screen.getByLabelText("Model")).toHaveValue("fugu");
		const effort = screen.getByLabelText("Effort");
		expect(effort).toHaveValue("xhigh");
		expect(
			within(effort)
				.getAllByRole("option")
				.map((option) => option.getAttribute("value")),
		).toEqual(["", "high", "xhigh"]);
		expect(screen.getByText(/known fallback catalog/i)).toBeInTheDocument();
		expect(screen.queryByText(/Status: unknown/i)).not.toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "Refresh model catalog" }));
		expect(onRefresh).toHaveBeenCalledOnce();
	});

	it("keeps long OpenCode model IDs intact and applies the catalog effort", () => {
		render(<ControlledMix initial={[bucket({ agent: "opencode" })]} />);

		expect(screen.getByText(/Manual model IDs are allowed/i)).toBeInTheDocument();
		const model = screen.getByLabelText("Model");
		const list = document.getElementById(model.getAttribute("list") ?? "");
		expect(list?.querySelector(`option[value="${longOpenCodeModel}"]`)).not.toBeNull();
		fireEvent.change(model, { target: { value: longOpenCodeModel } });

		expect(screen.getByLabelText("Model")).toHaveValue(longOpenCodeModel);
		expect(screen.getByLabelText("Effort")).toHaveValue("high");
		expect(screen.getByTestId("mix-state")).toHaveTextContent(longOpenCodeModel);
	});

	it("clears an unconfigured harness and restores remembered tuples on A to B to A", async () => {
		render(
			<ControlledMix
				initial={[
					bucket({
						model: "opus",
						effort: "high",
						selectionsByAgent: { "claude-code": { model: "opus", effort: "high" } },
					}),
				]}
			/>,
		);

		await chooseAgent("Codex Fugu");
		expect(screen.getByLabelText("Model")).toHaveValue("");
		expect(screen.getByLabelText("Effort")).toHaveValue("");

		fireEvent.change(screen.getByLabelText("Model"), { target: { value: "fugu" } });
		fireEvent.change(screen.getByLabelText("Effort"), { target: { value: "xhigh" } });
		await chooseAgent("Claude Code");
		expect(screen.getByLabelText("Model")).toHaveValue("opus");
		expect(screen.getByLabelText("Effort")).toHaveValue("high");

		await chooseAgent("Codex Fugu");
		expect(screen.getByLabelText("Model")).toHaveValue("fugu");
		expect(screen.getByLabelText("Effort")).toHaveValue("xhigh");
	});

	it("keeps the surviving row identity and tuple memory after removing an earlier row", async () => {
		render(
			<ControlledMix
				initial={[
					bucket({ id: "first", model: "opus", effort: "high" }),
					bucket({
						id: "second",
						agent: "opencode",
						model: longOpenCodeModel,
						effort: "high",
						weight: "50",
						selectionsByAgent: {
							opencode: { model: longOpenCodeModel, effort: "high" },
							"claude-code": { model: "sonnet", effort: "low" },
						},
					}),
				]}
			/>,
		);

		await userEvent.click(screen.getByRole("button", { name: "Remove bucket 1" }));
		expect(screen.getByLabelText("Model")).toHaveAttribute("id", "workerMix-second-model");
		await chooseAgent("Claude Code");
		expect(screen.getByLabelText("Model")).toHaveValue("sonnet");
		expect(screen.getByLabelText("Effort")).toHaveValue("low");
	});
});
