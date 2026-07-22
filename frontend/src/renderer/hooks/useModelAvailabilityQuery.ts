import { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

export type AgentModelAvailabilityResponse = components["schemas"]["AgentModelAvailabilityResponse"];
export type AgentHarnessModels = components["schemas"]["AgentHarnessModels"];
export type AgentModelAvailability = components["schemas"]["AgentModelAvailability"];

export const modelAvailabilityQueryKey = ["agents", "models"] as const;

export async function fetchModelAvailability(
	options: { force?: boolean } = {},
): Promise<AgentModelAvailabilityResponse> {
	const { data, error } = await apiClient.GET("/api/v1/agents/models", {
		params: options.force ? { query: { force: true } } : undefined,
	});
	if (error) throw new Error(apiErrorMessage(error, "Could not load model availability"));
	return data as AgentModelAvailabilityResponse;
}

export const modelAvailabilityQueryOptions = {
	queryKey: modelAvailabilityQueryKey,
	queryFn: () => fetchModelAvailability(),
	retry: 1,
	staleTime: 5 * 60 * 1000,
};

export function useModelAvailabilityQuery(enabled = true) {
	return useQuery({ ...modelAvailabilityQueryOptions, enabled });
}

export function useRefreshModelAvailability() {
	const queryClient = useQueryClient();
	const [isRefreshing, setIsRefreshing] = useState(false);
	const refresh = useCallback(async () => {
		setIsRefreshing(true);
		try {
			return await queryClient.fetchQuery({
				queryKey: modelAvailabilityQueryKey,
				queryFn: () => fetchModelAvailability({ force: true }),
				staleTime: 0,
			});
		} finally {
			setIsRefreshing(false);
		}
	}, [queryClient]);
	return { refresh, isRefreshing };
}
