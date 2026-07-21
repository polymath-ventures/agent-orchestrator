import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { ConfirmDialog } from "./ConfirmDialog";

export const fleetStatusQueryKey = ["fleet-status"] as const;

async function fetchFleetStatus(): Promise<boolean> {
	const { data, error } = await apiClient.GET("/api/v1/fleet");
	if (error) throw new Error(apiErrorMessage(error));
	return data?.paused ?? false;
}

// FleetSection is a drop-in Settings card for the daemon-global fleet pause.
// It reads the current pause status and exposes Pause (soft, drains workers at
// idle), Pause now (hard, terminates live workers — gated behind a confirm),
// and Resume. Every mutation invalidates both the fleet status and the
// workspace query so per-project pause badges refresh in the sidebar.
export function FleetSection() {
	const queryClient = useQueryClient();
	const [confirmHardOpen, setConfirmHardOpen] = useState(false);
	const query = useQuery({
		queryKey: fleetStatusQueryKey,
		queryFn: fetchFleetStatus,
	});

	const invalidate = () => {
		void queryClient.invalidateQueries({ queryKey: fleetStatusQueryKey });
		void queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
	};

	const pause = useMutation({
		mutationFn: async () => {
			const { error } = await apiClient.POST("/api/v1/fleet/pause");
			if (error) throw new Error(apiErrorMessage(error));
		},
		onSettled: invalidate,
	});

	const pauseHard = useMutation({
		mutationFn: async () => {
			const { error } = await apiClient.POST("/api/v1/fleet/pause", { params: { query: { hard: true } } });
			if (error) throw new Error(apiErrorMessage(error));
		},
		onSuccess: () => setConfirmHardOpen(false),
		onSettled: invalidate,
	});

	const resume = useMutation({
		mutationFn: async () => {
			const { error } = await apiClient.POST("/api/v1/fleet/resume");
			if (error) throw new Error(apiErrorMessage(error));
		},
		onSettled: invalidate,
	});

	const paused = query.data ?? false;
	// Until the status is known (loading or errored), disable the actions rather
	// than defaulting to "Running" — acting on an unknown state could pause/resume
	// the wrong way.
	const statusKnown = query.isSuccess;
	const busy = pause.isPending || pauseHard.isPending || resume.isPending || !statusKnown;
	const actionError = pause.error ?? pauseHard.error ?? resume.error;

	return (
		<>
			<Card>
				<CardHeader>
					<CardTitle className="text-control">Fleet</CardTitle>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<p className="text-xs leading-row text-muted-foreground">
						Pause the whole fleet to stop new work across every project. A soft pause lets live workers finish and drain
						at idle; a hard pause terminates them immediately and loses mid-flight work.
					</p>

					<div className="flex flex-col gap-2 text-xs">
						<div className="flex items-center gap-3">
							<span className="w-28 shrink-0 text-passive">Status</span>
							<span className="min-w-0 flex-1 truncate">
								{query.isLoading ? (
									<span className="text-passive">Checking…</span>
								) : query.isError ? (
									<span className="text-error">Unknown (daemon unreachable)</span>
								) : paused ? (
									<span className="text-muted-foreground">Paused</span>
								) : (
									<span className="text-success">Running</span>
								)}
							</span>
						</div>
					</div>

					{actionError && (
						<p className="text-xs leading-row text-error">
							{actionError instanceof Error ? actionError.message : "Request failed."}
						</p>
					)}

					<div className="flex items-center gap-3">
						{paused ? (
							<Button type="button" variant="primary" onClick={() => resume.mutate()} disabled={busy}>
								{resume.isPending && <Loader2 className="mr-2 size-icon-base animate-spin" />}
								Resume
							</Button>
						) : (
							<>
								<Button type="button" variant="primary" onClick={() => pause.mutate()} disabled={busy}>
									{pause.isPending && <Loader2 className="mr-2 size-icon-base animate-spin" />}
									Pause
								</Button>
								<Button
									type="button"
									variant="outline"
									onClick={() => setConfirmHardOpen(true)}
									disabled={busy}
									className="border-destructive text-destructive hover:bg-destructive/10"
								>
									Pause now (hard)
								</Button>
							</>
						)}
					</div>
				</CardContent>
			</Card>
			<ConfirmDialog
				open={confirmHardOpen}
				title="Hard pause the fleet?"
				description={
					<p className="text-sm text-muted-foreground">
						Hard pause terminates all live workers immediately. Mid-flight work is lost. Continue?
					</p>
				}
				confirmLabel={pauseHard.isPending ? "Pausing…" : "Pause now"}
				destructive
				busy={pauseHard.isPending}
				error={pauseHard.error instanceof Error ? pauseHard.error.message : null}
				onConfirm={() => pauseHard.mutate()}
				onOpenChange={(open) => {
					if (!pauseHard.isPending) setConfirmHardOpen(open);
				}}
			/>
		</>
	);
}
