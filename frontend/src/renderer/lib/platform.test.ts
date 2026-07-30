import { afterEach, describe, expect, it, vi } from "vitest";
import {
	isLinuxPlatform,
	isMacPlatform,
	isWindowsPlatform,
	usesFramedAppTopbar,
	hidesShellTopbar,
	usesBoardActionsInPanel,
} from "./platform";

const originalPlatform = Object.getOwnPropertyDescriptor(window.navigator, "platform");
const originalUserAgent = Object.getOwnPropertyDescriptor(window.navigator, "userAgent");
const originalUserAgentData = Object.getOwnPropertyDescriptor(window.navigator, "userAgentData");

function spoofPlatform(platform: string, userAgent = platform) {
	Object.defineProperty(window.navigator, "platform", {
		configurable: true,
		get: () => platform,
	});
	Object.defineProperty(window.navigator, "userAgent", {
		configurable: true,
		get: () => userAgent,
	});
	Object.defineProperty(window.navigator, "userAgentData", {
		configurable: true,
		get: () => ({ platform }),
	});
}

function restoreProperty(name: "platform" | "userAgent" | "userAgentData", descriptor: PropertyDescriptor | undefined) {
	if (descriptor) {
		Object.defineProperty(window.navigator, name, descriptor);
		return;
	}
	delete (window.navigator as unknown as Record<string, unknown>)[name];
}

afterEach(() => {
	restoreProperty("platform", originalPlatform);
	restoreProperty("userAgent", originalUserAgent);
	restoreProperty("userAgentData", originalUserAgentData);
	vi.unstubAllEnvs();
});

describe("renderer platform behavior", () => {
	it("hides the shell topbar on macOS and keeps board actions in the panel", () => {
		spoofPlatform("MacIntel", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)");

		expect(isMacPlatform()).toBe(true);
		expect(isWindowsPlatform()).toBe(false);
		expect(usesFramedAppTopbar()).toBe(true);
		expect(hidesShellTopbar()).toBe(true);
		expect(usesBoardActionsInPanel()).toBe(true);
	});

	it("keeps Windows board controls in the inset panel topbar", () => {
		spoofPlatform("Win32");

		expect(isWindowsPlatform()).toBe(true);
		expect(isMacPlatform()).toBe(false);
		expect(usesFramedAppTopbar()).toBe(true);
		expect(hidesShellTopbar()).toBe(false);
		expect(usesBoardActionsInPanel()).toBe(false);
	});

	it("hides the shell topbar on Linux and keeps board actions in the panel", () => {
		spoofPlatform("Linux x86_64");

		expect(isLinuxPlatform()).toBe(true);
		expect(isWindowsPlatform()).toBe(false);
		expect(usesFramedAppTopbar()).toBe(true);
		expect(hidesShellTopbar()).toBe(true);
		expect(usesBoardActionsInPanel()).toBe(true);
	});

	it("keeps browser-mode shell topbar visible on Apple mobile browser UAs", () => {
		vi.stubEnv("VITE_NO_ELECTRON", "1");
		spoofPlatform(
			"iPhone",
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1",
		);

		expect(isMacPlatform()).toBe(true);
		expect(hidesShellTopbar()).toBe(false);
		expect(usesBoardActionsInPanel()).toBe(false);
	});
});
