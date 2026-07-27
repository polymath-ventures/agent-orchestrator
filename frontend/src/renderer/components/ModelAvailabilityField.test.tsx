import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentModelAvailabilityResponse } from "../hooks/useModelAvailabilityQuery";
import {
	buildModelCatalogView,
	catalogProvenanceLabel,
	ModelAvailabilityField,
	type ModelSelection,
} from "./ModelAvailabilityField";
import {
	filterModelAvailabilityToSelectableAgents,
	modelAvailabilityFromAgentInventory,
	selectableAgentCatalog,
} from "../lib/agent-selection";

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
					efforts: ["low", "high", "max"],
					defaultEffort: "high",
					dynamic: true,
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
					reasonCode: "not-probed",
				},
			],
		},
	],
};

function datalistValues(input: HTMLElement): string[] {
	const listID = input.getAttribute("list");
	if (!listID) return [];
	const list = document.getElementById(listID);
	return Array.from(list?.querySelectorAll("option") ?? []).map((option) => option.value);
}

describe("buildModelCatalogView", () => {
	it("keeps configured and current synthetic pins visible with native efforts", () => {
		const view = buildModelCatalogView(
			availability,
			{ harness: "opencode", model: "custom/provider-model", effort: "turbo" },
			[
				{ harness: "claude-code", model: "claude-version-pin", effort: "xhigh" },
				{ harness: "opencode", model: "custom/provider-model", effort: "turbo" },
			],
		);
		const claude = view.find((harness) => harness.id === "claude-code");
		const openCode = view.find((harness) => harness.id === "opencode");
		expect(claude?.models.map((model) => model.model)).toContain("claude-version-pin");
		expect(openCode?.catalogSource).toBe("configured-pins");
		expect(openCode?.models[0]).toMatchObject({ model: "custom/provider-model", synthetic: true });
		expect(openCode?.models[0].efforts).toContain("turbo");
	});
});

describe("shared agent/model selection helpers", () => {
	it("builds model fallback rows from installed inventory instead of the theoretical supported set", () => {
		const fallback = modelAvailabilityFromAgentInventory({
			supported: [
				{ id: "codex", label: "Codex", reviewerCapable: true },
				{ id: "kiro", label: "Kiro", reviewerCapable: false },
			],
			installed: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
			authorized: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
		});

		expect(fallback?.harnesses.map((harness) => harness.id)).toEqual(["codex"]);
	});

	it("filters model availability to selectable installed harnesses while preserving the current saved harness", () => {
		const filtered = filterModelAvailabilityToSelectableAgents(
			availability,
			{
				supported: [
					{ id: "claude-code", label: "Claude Code", reviewerCapable: true },
					{ id: "codex-fugu", label: "Codex Fugu", reviewerCapable: false },
					{ id: "kiro", label: "Kiro", reviewerCapable: false },
				],
				installed: [{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true }],
				authorized: [{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true }],
			},
			{ current: "codex-fugu" },
		);

		expect(filtered?.harnesses.map((harness) => harness.id)).toEqual(["claude-code", "codex-fugu"]);
	});

	it("synthesizes a missing current harness after filtering so configured selections remain selectable", () => {
		const filtered = filterModelAvailabilityToSelectableAgents(
			availability,
			{
				supported: [
					{ id: "claude-code", label: "Claude Code", reviewerCapable: true },
					{ id: "opencode", label: "OpenCode", reviewerCapable: true },
					{ id: "kiro", label: "Kiro", reviewerCapable: false },
				],
				installed: [{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true }],
				authorized: [{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true }],
			},
			{ current: "opencode" },
		);

		expect(filtered?.harnesses.map((harness) => harness.id)).toEqual(["claude-code", "opencode"]);
		expect(filtered?.harnesses.find((harness) => harness.id === "opencode")).toMatchObject({
			label: "OpenCode",
			catalogSource: "configured-pins",
			catalogVerified: false,
			models: [],
		});
	});

	it("can require authorized inventory while preserving the current saved harness", () => {
		const filtered = filterModelAvailabilityToSelectableAgents(
			{
				...availability,
				harnesses: [
					...availability.harnesses,
					{
						id: "cursor",
						label: "Cursor",
						reviewerCapable: false,
						catalogSource: "none",
						catalogVerified: false,
						models: [],
					},
				],
			},
			{
				supported: [
					{ id: "claude-code", label: "Claude Code", reviewerCapable: true },
					{ id: "cursor", label: "Cursor", reviewerCapable: false },
					{ id: "opencode", label: "OpenCode", reviewerCapable: true },
				],
				installed: [
					{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true },
					{ id: "cursor", label: "Cursor", authStatus: "unauthorized", reviewerCapable: false },
				],
				authorized: [{ id: "claude-code", label: "Claude Code", authStatus: "authorized", reviewerCapable: true }],
			},
			{ current: "opencode", requireAuthorized: true },
		);

		expect(filtered?.harnesses.map((harness) => harness.id)).toEqual(["claude-code", "opencode"]);
		expect(filtered?.harnesses.some((harness) => harness.id === "cursor")).toBe(false);
	});

	it("can build model fallback rows from authorized inventory only", () => {
		const fallback = modelAvailabilityFromAgentInventory(
			{
				supported: [
					{ id: "codex", label: "Codex", reviewerCapable: true },
					{ id: "cursor", label: "Cursor", reviewerCapable: false },
				],
				installed: [
					{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true },
					{ id: "cursor", label: "Cursor", authStatus: "unauthorized", reviewerCapable: false },
				],
				authorized: [{ id: "codex", label: "Codex", authStatus: "authorized", reviewerCapable: true }],
			},
			{ requireAuthorized: true },
		);

		expect(fallback?.harnesses.map((harness) => harness.id)).toEqual(["codex"]);
	});

	it("keeps model availability unchanged while the agent inventory is still unknown", () => {
		expect(filterModelAvailabilityToSelectableAgents(availability, undefined)).toBe(availability);
	});

	it("uses a friendly label for a current saved harness that is absent from inventory", () => {
		const catalog = selectableAgentCatalog(undefined, { current: "claude-code", currentLabel: "Claude Code" });

		expect(catalog.supported).toEqual([{ id: "claude-code", label: "Claude Code", reviewerCapable: false }]);
	});
});

describe("ModelAvailabilityField", () => {
	it("renders dynamic harness/model/effort choices, checked time, and fallback provenance, and suppresses a non-actionable status", () => {
		const onChange = vi.fn();
		const onRefresh = vi.fn();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "codex-fugu", model: "fugu", effort: "xhigh" }}
				onChange={onChange}
				availability={availability}
				onRefresh={onRefresh}
			/>,
		);

		expect(screen.getByLabelText("Harness")).toHaveValue("codex-fugu");
		expect(screen.getByLabelText("Model")).toHaveValue("fugu");
		expect(screen.getByLabelText("Effort")).toHaveValue("xhigh");
		// A non-actionable status is never rendered — it used to depend on every
		// caller passing statusVisibility="actionable", and now holds by
		// construction. Provenance and catalog warnings are unaffected.
		expect(screen.queryByText(/Status: unknown/i)).not.toBeInTheDocument();
		expect(screen.queryByText(/not probed/i)).not.toBeInTheDocument();
		expect(screen.getByText(/known fallback catalog/i)).toBeInTheDocument();
		expect(screen.getByText(/installed catalog could not be read/i)).toBeInTheDocument();
		expect(screen.getByText(/Checked/)).toHaveAttribute("datetime", availability.checkedAt);

		fireEvent.click(screen.getByRole("button", { name: "Refresh model catalog" }));
		expect(onRefresh).toHaveBeenCalledOnce();
	});

	it("uses field-specific empty labels for harness, model, and effort", () => {
		render(
			<ModelAvailabilityField
				id="prime-model"
				label="Prime model and effort"
				value={{ harness: "", model: "", effort: "" }}
				onChange={vi.fn()}
				availability={availability}
				harnessEmptyLabel="Select harness"
				modelEmptyLabel="Select model"
				effortEmptyLabel="Select effort"
			/>,
		);

		expect(screen.getByLabelText("Harness").querySelector('option[value=""]')).toHaveTextContent("Select harness");
		expect(screen.getByLabelText("Model")).toHaveAttribute("placeholder", "Select model");
		expect(screen.getByLabelText("Effort")).toHaveAttribute("placeholder", "Select effort");
	});

	it("hides advisory status in actionable mode but keeps unreachable warnings", () => {
		const unreachableAvailability: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: availability.harnesses.map((harness) =>
				harness.id === "codex-fugu"
					? {
							...harness,
							models: [
								...harness.models,
								{
									model: "retired-model",
									label: "Retired model",
									efforts: ["high"],
									defaultEffort: "high",
									verified: false,
									status: "unreachable" as const,
									reason: "model rejected by provider",
								},
							],
						}
					: harness,
			),
		};
		const { rerender } = render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "codex-fugu", model: "fugu", effort: "xhigh" }}
				onChange={vi.fn()}
				availability={unreachableAvailability}
			/>,
		);

		expect(screen.queryByText(/Status: unknown/i)).not.toBeInTheDocument();
		expect(screen.queryByText(/not probed/i)).not.toBeInTheDocument();

		rerender(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "codex-fugu", model: "retired-model", effort: "high" }}
				onChange={vi.fn()}
				availability={unreachableAvailability}
			/>,
		);

		expect(screen.getByText(/Status: unreachable/i)).toBeInTheDocument();
		expect(screen.getByText(/model rejected by provider/i)).toBeInTheDocument();
	});

	it("contains a rejected refresh so the last successful catalog remains usable", async () => {
		const onRefresh = vi.fn().mockRejectedValue(new Error("refresh unavailable"));
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "codex-fugu", model: "fugu", effort: "xhigh" }}
				onChange={vi.fn()}
				availability={availability}
				onRefresh={onRefresh}
			/>,
		);

		fireEvent.click(screen.getByRole("button", { name: "Refresh model catalog" }));
		await waitFor(() => expect(onRefresh).toHaveBeenCalledOnce());
		expect(screen.getByLabelText("Model")).toHaveValue("fugu");
	});

	it("applies model native default effort when model changes", () => {
		const expanded: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: availability.harnesses.map((harness) =>
				harness.id === "claude-code"
					? {
							...harness,
							models: [
								...harness.models,
								{
									model: "sonnet",
									label: "Sonnet",
									efforts: ["low", "medium"],
									defaultEffort: "medium",
									verified: true,
									status: "reachable" as const,
								},
							],
						}
					: harness,
			),
		};
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "claude-code", model: "opus", effort: "high" }}
				onChange={onChange}
				availability={expanded}
			/>,
		);

		fireEvent.change(screen.getByLabelText("Model"), { target: { value: "sonnet" } });
		expect(onChange).toHaveBeenCalledWith({ harness: "claude-code", model: "sonnet", effort: "medium" });
	});

	it("accepts a long undiscovered provider model ID verbatim and preserves valid effort", () => {
		const openCodeAvailability: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: [
				...availability.harnesses,
				{
					id: "opencode",
					label: "OpenCode",
					reviewerCapable: true,
					catalogSource: "adapter",
					catalogVerified: true,
					models: [
						{
							model: "openai/gpt-5.4",
							label: "OpenAI GPT-5.4",
							efforts: ["medium", "high", "turbo"],
							defaultEffort: "medium",
							verified: true,
							status: "reachable",
						},
					],
				},
			],
		};
		const unknownModel = "private-cloud-provider/teams/research/models/experimental-reasoner-2026-07-22";
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "opencode", model: "openai/gpt-5.4", effort: "high" }}
				onChange={onChange}
				availability={openCodeAvailability}
			/>,
		);

		const modelControl = screen.getByLabelText("Model");
		expect(modelControl.tagName).toBe("INPUT");
		expect(modelControl).toHaveAttribute("list", "worker-model-model-options");
		fireEvent.change(modelControl, { target: { value: unknownModel } });
		expect(onChange).toHaveBeenCalledWith({ harness: "opencode", model: unknownModel, effort: "high" });
	});

	it("offers native effort suggestions while allowing manual effort values", () => {
		const openCodeAvailability: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: [
				...availability.harnesses,
				{
					id: "opencode",
					label: "OpenCode",
					reviewerCapable: true,
					catalogSource: "adapter",
					catalogVerified: true,
					models: [
						{
							model: "openai/gpt-5.4",
							label: "OpenAI GPT-5.4",
							efforts: ["medium", "high", "turbo"],
							defaultEffort: "medium",
							verified: true,
							status: "reachable",
						},
					],
				},
			],
		};
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "opencode", model: "private/provider-model", effort: "xhigh" }}
				onChange={onChange}
				availability={openCodeAvailability}
				showManualModelNotice
			/>,
		);

		const effortControl = screen.getByLabelText("Effort");
		expect(effortControl.tagName).toBe("INPUT");
		expect(effortControl).toHaveValue("xhigh");
		expect(effortControl).toHaveAttribute("list", "worker-model-effort-options");
		expect(screen.getByText(/Manual model IDs are allowed/i)).toBeInTheDocument();
		expect(screen.getByText(/Manual effort values are allowed/i)).toBeInTheDocument();
		expect(datalistValues(effortControl)).toEqual(["xhigh", "medium", "high", "turbo"]);

		fireEvent.change(effortControl, { target: { value: "turbo" } });
		expect(onChange).toHaveBeenCalledWith({ harness: "opencode", model: "private/provider-model", effort: "turbo" });
	});

	it("keeps verified adapter catalog model efforts constrained to the catalog", () => {
		const openCodeAvailability: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: [
				...availability.harnesses,
				{
					id: "opencode",
					label: "OpenCode",
					reviewerCapable: true,
					catalogSource: "adapter",
					catalogVerified: true,
					models: [
						{
							model: "openai/gpt-5.4",
							label: "OpenAI GPT-5.4",
							efforts: ["medium", "high", "turbo"],
							defaultEffort: "medium",
							verified: true,
							status: "reachable",
						},
					],
				},
			],
		};
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "opencode", model: "openai/gpt-5.4", effort: "turbo" }}
				onChange={onChange}
				availability={openCodeAvailability}
				showManualModelNotice
			/>,
		);

		const effortControl = screen.getByLabelText("Effort");
		expect(effortControl.tagName).toBe("SELECT");
		expect(effortControl.querySelector('option[value="medium"]')).toBeInTheDocument();
		expect(effortControl.querySelector('option[value="high"]')).toBeInTheDocument();
		expect(effortControl.querySelector('option[value="turbo"]')).toBeInTheDocument();
		expect(screen.queryByText(/Manual effort values are allowed/i)).not.toBeInTheDocument();
	});

	it("keeps zero-effort catalog models editable after pinning a manual effort", () => {
		const openCodeAvailability: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: [
				...availability.harnesses,
				{
					id: "opencode",
					label: "OpenCode",
					reviewerCapable: true,
					catalogSource: "adapter",
					catalogVerified: true,
					models: [
						{
							model: "openai/gpt-no-variants",
							label: "GPT No Variants",
							verified: true,
							status: "reachable",
						},
					],
				},
			],
		};
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "opencode", model: "openai/gpt-no-variants", effort: "high" }}
				onChange={onChange}
				availability={openCodeAvailability}
				showManualModelNotice
			/>,
		);

		const effortControl = screen.getByLabelText("Effort");
		expect(effortControl.tagName).toBe("INPUT");
		expect(effortControl).toHaveValue("high");
		expect(datalistValues(effortControl)).toEqual(["high"]);
		expect(screen.getByText(/Manual effort values are allowed/i)).toBeInTheDocument();

		fireEvent.change(effortControl, { target: { value: "turbo" } });
		expect(onChange).toHaveBeenCalledWith({ harness: "opencode", model: "openai/gpt-no-variants", effort: "turbo" });
	});

	it("does not use sibling effort suggestions for a known model with omitted efforts", () => {
		const openCodeAvailability: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: [
				...availability.harnesses,
				{
					id: "opencode",
					label: "OpenCode",
					reviewerCapable: true,
					catalogSource: "adapter",
					catalogVerified: true,
					models: [
						{
							model: "openai/gpt-no-variants",
							label: "GPT No Variants",
							verified: true,
							status: "reachable",
						},
						{
							model: "openai/gpt-5.4",
							label: "OpenAI GPT-5.4",
							efforts: ["medium", "high"],
							defaultEffort: "medium",
							verified: true,
							status: "reachable",
						},
					],
				},
			],
		};
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "opencode", model: "openai/gpt-no-variants", effort: "" }}
				onChange={vi.fn()}
				availability={openCodeAvailability}
				showManualModelNotice
			/>,
		);

		const effortControl = screen.getByLabelText("Effort");
		expect(effortControl.tagName).toBe("INPUT");
		expect(datalistValues(effortControl)).toEqual([]);
	});

	it("keeps authoritative harness efforts constrained to the catalog", () => {
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "claude-code", model: "opus", effort: "high" }}
				onChange={vi.fn()}
				availability={availability}
			/>,
		);

		const effortControl = screen.getByLabelText("Effort");
		expect(effortControl.tagName).toBe("SELECT");
		expect(effortControl.querySelector('option[value="minimal"]')).not.toBeInTheDocument();
		expect(screen.queryByText(/Manual effort values are allowed/i)).not.toBeInTheDocument();
	});

	it("uses the selected harness effort union as suggestions when model is blank", () => {
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "claude-code", model: "", effort: "" }}
				onChange={vi.fn()}
				availability={availability}
				showManualModelNotice
			/>,
		);

		const effortControl = screen.getByLabelText("Effort");
		expect(effortControl.tagName).toBe("INPUT");
		expect(datalistValues(effortControl)).toEqual(["low", "high", "max"]);
		expect(datalistValues(effortControl)).not.toContain("minimal");
		expect(screen.getByText(/Manual effort values are allowed/i)).toBeInTheDocument();
		expect(screen.queryByText(/Effort choices use AO's supported effort values/i)).not.toBeInTheDocument();
	});

	it("uses native effort suggestions for a blank model", () => {
		const openCodeAvailability: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: [
				...availability.harnesses,
				{
					id: "opencode",
					label: "OpenCode",
					reviewerCapable: true,
					catalogSource: "adapter",
					catalogVerified: true,
					models: [
						{
							model: "openai/gpt-5.4",
							label: "OpenAI GPT-5.4",
							efforts: ["high", "none"],
							defaultEffort: "high",
							verified: true,
							status: "reachable",
						},
					],
				},
			],
		};
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "opencode", model: "", effort: "" }}
				onChange={vi.fn()}
				availability={openCodeAvailability}
				showManualModelNotice
			/>,
		);

		const effortControl = screen.getByLabelText("Effort");
		expect(effortControl.tagName).toBe("INPUT");
		expect(datalistValues(effortControl)).toEqual(["high", "none"]);
		expect(screen.getByText(/Manual effort values are allowed/i)).toBeInTheDocument();
	});

	it("restores the configured model and effort when switching harnesses", () => {
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "claude-code", model: "opus", effort: "high" }}
				onChange={onChange}
				availability={availability}
				configuredPins={[{ harness: "codex-fugu", model: "fugu", effort: "xhigh" }]}
			/>,
		);

		fireEvent.change(screen.getByLabelText("Harness"), { target: { value: "codex-fugu" } });
		expect(onChange).toHaveBeenCalledWith({ harness: "codex-fugu", model: "fugu", effort: "xhigh" });
	});

	it("restores a synthetic model pin's own effort when selecting that model", () => {
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "claude-code", model: "opus", effort: "high" }}
				onChange={onChange}
				availability={availability}
				configuredPins={[{ harness: "claude-code", model: "claude-version-pin", effort: "low" }]}
			/>,
		);

		fireEvent.change(screen.getByLabelText("Model"), { target: { value: "claude-version-pin" } });
		expect(onChange).toHaveBeenCalledWith({ harness: "claude-code", model: "claude-version-pin", effort: "low" });
	});

	it("clears stale model and effort when switching to a harness without a configured pin", () => {
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "claude-code", model: "opus", effort: "high" }}
				onChange={onChange}
				availability={availability}
			/>,
		);

		fireEvent.change(screen.getByLabelText("Harness"), { target: { value: "codex-fugu" } });
		expect(onChange).toHaveBeenCalledWith({ harness: "codex-fugu", model: "", effort: "" });
	});

	it("restores an effort-only configured pair when switching harnesses", () => {
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "claude-code", model: "opus", effort: "high" }}
				onChange={onChange}
				availability={availability}
				configuredPins={[{ harness: "codex-fugu", model: "", effort: "xhigh" }]}
			/>,
		);

		fireEvent.change(screen.getByLabelText("Harness"), { target: { value: "codex-fugu" } });
		expect(onChange).toHaveBeenCalledWith({ harness: "codex-fugu", model: "", effort: "xhigh" });
	});

	it("keeps per-field <label> elements but visually hides them when fieldLabelsVisible is false", () => {
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model and effort"
				value={{ harness: "codex-fugu", model: "fugu", effort: "xhigh" }}
				onChange={vi.fn()}
				availability={availability}
				showHarness
				fieldLabelsVisible={false}
			/>,
		);

		// Real <label htmlFor> elements stay in the DOM — for native label-click
		// focus behavior and reliable a11y-tree association — just visually
		// hidden, not swapped for aria-label.
		for (const text of ["Harness", "Model", "Effort"]) {
			const label = screen.getByText(text, { selector: "label" });
			expect(label).toHaveClass("sr-only");
			expect(label).not.toHaveClass("text-[11px]");
		}
		expect(screen.getByLabelText("Harness")).toHaveValue("codex-fugu");
		expect(screen.getByLabelText("Model")).toHaveValue("fugu");
		expect(screen.getByLabelText("Effort")).toHaveValue("xhigh");
	});

	it("shows visible per-field labels by default", () => {
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model and effort"
				value={{ harness: "codex-fugu", model: "fugu", effort: "xhigh" }}
				onChange={vi.fn()}
				availability={availability}
				showHarness
			/>,
		);

		for (const text of ["Harness", "Model", "Effort"]) {
			const label = screen.getByText(text, { selector: "label" });
			expect(label).not.toHaveClass("sr-only");
			expect(label).toHaveClass("text-[11px]");
		}
	});

	it("keeps a zero-row harness usable through its configured synthetic pin", () => {
		const zeroRowAvailability: AgentModelAvailabilityResponse = {
			...availability,
			harnesses: [
				...availability.harnesses,
				{
					id: "opencode",
					label: "OpenCode",
					reviewerCapable: true,
					catalogSource: "none",
					catalogVerified: false,
					models: [],
				},
			],
		};
		const onChange = vi.fn<(selection: ModelSelection) => void>();
		render(
			<ModelAvailabilityField
				id="worker-model"
				label="Worker model"
				value={{ harness: "claude-code", model: "opus", effort: "high" }}
				onChange={onChange}
				availability={zeroRowAvailability}
				configuredPins={[{ harness: "opencode", model: "provider/model", effort: "turbo" }]}
			/>,
		);

		fireEvent.change(screen.getByLabelText("Harness"), { target: { value: "opencode" } });
		expect(onChange).toHaveBeenCalledWith({ harness: "opencode", model: "provider/model", effort: "turbo" });
	});
});

describe("catalogProvenanceLabel", () => {
	it("is silent for verified native catalogs and explicit for every fallback", () => {
		expect(catalogProvenanceLabel(availability.harnesses[0])).toBe("");
		expect(catalogProvenanceLabel(availability.harnesses[1])).toContain("known fallback catalog");
	});
});
