import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The titlebar cluster's history arrows depend on the router; stub the two
// hooks TitlebarNav consumes so the component renders without a real router.
const routerHistory = vi.hoisted(() => ({
	location: { state: { __TSR_index: 0 } },
	listener: undefined as
		((update: { location: { state: { __TSR_index: number } }; action: { type: string } }) => void) | undefined,
	back: vi.fn(),
	forward: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
	useRouter: () => ({
		history: {
			get location() {
				return routerHistory.location;
			},
			subscribe: (
				listener: (update: { location: { state: { __TSR_index: number } }; action: { type: string } }) => void,
			) => {
				routerHistory.listener = listener;
				return () => {
					routerHistory.listener = undefined;
				};
			},
			back: routerHistory.back,
			forward: routerHistory.forward,
		},
	}),
	useCanGoBack: () => false,
}));

// Force the Sheet-backed mobile sidebar path regardless of jsdom viewport.
vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: () => true }));

// Regression for GH #54: iOS must not inherit the macOS desktop titlebar
// cluster (traffic-light positioning, history buttons, drag semantics). The
// replacement mobile opener lives in ShellTopbar and has its own e2e coverage.
describe("TitlebarNav mobile chrome", () => {
	const originalUserAgent = navigator.userAgent;
	const originalMaxTouchPoints = navigator.maxTouchPoints;
	const originalBridge = window.ao;

	beforeEach(() => {
		// iOS UA reproduces the bug: the legacy /Mac|iPhone|iPad/ regex
		// incorrectly classified this as macOS desktop chrome.
		Object.defineProperty(navigator, "userAgent", {
			configurable: true,
			value:
				"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
		});
		Object.defineProperty(navigator, "maxTouchPoints", { configurable: true, value: 5 });
		Object.defineProperty(window, "ao", { configurable: true, value: undefined });
		routerHistory.location = { state: { __TSR_index: 0 } };
		routerHistory.listener = undefined;
		routerHistory.back.mockReset();
		routerHistory.forward.mockReset();
		vi.resetModules();
	});

	afterEach(() => {
		Object.defineProperty(navigator, "userAgent", { configurable: true, value: originalUserAgent });
		Object.defineProperty(navigator, "maxTouchPoints", { configurable: true, value: originalMaxTouchPoints });
		Object.defineProperty(window, "ao", { configurable: true, value: originalBridge });
		vi.clearAllMocks();
	});

	it("does not render the macOS desktop titlebar cluster on iOS", async () => {
		const { TitlebarNav } = await import("./TitlebarNav");
		const { SidebarProvider } = await import("./ui/sidebar");

		render(
			<SidebarProvider>
				<TitlebarNav />
			</SidebarProvider>,
		);

		expect(screen.queryByRole("button", { name: /expand sidebar|collapse sidebar/i })).not.toBeInTheDocument();
	});

	it("keeps the titlebar cluster for macOS Electron", async () => {
		Object.defineProperty(navigator, "userAgent", {
			configurable: true,
			value:
				"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) ao/0.10.3 Chrome/120.0.0.0 Electron/33.0.0 Safari/537.36",
		});
		Object.defineProperty(navigator, "maxTouchPoints", { configurable: true, value: 0 });
		Object.defineProperty(window, "ao", { configurable: true, value: {} as Window["ao"] });
		vi.resetModules();

		const { TitlebarNav } = await import("./TitlebarNav");
		const { SidebarProvider } = await import("./ui/sidebar");

		render(
			<SidebarProvider>
				<TitlebarNav />
			</SidebarProvider>,
		);

		expect(screen.getByRole("button", { name: "Expand sidebar" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Go back" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Go forward" })).toBeInTheDocument();
	});

	it("enables forward after navigating back within Electron router history", async () => {
		Object.defineProperty(navigator, "userAgent", {
			configurable: true,
			value:
				"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 ao/0.10.3 Chrome/120.0.0.0 Electron/33.0.0 Safari/537.36",
		});
		Object.defineProperty(navigator, "maxTouchPoints", { configurable: true, value: 0 });
		Object.defineProperty(window, "ao", { configurable: true, value: {} as Window["ao"] });
		vi.resetModules();

		const { TitlebarNav } = await import("./TitlebarNav");
		const { SidebarProvider } = await import("./ui/sidebar");

		render(
			<SidebarProvider>
				<TitlebarNav />
			</SidebarProvider>,
		);

		const forward = screen.getByRole("button", { name: "Go forward" });
		expect(forward).toBeDisabled();

		// Home (index 0) → session (PUSH index 1): index 1 becomes the stack tip.
		act(() => {
			routerHistory.listener?.({
				location: { state: { __TSR_index: 1 } },
				action: { type: "PUSH" },
			});
		});
		expect(forward).toBeDisabled();

		// Back to index 0 keeps tip=1, so forward becomes available.
		act(() => {
			routerHistory.listener?.({
				location: { state: { __TSR_index: 0 } },
				action: { type: "BACK" },
			});
		});
		expect(forward).toBeEnabled();

		forward.click();
		expect(routerHistory.forward).toHaveBeenCalledOnce();

		// A new PUSH discards the old forward stack and becomes the new tip.
		act(() => {
			routerHistory.listener?.({
				location: { state: { __TSR_index: 1 } },
				action: { type: "PUSH" },
			});
		});
		expect(forward).toBeDisabled();
	});
});
