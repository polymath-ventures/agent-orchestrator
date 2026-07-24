import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

type PrimeSettingsView = components["schemas"]["PrimeSettingsView"];

/**
 * The one Prime settings query.
 *
 * This lives here rather than in the settings editor because two consumers need
 * it now: the settings form, and the shell/nav, which decides whether Prime is
 * *desired*. Two query keys for the same data would mean saving settings in one
 * place left the other reading a stale cache — the nav could keep claiming
 * Prime is enabled after it was turned off, and vice versa.
 */
export const primeSettingsQueryKey = ["prime-settings"] as const;

async function fetchPrimeSettings(): Promise<PrimeSettingsView> {
	const { data, error } = await apiClient.GET("/api/v1/prime/settings");
	if (error) throw new Error(apiErrorMessage(error));
	if (!data) throw new Error("Prime settings are unavailable.");
	return data;
}

export const primeSettingsQueryOptions = {
	queryKey: primeSettingsQueryKey,
	queryFn: fetchPrimeSettings,
	refetchInterval: 15_000,
} as const;

/**
 * Whether Prime is *desired*, read from persisted daemon settings.
 *
 * Prime's presence in the supervisor UI must not be derived from the existence
 * of a live session row. When Prime dies, the row goes terminated — and if that
 * were the source of truth, Prime would vanish from the navigation exactly when
 * the operator needs it to recover. Settings say what should be running;
 * session rows only say what is.
 *
 * Loading and error stay distinguishable from `enabled === false` so callers can
 * avoid telling an operator Prime is disabled when the request merely failed.
 */
export function usePrimeEnabledQuery() {
	return useQuery({
		...primeSettingsQueryOptions,
		select: (data: PrimeSettingsView) => data.settings?.enabled === true,
	});
}
