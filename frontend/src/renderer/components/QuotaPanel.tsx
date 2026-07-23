import { useIsMutating } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Gauge, Loader2, RefreshCw } from "lucide-react";
import type { components } from "../../api/schema";
import { useAgentsQuery } from "../hooks/useAgentsQuery";
import { useMetricsQuery } from "../hooks/useMetricsQuery";
import { probeQuotaMutationKey, useProbeQuota } from "../hooks/useProbeQuota";
import { formatTimeCompact } from "../lib/format-time";
import { useUiStore } from "../stores/ui-store";
import { cn } from "../lib/utils";

type QuotaSnapshot = components["schemas"]["QuotaSnapshot"];
type HarnessQuotaStatus = components["schemas"]["HarnessQuotaStatus"];
type ProbeMutation = ReturnType<typeof useProbeQuota>;

const MAX_REASON_LEN = 120;

/**
 * Sidebar-footer quota widget. Renders one chip per harness reported by the
 * daemon prober (`probeStatuses`), each showing an honest INLINE state — never a
 * tooltip-only error. Harness labels come from the shared agents inventory.
 */
export function QuotaPanel() {
	const metrics = useMetricsQuery();
	const agents = useAgentsQuery();
	const probe = useProbeQuota();
	const collapsed = useUiStore((state) => state.isQuotaWidgetCollapsed);
	const toggleCollapsed = useUiStore((state) => state.toggleQuotaWidgetCollapsed);

	// `busy` is derived from the GLOBAL in-flight count for the probe mutation key,
	// not this component's `probe.isPending`, so controls stay disabled across a
	// remount (e.g. toggling widget visibility mid-probe) until the POST settles.
	const busy = useIsMutating({ mutationKey: probeQuotaMutationKey }) > 0;

	const statuses = metrics.data?.probeStatuses ?? [];
	// The prober owns the authoritative harness set; nothing to show without it.
	if (statuses.length === 0) return null;

	const labelFor = (harness: string): string => {
		const inventory = [...(agents.data?.installed ?? []), ...(agents.data?.supported ?? [])];
		return inventory.find((agent) => agent.id === harness)?.label ?? harness;
	};

	// The spinner is a local best-effort hint on the clicked control; `busy` (above)
	// is the authoritative disable signal and survives remount.
	const refreshing = probe.isPending && !probe.variables?.harness;

	return (
		<div className="flex w-full flex-col overflow-hidden rounded-md border border-border bg-surface">
			<div className="flex items-center gap-1.5 px-2 py-1.5">
				<button
					aria-expanded={!collapsed}
					aria-label={collapsed ? "Expand quota widget" : "Collapse quota widget"}
					className="grid size-icon-lg shrink-0 place-items-center rounded text-passive transition-colors hover:bg-interactive-hover hover:text-foreground"
					onClick={toggleCollapsed}
					type="button"
				>
					{collapsed ? (
						<ChevronRight className="size-icon-sm" aria-hidden="true" />
					) : (
						<ChevronDown className="size-icon-sm" aria-hidden="true" />
					)}
				</button>
				<Gauge className="size-icon-sm shrink-0 text-passive" aria-hidden="true" />
				<span className="min-w-0 flex-1 truncate text-caption font-semibold uppercase tracking-wide-md text-muted-foreground">
					Quota
				</span>
				<button
					aria-label="Refresh all quota probes"
					className="grid size-icon-lg shrink-0 place-items-center rounded text-passive transition-colors hover:bg-interactive-hover hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
					disabled={busy}
					onClick={() => probe.mutate({})}
					type="button"
				>
					<RefreshCw className={cn("size-icon-sm", refreshing && "animate-spin")} aria-hidden="true" />
				</button>
			</div>
			{probe.isError && (
				<div className="px-2 pb-1.5">
					<span role="alert" className="text-micro text-warning">
						probe failed — retry
					</span>
				</div>
			)}
			{!collapsed && (
				<div className="flex flex-col gap-1 px-2 pb-2">
					{statuses.map((status) => (
						<HarnessChip
							key={status.harness}
							status={status}
							label={labelFor(status.harness)}
							snapshots={status.snapshots ?? []}
							probe={probe}
							busy={busy}
						/>
					))}
				</div>
			)}
		</div>
	);
}

function HarnessChip({
	status,
	label,
	snapshots,
	probe,
	busy,
}: {
	status: HarnessQuotaStatus;
	label: string;
	snapshots: QuotaSnapshot[];
	probe: ProbeMutation;
	busy: boolean;
}) {
	// spinning: local best-effort hint that this harness is the one being probed.
	// busy (from the parent) is the authoritative "any probe in flight" disable
	// signal — global and remount-safe — so two probes can never run at once.
	const spinning = probe.isPending && probe.variables?.harness === status.harness;
	const onProbe = () => probe.mutate({ harness: status.harness });
	const age = status.state === "ok" && status.probedAt ? formatTimeCompact(status.probedAt) : null;

	return (
		<div className="rounded border border-border bg-background px-2 py-1.5">
			<div className="flex items-center justify-between gap-2">
				<span className="min-w-0 truncate text-2xs font-medium text-foreground">{label}</span>
				{age ? <span className="shrink-0 text-micro text-passive">updated {age}</span> : null}
			</div>
			<div className="mt-1">
				<ChipBody
					status={status}
					snapshots={snapshots}
					label={label}
					onProbe={onProbe}
					spinning={spinning}
					busy={busy}
				/>
			</div>
		</div>
	);
}

function ChipBody({
	status,
	snapshots,
	label,
	onProbe,
	spinning,
	busy,
}: {
	status: HarnessQuotaStatus;
	snapshots: QuotaSnapshot[];
	label: string;
	onProbe: () => void;
	spinning: boolean;
	busy: boolean;
}) {
	switch (status.state) {
		case "ok":
			return status.hasData && snapshots.length > 0 ? (
				<UsageLines harness={status.harness} snapshots={snapshots} />
			) : (
				<InlineAction text="no usage recorded yet" label={label} onProbe={onProbe} spinning={spinning} busy={busy} />
			);
		case "not_probed":
			return <InlineAction text="not probed yet" label={label} onProbe={onProbe} spinning={spinning} busy={busy} />;
		case "failed":
			return (
				<InlineAction
					text={`probe failed: ${truncate(status.reason)}`}
					tone="warning"
					label={label}
					onProbe={onProbe}
					spinning={spinning}
					busy={busy}
				/>
			);
		case "no_source":
			return (
				<div className="flex flex-col gap-0.5">
					<span className="text-2xs text-passive">no machine-readable source</span>
					{status.reason ? <span className="text-micro text-passive">{truncate(status.reason)}</span> : null}
				</div>
			);
		default:
			return <span className="text-2xs text-passive">{status.state}</span>;
	}
}

function UsageLines({ harness, snapshots }: { harness: string; snapshots: QuotaSnapshot[] }) {
	const { headline, secondary } = pickWindows(harness, snapshots);
	if (!headline && !secondary) return <span className="text-2xs text-passive">no usage recorded yet</span>;
	return (
		<div className="flex flex-col gap-0.5">
			{headline ? <WindowLine snapshot={headline} prominent /> : null}
			{secondary ? <WindowLine snapshot={secondary} /> : null}
		</div>
	);
}

function WindowLine({ snapshot, prominent = false }: { snapshot: QuotaSnapshot; prominent?: boolean }) {
	const used = typeof snapshot.used === "number" ? Math.round(snapshot.used) : null;
	// used/limit are percentages (used 0–100); warn once ≤10% remains.
	const low = used !== null && used >= 90;
	const reset = formatReset(snapshot.windowEnd);
	// Always name the window — the headline was previously anonymous ("46% used"),
	// which hides WHICH limit (weekly vs session) the percentage measures.
	const name = snapshot.windowName;
	// Colour lives on the spans, not the container: a child's own colour overrides
	// the parent's inherited one, so a container-level `text-warning` would never
	// take effect. Keep the low-quota warning ("warn once ≤10% remains") by putting
	// it directly on each line.
	const usageColor = low ? "text-warning" : prominent ? "text-foreground" : "text-passive";
	const resetColor = low ? "text-warning" : "text-passive";
	// The reset gets its OWN line rather than being appended to a `truncate`d line:
	// in the narrow sidebar a one-liner clips the reset off the end, which is exactly
	// the "resets 2:00 PM with no date" bug. Within the first line the window name
	// truncates but the `% used` metric is `shrink-0`, so a long window name can
	// never hide the number. Both the named window and the full dated reset survive.
	return (
		<div className="flex flex-col font-mono">
			<span className={cn("flex min-w-0 items-baseline gap-1", prominent ? "text-2xs" : "text-micro", usageColor)}>
				{name ? <span className="min-w-0 truncate">{name}</span> : null}
				<span className="shrink-0">{used !== null ? `${used}% used` : "usage unknown"}</span>
			</span>
			{reset ? <span className={cn("text-micro", resetColor)}>resets {reset}</span> : null}
		</div>
	);
}

function InlineAction({
	text,
	tone,
	label,
	onProbe,
	spinning,
	busy,
}: {
	text: string;
	tone?: "warning";
	label: string;
	onProbe: () => void;
	spinning: boolean;
	busy: boolean;
}) {
	return (
		<div className="flex items-center justify-between gap-2">
			<span className={cn("min-w-0 flex-1 truncate text-2xs", tone === "warning" ? "text-warning" : "text-passive")}>
				{text}
			</span>
			<button
				aria-label={`Probe ${label}`}
				className="inline-flex shrink-0 items-center gap-1 rounded border border-border px-1.5 py-0.5 text-micro font-medium text-passive transition-colors hover:bg-interactive-hover hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
				disabled={busy}
				onClick={onProbe}
				type="button"
			>
				{spinning ? (
					<Loader2 className="size-icon-xs animate-spin" aria-hidden="true" />
				) : (
					<RefreshCw className="size-icon-xs" aria-hidden="true" />
				)}
				Probe now
			</button>
		</div>
	);
}

/**
 * Resolve the headline + optional secondary window for a harness. Known harness
 * vocabularies map to their named windows; anything else falls back to the
 * highest-usage window so a new harness still renders honestly.
 */
function pickWindows(
	harness: string,
	snapshots: QuotaSnapshot[],
): { headline?: QuotaSnapshot; secondary?: QuotaSnapshot } {
	if (harness === "claude-code") {
		const headline = snapshots.find((snap) => (snap.windowName ?? "").toLowerCase().includes("all models"));
		const secondary = snapshots.find((snap) => snap.windowName === "session");
		if (headline || secondary) return { headline, secondary };
	}
	if (harness === "codex") {
		const headline = snapshots.find((snap) => snap.windowName === "primary");
		const secondary = snapshots.find((snap) => snap.windowName === "secondary");
		if (headline || secondary) return { headline, secondary };
	}
	const sorted = [...snapshots].sort((a, b) => (b.used ?? 0) - (a.used ?? 0));
	return { headline: sorted[0], secondary: undefined };
}

/**
 * Format a reset instant as `3CharWeekday 3CharMonth DD HH:MM TZ` in 24-hour
 * time, e.g. `Mon Jul 27 14:00 EDT`. Always dated — a bare clock time is
 * ambiguous (a weekly reset days out reads as "today"), which is the whole bug
 * this widget had.
 *
 * Locale is pinned to `en-US` so the weekday/month names, Latin digits, and
 * field order are deterministic regardless of the host locale (an unpinned
 * formatter yields `lun. juil.` etc. and makes the output — and its tests —
 * environment-dependent). The timezone is left as the host's local zone (no
 * `timeZone` option) so the reset reads in the user's wall-clock, with a short
 * English zone name. `hourCycle: "h23"` forces 00–23; `hour12: false` alone can
 * resolve to an `h24` cycle that renders midnight as the ambiguous `24:00`.
 *
 * Returns null for a missing/zero/unparseable instant so the caller omits the
 * clause instead of rendering "Invalid Date".
 */
function formatReset(value: string | undefined): string | null {
	if (!value) return null;
	const end = new Date(value);
	if (Number.isNaN(end.getTime()) || end.getUTCFullYear() <= 1) return null;
	const fmt = new Intl.DateTimeFormat("en-US", {
		weekday: "short",
		month: "short",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		hourCycle: "h23",
		timeZoneName: "short",
	});
	const parts = fmt.formatToParts(end);
	const get = (type: Intl.DateTimeFormatPartTypes): string => parts.find((p) => p.type === type)?.value ?? "";
	const weekday = get("weekday");
	const month = get("month");
	const day = get("day");
	const hour = get("hour");
	const minute = get("minute");
	// timeZoneName:"short" effectively always yields a value; fall back to the
	// resolved IANA zone so the stamp is never zone-less (the contract requires TZ).
	const tz = get("timeZoneName") || fmt.resolvedOptions().timeZone;
	if (!weekday || !month || !day || !hour || !minute) return null;
	const stamp = `${weekday} ${month} ${day} ${hour}:${minute}`;
	return tz ? `${stamp} ${tz}` : stamp;
}

function truncate(value: string | undefined): string {
	if (!value) return "unknown";
	return value.length > MAX_REASON_LEN ? `${value.slice(0, MAX_REASON_LEN - 1)}…` : value;
}
