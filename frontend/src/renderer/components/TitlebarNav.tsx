import { useCanGoBack, useRouter } from "@tanstack/react-router";
import { ArrowLeft, ArrowRight, PanelLeft } from "lucide-react";
import { useEffect, useState } from "react";
import { isMacDesktopChrome } from "../lib/runtime-environment";
import { useSidebar } from "./ui/sidebar";

const isMac = isMacDesktopChrome();
const noDragStyle = isMac ? ({ WebkitAppRegion: "no-drag" } as React.CSSProperties) : undefined;

// macOS-only sidebar chrome cluster (sidebar toggle + history arrows). Lives
// in the Sidebar header below the traffic lights while windowed; in native
// fullscreen the clearance pad drops so this cluster sits near the top edge.
// The toggle is pinned in the icon-rail column so it never moves during
// expand/collapse; history arrows sit absolutely to its right and only fade
// in when expanded.
// The installed router has no useCanGoForward, and deriving one as
// `__TSR_index < history.length - 1` (the upstream hook's approach) is wrong
// here: window.history.length also counts entries the router never created —
// the WebContents' initial blank entry, pre-router loads — so the tip of the
// stack still reads as "forward available" and the arrow no-ops. Instead,
// track the highest router index reachable on the live stack: a PUSH discards
// the forward entries (the new index is the tip); BACK/FORWARD/GO only move
// within it. After a mid-stack reload the tip resets to the current entry —
// forward greys out rather than dangle on entries we can no longer see.
function useCanGoForward(): boolean {
	const router = useRouter();
	const [canGoForward, setCanGoForward] = useState(false);
	useEffect(() => {
		let tip = router.history.location.state.__TSR_index;
		return router.history.subscribe(({ location, action }) => {
			const index = location.state.__TSR_index;
			tip = action.type === "PUSH" ? index : Math.max(tip, index);
			setCanGoForward(index < tip);
		});
	}, [router]);
	return canGoForward;
}

export function TitlebarNav({ historyLocked = false }: { historyLocked?: boolean }) {
	// Drive the sidebar through the SidebarProvider context, not the ui-store
	// bool: the context toggle is viewport-aware. Below MOBILE_BREAKPOINT the
	// sidebar is a Sheet controlled by `openMobile`, so the store bool never
	// opens it (GH #46). The desktop path still round-trips through the
	// provider's onOpenChange in _shell, which persists the ui-store preference.
	const { open, openMobile, isMobile, toggleSidebar } = useSidebar();
	const isSidebarOpen = isMobile ? openMobile : open;
	const router = useRouter();
	const canGoBack = useCanGoBack();
	const canGoForward = useCanGoForward();

	if (!isMac) return null;

	return (
		<div
			className="titlebar-nav fixed top-0 left-titlebar-cluster-left z-titlebar flex h-toolbar items-center gap-1"
			style={noDragStyle}
		>
			<TitlebarButton
				label={isSidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
				onClick={toggleSidebar}
				title={`${isSidebarOpen ? "Collapse" : "Expand"} sidebar · ⌘B`}
			>
				<PanelLeft className="size-icon-lg" aria-hidden="true" />
			</TitlebarButton>
			<TitlebarButton
				disabled={historyLocked || !canGoBack}
				label="Go back"
				onClick={() => router.history.back()}
				title="Go back"
			>
				<ArrowLeft className="size-icon-lg" aria-hidden="true" />
			</TitlebarButton>
			<TitlebarButton
				disabled={historyLocked || !canGoForward}
				label="Go forward"
				onClick={() => router.history.forward()}
				title="Go forward"
			>
				<ArrowRight className="size-icon-lg" aria-hidden="true" />
			</TitlebarButton>
		</div>
	);
}

function TitlebarButton({
	label,
	title,
	disabled,
	tabIndex,
	onClick,
	children,
}: {
	label: string;
	title: string;
	disabled?: boolean;
	tabIndex?: number;
	onClick: () => void;
	children: React.ReactNode;
}) {
	return (
		<button
			aria-label={label}
			aria-disabled={disabled || undefined}
			className="grid size-control-md place-items-center rounded-md text-passive transition-colors hover:bg-interactive-hover hover:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-45 disabled:hover:bg-transparent disabled:hover:text-passive"
			disabled={disabled}
			onClick={onClick}
			tabIndex={tabIndex}
			title={title}
			type="button"
		>
			{children}
		</button>
	);
}
