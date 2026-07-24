import { apiClient, apiErrorMessage } from "./api-client";
import { captureRendererEvent } from "./telemetry";

/** Relaunch the fleet Prime singleton via the daemon API.
 *
 *  Deliberately its own endpoint rather than the generic session spawn or
 *  restore paths: both of those are forbidden for Prime, because Prime is a
 *  supervisor-managed singleton. Relaunch routes through the same reconciliation
 *  the supervisor uses, so it clears any restart-budget pause, recovers a
 *  terminated Prime, and cannot create a second one. Calling it while a healthy
 *  Prime is running returns that Prime unchanged. */
export async function relaunchPrime(): Promise<string> {
	void captureRendererEvent("ao.renderer.prime_relaunch_requested", {});
	try {
		const { data, error, response } = await apiClient.POST("/api/v1/prime/relaunch", {});

		if (error || !data?.session?.id) {
			const message = error
				? apiErrorMessage(error, `Failed to relaunch Prime (${response.status})`)
				: `Failed to relaunch Prime (${response.status})`;
			throw new Error(message);
		}

		void captureRendererEvent("ao.renderer.prime_relaunch_succeeded", {});
		return data.session.id;
	} catch (err) {
		void captureRendererEvent("ao.renderer.prime_relaunch_failed", {});
		throw err;
	}
}
