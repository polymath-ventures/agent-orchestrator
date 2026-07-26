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
 */
export function HarnessGlyph({ harness, className }: { harness: string; className?: string }) {
	const view = getHarnessGlyphView(harness);
	return (
		<span
			aria-label={view.label}
			className={cn(
				"relative inline-flex size-[13px] shrink-0 items-center justify-center rounded-[26%] leading-none",
				className,
			)}
			role="img"
			style={{ background: view.tile }}
			title={view.label}
		>
			{view.paths.length > 0 ? (
				<svg
					aria-hidden="true"
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
				<span aria-hidden="true" className="text-[6px] font-bold tracking-tight text-white">
					{view.monogram}
				</span>
			)}
			{view.pip ? (
				// A variant sharing another harness's mark (codex-fugu over codex). The
				// ring punches the pip out of whatever surface the row sits on.
				<span
					aria-hidden="true"
					className="absolute -right-px -bottom-px size-1 rounded-full ring-[1.5px] ring-sidebar"
					data-harness-pip=""
					style={{ background: view.pip }}
				/>
			) : null}
		</span>
	);
}
