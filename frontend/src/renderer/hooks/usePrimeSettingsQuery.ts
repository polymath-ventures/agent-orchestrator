import { useQuery } from "@tanstack/react-query";
import { apiClient } from "../lib/api-client";

export const primeSettingsQueryKey = ["prime", "settings"] as const;

/**
 * Whether Prime is *desired*, read from persisted daemon settings.
 *
 * Prime's presence in the supervisor UI must not be derived from the existence
 * of a live session row. When Prime dies, the row goes terminated — and if that
 * were the source of truth, Prime would vanish from the navigation exactly when
 * the operator needs to reach it to recover. Settings say what should be
 * running; session rows only say what is.
 */
export function usePrimeEnabledQuery() {
	return useQuery({
		queryKey: primeSettingsQueryKey,
		queryFn: async (): Promise<boolean> => {
			const { data, error } = await apiClient.GET("/api/v1/prime/settings");
			if (error) throw new Error("Failed to load Prime settings");
			return data?.settings?.enabled === true;
		},
		// A stale "enabled" only affects whether a nav entry renders, so a short
		// cache is fine and keeps the shell off the settings endpoint on every
		// workspace poll.
		staleTime: 30_000,
	});
}
