import { Gauge, Signal, SignalZero } from "lucide-react";
import type { components } from "../../api/schema";
import { useMetricsQuery } from "../hooks/useMetricsQuery";
import { cn } from "../lib/utils";

type QuotaSnapshot = components["schemas"]["QuotaSnapshot"];

export function QuotaPanel() {
	const metrics = useMetricsQuery();
	const quotas = metrics.data?.latest?.quotas ?? [];
	if (quotas.length === 0) return null;

	return (
		<div className="mb-3 flex min-h-row-lg items-center gap-3 rounded-panel border border-border bg-surface px-3 py-2">
			<div className="flex shrink-0 items-center gap-2 text-caption font-semibold uppercase tracking-wide-md text-muted-foreground">
				<Gauge className="size-icon-base" aria-hidden="true" />
				Quota
			</div>
			<div className="flex min-w-0 flex-1 flex-wrap gap-2">
				{quotas.map((quota) => (
					<QuotaChip quota={quota} key={quotaKey(quota)} />
				))}
			</div>
		</div>
	);
}

function QuotaChip({ quota }: { quota: QuotaSnapshot }) {
	const quality = quota.signalQuality;
	const remaining = quota.remaining;
	const limit = quota.limit;
	const pct = typeof remaining === "number" && typeof limit === "number" && limit > 0 ? (remaining / limit) * 100 : null;
	const tone =
		quality === "none"
			? "border-border text-passive"
			: pct !== null && pct <= 10
				? "border-warning/50 text-warning"
				: "border-success/40 text-success";
	const Icon = quality === "none" ? SignalZero : Signal;
	return (
		<div
			className={cn(
				"inline-flex min-h-control-md max-w-full items-center gap-2 rounded-md border bg-background px-2.5 py-1 font-mono text-2xs",
				tone,
			)}
			title={quota.basis || quota.source}
		>
			<Icon className="size-icon-sm shrink-0" aria-hidden="true" />
			<span className="min-w-0 truncate text-foreground">{quotaLabel(quota)}</span>
			<span className="shrink-0">{quotaValue(quota, pct)}</span>
			<span className="shrink-0 text-passive">{qualityLabel(quality)}</span>
			{hasWindowEnd(quota.windowEnd) ? <span className="shrink-0 text-passive">{windowLabel(quota.windowEnd)}</span> : null}
		</div>
	);
}

function quotaKey(quota: QuotaSnapshot): string {
	return [quota.harness, quota.accountId, quota.model, quota.windowStart, quota.windowEnd].filter(Boolean).join(":");
}

function quotaLabel(quota: QuotaSnapshot): string {
	const account = quota.accountId || "unknown";
	return quota.model ? `${quota.harness}/${account}/${quota.model}` : `${quota.harness}/${account}`;
}

function quotaValue(quota: QuotaSnapshot, pct: number | null): string {
	if (quota.signalQuality === "none") return "no signal";
	if (typeof quota.remaining === "number" && typeof quota.limit === "number") {
		return pct === null ? `${formatNumber(quota.remaining)}/${formatNumber(quota.limit)}` : `${pct.toFixed(1)}%`;
	}
	if (typeof quota.remaining === "number") return `${formatNumber(quota.remaining)} remaining`;
	return "unknown";
}

function qualityLabel(quality: QuotaSnapshot["signalQuality"]): string {
	switch (quality) {
		case "exact":
			return "exact";
		case "estimated":
			return "est.";
		default:
			return "none";
	}
}

function windowLabel(value: string): string {
	const end = new Date(value);
	if (Number.isNaN(end.getTime())) return "";
	return `until ${end.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
}

function hasWindowEnd(value: string | undefined): value is string {
	if (!value) return false;
	const end = new Date(value);
	return !Number.isNaN(end.getTime()) && end.getUTCFullYear() > 1;
}

function formatNumber(value: number): string {
	return new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value);
}
