import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentModelAvailabilityResponse } from "../hooks/useModelAvailabilityQuery";
import {
	buildModelCatalogView,
	catalogProvenanceLabel,
	ModelAvailabilityField,
	type ModelSelection,
} from "./ModelAvailabilityField";

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

describe("ModelAvailabilityField", () => {
	it("renders dynamic harness/model/effort choices, selected status, checked time, and fallback provenance", () => {
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
		expect(screen.getByText(/Status: unknown/i)).toBeInTheDocument();
		expect(screen.getByText(/not probed/i)).toBeInTheDocument();
		expect(screen.getByText(/known fallback catalog/i)).toBeInTheDocument();
		expect(screen.getByText(/installed catalog could not be read/i)).toBeInTheDocument();
		expect(screen.getByText(/Checked/)).toHaveAttribute("datetime", availability.checkedAt);

		fireEvent.click(screen.getByRole("button", { name: "Refresh model catalog" }));
		expect(onRefresh).toHaveBeenCalledOnce();
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

	it("accepts a long undiscovered provider model ID verbatim and clears stale effort", () => {
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
							efforts: ["medium", "high"],
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
		expect(onChange).toHaveBeenCalledWith({ harness: "opencode", model: unknownModel, effort: "" });
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
