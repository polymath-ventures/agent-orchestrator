import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { metricsQueryKey } from "./useMetricsQuery";

export type ProbeQuotaResponse = components["schemas"]["ProbeQuotaResponse"];

/** Variables for a force-probe: omit `harness` to probe every configured harness. */
export type ProbeQuotaVariables = { harness?: string };

async function probeQuota({ harness }: ProbeQuotaVariables): Promise<ProbeQuotaResponse> {
	const { data, error } = await apiClient.POST("/api/v1/metrics/probe", {
		body: { harness },
	});
	if (error) throw new Error(apiErrorMessage(error, "Could not probe quota"));
	return data ?? { statuses: [] };
}

/**
 * Force a daemon quota probe for one harness (`{ harness }`) or all of them
 * (`{}`). On success the `["metrics"]` query is invalidated so the widget
 * re-renders with the freshly persisted snapshots and probe statuses.
 */
export function useProbeQuota() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: probeQuota,
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: metricsQueryKey });
		},
	});
}
