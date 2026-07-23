import { PanelLeft } from "lucide-react";
import { hasElectronBridge } from "../lib/runtime-environment";
import { TopbarButton } from "./TopbarButton";
import { useSidebar } from "./ui/sidebar";

// Browser-mobile sidebar opener. In browser mode there is no macOS titlebar
// cluster and the sidebar collapses to an off-screen Sheet, so the app must
// surface an explicit opener or the sidebar becomes unreachable (GH #54).
// Self-gates to browser + mobile so callers can render it unconditionally:
// ShellTopbar hosts it inline in its header, and the shell renders it as a
// floating control on the hideShellTopbar routes (settings, first-launch
// welcome) where no topbar exists to carry it.
export function MobileSidebarOpener() {
	const { isMobile, openMobile, toggleSidebar } = useSidebar();
	if (hasElectronBridge() || !isMobile) return null;

	return (
		<TopbarButton
			aria-label={openMobile ? "Close sidebar" : "Open sidebar"}
			aria-pressed={openMobile}
			onClick={toggleSidebar}
			title={openMobile ? "Close sidebar" : "Open sidebar"}
			variant="icon"
		>
			<PanelLeft className="size-icon-lg" aria-hidden="true" />
		</TopbarButton>
	);
}
