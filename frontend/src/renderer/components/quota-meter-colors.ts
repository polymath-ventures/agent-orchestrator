export type QuotaSeverity = "normal" | "warning" | "critical";

/**
 * The colours the quota usage meter paints, in one place so the contrast guard
 * can measure exactly what ships.
 *
 * All four are shadcn roles upstream carries in every theme scope, which is the
 * property that matters here: the two tokens that used to do this job did not
 * have it. The 2026-08-07 sync deleted the fork-only `--color-quota-track` the
 * groove referenced, and its named-theme system redefined `--accent` — the
 * normal-severity fill — as a subtle hover surface holding the same value as
 * `--muted`, so the bar became the exact colour of its own track and vanished
 * below 75% (#289). `bg-foreground` on `bg-muted` is the only upstream pair
 * that clears 3:1 in every scope; `e2e/quota-meter-contrast.spec.ts` measures it
 * in a real browser and holds it there.
 *
 * This lives apart from `QuotaPanel.tsx` so that guard can import it without
 * pulling the renderer's React and preload-bridge types into the e2e program.
 */
export const QUOTA_METER_COLORS = {
	track: "bg-muted",
	fill: { normal: "bg-foreground", warning: "bg-warning", critical: "bg-error" },
} as const satisfies { track: string; fill: Record<QuotaSeverity, string> };
