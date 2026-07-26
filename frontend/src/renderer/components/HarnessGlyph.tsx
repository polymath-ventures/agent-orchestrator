import { getHarnessGlyphView } from "../lib/harness-glyphs";
import { cn } from "../lib/utils";

/**
 * The harness a session is running, as a chip sized to sit beside the sidebar's
 * status dot.
 *
 * The session name deliberately omits the harness (see the session-naming
 * spec: the name is capped at 20 runes and encoding the harness textually
 * muddies the identity it carries), so this is where the harness becomes
 * visible. It is chrome, not content: fixed width, never wrapping, and it
 * takes no space from the name beyond its own slot.
 *
 * The chip is deliberately `aria-hidden`. Every row it sits in is a control
 * carrying its own `aria-label`, and a labelled control drops its descendants
 * from the accessible-name computation — a chip that labelled itself would
 * simply never be announced. The row describes the harness instead
 * (`SessionMarkers` in Sidebar.tsx), and `title` covers the hover case.
 */
export function HarnessGlyph({ harness, className }: { harness: string; className?: string }) {
	const view = getHarnessGlyphView(harness);
	return (
		<span
			aria-hidden="true"
			className={cn(
				"relative inline-flex size-[13px] shrink-0 items-center justify-center overflow-hidden rounded-[26%] leading-none",
				className,
			)}
			data-harness-glyph=""
			style={{ background: view.tile }}
			title={view.label}
		>
			{view.paths.length > 0 ? (
				<svg
					clipRule="evenodd"
					fill="#fff"
					fillRule="evenodd"
					// Marks differ in how much of their own box they fill, so each carries
					// the scale that makes it read at 13px rather than a shared inset.
					style={{ width: `${view.markScale}%`, height: `${view.markScale}%` }}
					viewBox="0 0 24 24"
				>
					{view.paths.map((d) => (
						<path d={d} key={d} />
					))}
				</svg>
			) : (
				<span className="text-[6px] font-bold tracking-tight text-white">{view.monogram}</span>
			)}
			{view.pip ? (
				// A variant sharing another harness's mark (codex-fugu over codex). It
				// sits inside the chip, separated from the mark by the chip's own tile,
				// so it reads identically on a hovered or active row — an outward ring
				// would have to guess the row's current background and get it wrong.
				<span
					className="absolute right-0 bottom-0 size-[4px] rounded-full"
					data-harness-pip=""
					style={{ background: view.pip }}
				/>
			) : null}
		</span>
	);
}
