import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The titlebar cluster's history arrows depend on the router; stub the two
// hooks TitlebarNav consumes so the component renders without a real router.
vi.mock("@tanstack/react-router", () => ({
	useRouter: () => ({
		history: {
			location: { state: { __TSR_index: 0 } },
			subscribe: () => () => undefined,
			back: () => undefined,
			forward: () => undefined,
		},
	}),
	useCanGoBack: () => false,
}));

// Force the Sheet-backed mobile sidebar path regardless of jsdom viewport.
vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: () => true }));

// Regression for GH #46: on a mobile (<768px) viewport the sidebar renders as a
// Sheet controlled by the SidebarProvider context's `openMobile`. The titlebar
// toggle is the only visible affordance to open it, so tapping it MUST drive the
// context toggle (which flips `openMobile`) — not the desktop-only ui-store bool,
// which leaves the Sheet shut and the app feeling inert on a phone.
describe("TitlebarNav mobile sidebar toggle", () => {
	const originalUserAgent = navigator.userAgent;

	beforeEach(() => {
		// iOS UA so the titlebar cluster (today the only mobile toggle host)
		// renders and exercises the reported browser-on-phone surface.
		Object.defineProperty(navigator, "userAgent", {
			configurable: true,
			value:
				"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
		});
		vi.resetModules();
	});

	afterEach(() => {
		Object.defineProperty(navigator, "userAgent", { configurable: true, value: originalUserAgent });
		vi.clearAllMocks();
	});

	it("opens the mobile sidebar Sheet when the titlebar toggle is tapped", async () => {
		const { TitlebarNav } = await import("./TitlebarNav");
		const { Sidebar, SidebarProvider } = await import("./ui/sidebar");

		render(
			<SidebarProvider>
				<Sidebar>
					<div>sidebar-body</div>
				</Sidebar>
				<TitlebarNav />
			</SidebarProvider>,
		);

		// Sheet starts closed: its content is not mounted.
		expect(screen.queryByText("sidebar-body")).not.toBeInTheDocument();

		fireEvent.click(screen.getByRole("button", { name: /expand sidebar|collapse sidebar/i }));

		await waitFor(() => expect(screen.getByText("sidebar-body")).toBeInTheDocument());
		// The label now reflects the open Sheet. The open Sheet marks the
		// background subtree aria-hidden, so include hidden elements in the query.
		expect(screen.getByRole("button", { name: "Collapse sidebar", hidden: true })).toBeInTheDocument();
	});
});
