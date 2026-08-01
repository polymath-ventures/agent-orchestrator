import type { components } from "../../api/schema";
import type { AgentModelAvailabilityResponse } from "../hooks/useModelAvailabilityQuery";

export type AgentInfo = components["schemas"]["AgentInfo"];

export type AgentInventory = {
	authorized?: AgentInfo[];
	installed?: AgentInfo[];
	supported?: AgentInfo[];
};

export type AgentSelectionCatalog = AgentInventory;

// When a role's configured harness has dropped out of the polled model catalog
// (the daemon degrades per-harness and omits a harness whose binary is missing),
// the picker displays Claude Code instead of a stale/unavailable harness. This
// is the single default fallback harness.
export const FALLBACK_DISPLAY_HARNESS = "claude-code";

// The harness a picker should *display* for a configured selection. Display-only:
// callers must not write the result back into saved form state.
//
// The signal is the polled model-availability response — NOT the raw agent
// inventory. A missing-binary harness (e.g. opencode) is still listed in the
// inventory's `supported` set, but the daemon omits it from `/api/v1/agents/models`,
// so keying off availability is what actually catches the per-harness degradation
// this fixes. Returns the configured id unchanged when it is empty, when the
// catalog has not been polled yet (undefined — absence is not evidence of
// removal), or when the catalog still carries it; otherwise falls back to Claude.
export function effectiveDisplayHarness(
	configuredHarnessId: string,
	availability: AgentModelAvailabilityResponse | undefined,
): string {
	if (!configuredHarnessId) return configuredHarnessId;
	if (availability === undefined) return configuredHarnessId;
	const present = (availability.harnesses ?? []).some((harness) => harness.id === configuredHarnessId);
	return present ? configuredHarnessId : FALLBACK_DISPLAY_HARNESS;
}

export type AgentSelectionOptions = {
	current?: string;
	currentLabel?: string;
	includeDefault?: AgentInfo;
	requireAuthorized?: boolean;
	reviewerOnly?: boolean;
};

export function selectableAgentCatalog(
	catalog: AgentInventory | undefined,
	options: AgentSelectionOptions = {},
): AgentSelectionCatalog {
	const installedByID = agentMap(catalog?.installed);
	const authorizedByID = agentMap(catalog?.authorized);
	const supportedByID = agentMap(catalog?.supported);
	const selectableIDs = new Set([...installedByID.keys(), ...authorizedByID.keys()]);
	if (options.current) selectableIDs.add(options.current);
	if (options.includeDefault) selectableIDs.add(options.includeDefault.id);

	const allows = (id: string) => {
		if (options.includeDefault?.id === id) return true;
		const info = installedByID.get(id) ?? authorizedByID.get(id) ?? supportedByID.get(id);
		if (options.requireAuthorized && id !== options.current && !authorizedByID.has(id)) return false;
		if (options.reviewerOnly && !info?.reviewerCapable && id !== options.current) return false;
		return selectableIDs.has(id);
	};
	const selectableInfo = (id: string): AgentInfo | undefined => {
		if (options.includeDefault?.id === id) return options.includeDefault;
		const merged = mergeAgentInfo(supportedByID.get(id), installedByID.get(id), authorizedByID.get(id));
		if (merged) return merged;
		if (id === options.current) return { id, label: options.currentLabel ?? id, reviewerCapable: false };
		return undefined;
	};
	const fromIDs = (ids: Iterable<string>) =>
		[...ids]
			.map(selectableInfo)
			.filter((agent): agent is AgentInfo => agent !== undefined)
			.filter((agent) => allows(agent.id));

	const supportedIDs = new Set<string>(selectableIDs);
	const installedIDs = new Set(installedByID.keys());
	const authorizedIDs = new Set(authorizedByID.keys());
	if (options.includeDefault) supportedIDs.add(options.includeDefault.id);
	if (options.includeDefault) installedIDs.add(options.includeDefault.id);
	if (options.includeDefault) authorizedIDs.add(options.includeDefault.id);
	return {
		supported: fromIDs(supportedIDs),
		installed: fromIDs(installedIDs),
		authorized: fromIDs(authorizedIDs),
	};
}

export function modelAvailabilityFromAgentInventory(
	catalog: AgentInventory | undefined,
	options: AgentSelectionOptions = {},
): AgentModelAvailabilityResponse | undefined {
	const agentsByID = new Map<string, AgentInfo>();
	for (const agent of selectableAgentCatalog(catalog, options).supported ?? []) {
		agentsByID.set(agent.id, agent);
	}
	if (agentsByID.size === 0) return undefined;
	return {
		checkedAt: "",
		harnesses: [...agentsByID.values()]
			.map((agent) => ({
				id: agent.id,
				label: agent.label,
				reviewerCapable: agent.reviewerCapable,
				catalogSource: "none" as const,
				catalogVerified: false,
				catalogReason: "Model catalogs are unavailable; this harness comes from the installed agent inventory.",
				models: [],
			}))
			.sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id)),
	};
}

export function filterModelAvailabilityToSelectableAgents(
	availability: AgentModelAvailabilityResponse | undefined,
	catalog: AgentInventory | undefined,
	options: Pick<AgentSelectionOptions, "current" | "currentLabel" | "requireAuthorized" | "reviewerOnly"> = {},
): AgentModelAvailabilityResponse | undefined {
	if (!availability) return undefined;
	if (!catalog) return availability;
	const selectable = selectableAgentCatalog(catalog, options);
	const selectableIDs = new Set((selectable.supported ?? []).map((agent) => agent.id));
	const harnesses = (availability.harnesses ?? []).filter((harness) => selectableIDs.has(harness.id));
	const currentAgent = selectable.supported?.find((agent) => agent.id === options.current);
	if (
		options.current &&
		selectableIDs.has(options.current) &&
		!harnesses.some((harness) => harness.id === options.current)
	) {
		harnesses.push({
			id: options.current,
			label: currentAgent?.label ?? options.currentLabel ?? options.current,
			reviewerCapable: currentAgent?.reviewerCapable ?? false,
			catalogSource: "configured-pins",
			catalogVerified: false,
			catalogReason: "Harness is preserved from the current selection.",
			models: [],
		});
	}
	return {
		...availability,
		harnesses,
	};
}

export function agentDisplayLabel(
	catalog: AgentInventory | undefined,
	availability: AgentModelAvailabilityResponse | undefined,
	harness: string,
): string {
	const available = availability?.harnesses?.find((option) => option.id === harness);
	if (available?.label) return available.label;
	for (const agents of [catalog?.authorized, catalog?.installed, catalog?.supported]) {
		const agent = agents?.find((option) => option.id === harness);
		if (agent?.label) return agent.label;
	}
	return harness;
}

function agentMap(agents: AgentInfo[] | undefined) {
	return new Map((agents ?? []).map((agent) => [agent.id, agent]));
}

function mergeAgentInfo(...agents: Array<AgentInfo | undefined>): AgentInfo | undefined {
	return agents.reduce<AgentInfo | undefined>((merged, agent) => (agent ? { ...merged, ...agent } : merged), undefined);
}
