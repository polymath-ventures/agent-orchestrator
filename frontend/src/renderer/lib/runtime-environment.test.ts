import { afterEach, describe, expect, it } from "vitest";
import { isMacDesktopChrome } from "./runtime-environment";

const originalUserAgent = navigator.userAgent;
const originalMaxTouchPoints = navigator.maxTouchPoints;
const originalBridge = window.ao;

function setEnvironment(userAgent: string, maxTouchPoints: number, electron = false) {
	Object.defineProperty(navigator, "userAgent", { configurable: true, value: userAgent });
	Object.defineProperty(navigator, "maxTouchPoints", { configurable: true, value: maxTouchPoints });
	Object.defineProperty(window, "ao", {
		configurable: true,
		value: electron ? ({} as Window["ao"]) : undefined,
	});
}

afterEach(() => {
	Object.defineProperty(navigator, "userAgent", { configurable: true, value: originalUserAgent });
	Object.defineProperty(navigator, "maxTouchPoints", { configurable: true, value: originalMaxTouchPoints });
	Object.defineProperty(window, "ao", { configurable: true, value: originalBridge });
});

const MAC_DESKTOP =
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";
const IPHONE =
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1";
const IPAD =
	"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1";
// iPadOS 13+ Safari reports the desktop Macintosh UA; only maxTouchPoints betrays it.
const IPADOS_DESKTOP_UA =
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15";
const LINUX = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";
const WINDOWS =
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";

describe("isMacDesktopChrome", () => {
	it("is true for macOS inside Electron", () => {
		setEnvironment(MAC_DESKTOP, 0, true);
		expect(isMacDesktopChrome()).toBe(true);
	});

	it("is false for a macOS desktop browser without the Electron bridge", () => {
		setEnvironment(MAC_DESKTOP, 0);
		expect(isMacDesktopChrome()).toBe(false);
	});

	// The next three force the Electron bridge on so the assertion exercises the
	// Apple-mobile / iPad-touch exclusions themselves, not just the bridge gate —
	// otherwise they would pass on `!hasElectronBridge()` alone and the exclusion
	// logic would be untested (GH #54).
	it("is false on iPhone even with a bridge (GH #54: iOS must not get macOS-Electron chrome)", () => {
		setEnvironment(IPHONE, 5, true);
		expect(isMacDesktopChrome()).toBe(false);
	});

	it("is false on iPad even with a bridge", () => {
		setEnvironment(IPAD, 5, true);
		expect(isMacDesktopChrome()).toBe(false);
	});

	it("is false on iPadOS masquerading as desktop Safari (touch device) even with a bridge", () => {
		setEnvironment(IPADOS_DESKTOP_UA, 5, true);
		expect(isMacDesktopChrome()).toBe(false);
	});

	it("is false on Linux", () => {
		setEnvironment(LINUX, 0);
		expect(isMacDesktopChrome()).toBe(false);
	});

	it("is false on Windows", () => {
		setEnvironment(WINDOWS, 0);
		expect(isMacDesktopChrome()).toBe(false);
	});
});
