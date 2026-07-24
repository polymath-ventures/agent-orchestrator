import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";
import { AlertTriangle, RotateCcw } from "lucide-react";
import { useWorkspaceQuery, workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { usePrimeEnabledQuery } from "../hooks/usePrimeSettingsQuery";
import { findFleetPrime, primeSurfaceState } from "../types/workspace";
import { relaunchPrime } from "../lib/relaunch-prime";

/**
 * The Prime surface.
 *
 * Prime used to be reachable only through a live session row, so when Prime
 * died it disappeared from the UI entirely — exactly when the operator needed
 * to reach it. This route exists whenever Prime is *enabled*, and uses the
 * session row only to decide whether to hand off to the live terminal or offer
 * recovery.
 */
export function PrimeBoard() {
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const workspaceQuery = useWorkspaceQuery();
	const primeEnabledQuery = usePrimeEnabledQuery();
	const [isRelaunching, setIsRelaunching] = useState(false);
	const [error, setError] = useState<string | undefined>();

	const workspaces = workspaceQuery.data ?? [];
	const activePrime = findFleetPrime(workspaces);
	const state = primeSurfaceState(primeEnabledQuery.data === true, activePrime);

	// A live Prime has a real terminal; send the operator straight to it.
	useEffect(() => {
		if (state !== "running" || !activePrime) return;
		void navigate({ to: "/sessions/$sessionId", params: { sessionId: activePrime.id }, replace: true });
	}, [state, activePrime, navigate]);

	const onRelaunch = useCallback(async () => {
		if (isRelaunching) return;
		setIsRelaunching(true);
		setError(undefined);
		try {
			await relaunchPrime();
			await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
		} catch (err) {
			setError(err instanceof Error ? err.message : "Unable to relaunch Prime");
		} finally {
			setIsRelaunching(false);
		}
	}, [isRelaunching, queryClient]);

	if (workspaceQuery.isLoading || primeEnabledQuery.isLoading) {
		return <p className="py-10 text-center text-xs text-passive">Loading Prime…</p>;
	}

	if (state === "disabled") {
		return (
			<div className="p-4.5" data-testid="prime-disabled">
				<p className="text-xs text-muted-foreground">
					Prime is disabled. Enable it in Settings to run a fleet-wide supervisor.
				</p>
			</div>
		);
	}

	if (state === "running") {
		return <p className="py-10 text-center text-xs text-passive">Opening the Prime terminal…</p>;
	}

	return (
		<div className="p-4.5" data-testid="prime-not-running">
			<div className="flex items-start gap-3 rounded-md border border-border bg-surface px-3 py-3">
				<AlertTriangle className="mt-0.5 size-icon-base shrink-0 text-warning" aria-hidden="true" />
				<div className="min-w-0 flex-1">
					<div className="text-control text-foreground">Prime is enabled but not running</div>
					<p className="mt-1 text-xs leading-row text-muted-foreground">
						No Prime session is active. Relaunch Prime to start a fresh supervisor on the canonical branch — this also
						clears any paused automatic replacement.
					</p>
					{error && <p className="mt-2 text-xs text-destructive">{error}</p>}
				</div>
				<button
					type="button"
					className="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-border bg-raised px-2.5 py-1 text-xs text-foreground transition hover:bg-interactive-hover disabled:cursor-not-allowed disabled:opacity-50"
					disabled={isRelaunching}
					onClick={() => void onRelaunch()}
				>
					<RotateCcw className={isRelaunching ? "size-icon-base animate-spin" : "size-icon-base"} aria-hidden="true" />
					Relaunch Prime
				</button>
			</div>
		</div>
	);
}
