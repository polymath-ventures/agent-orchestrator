import { isMacPlatform } from "./platform";

export function hasElectronBridge(): boolean {
	return typeof window !== "undefined" && Boolean(window.ao);
}

// The traffic-light inset, draggable titlebar regions, and TitlebarNav cluster
// belong to the macOS Electron window only. Browser mode — including iPhone,
// iPad, iPadOS's desktop UA, and ordinary macOS browsers — must use normal web
// chrome instead (GH #54).
export function isMacDesktopChrome(): boolean {
	if (!hasElectronBridge() || typeof navigator === "undefined") return false;

	const userAgent = navigator.userAgent;
	const isAppleMobile = /iPod|iPhone|iPad/.test(userAgent);
	const isIPadDesktopUA = /Macintosh|Mac OS X/.test(userAgent) && navigator.maxTouchPoints > 1;

	return isMacPlatform() && !isAppleMobile && !isIPadDesktopUA;
}
