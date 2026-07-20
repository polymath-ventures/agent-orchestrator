import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage, hasTrustedApiBaseUrl } from "../lib/api-client";

export const metricsQueryKey = ["metrics"] as const;

export type MetricsResponse = components["schemas"]["MetricsResponse"];

export async function fetchMetrics(): Promise<MetricsResponse> {
	const { data, error, response } = await apiClient.GET("/api/v1/metrics");
	if (error) {
		if (response.status === 501) return { history: [] };
		throw new Error(apiErrorMessage(error, "Could not load metrics"));
	}
	return data ?? { history: [] };
}

export function useMetricsQuery() {
	return useQuery({
		queryKey: metricsQueryKey,
		queryFn: fetchMetrics,
		enabled: hasTrustedApiBaseUrl(),
		refetchInterval: 30_000,
		retry: 1,
	});
}
