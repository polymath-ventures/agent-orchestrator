import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { WorkspaceSession } from "../types/workspace";
import { CenterPane } from "./CenterPane";

const terminalPaneProps = vi.hoisted(() => ({
	value: {} as { autoFocus?: boolean; focusRequest?: number; onExitFocus?: () => void },
}));

// The terminal body pulls in xterm/SSE machinery irrelevant to the header under test.
vi.mock("./TerminalPane", () => ({
	TerminalPane: (props: { autoFocus?: boolean; focusRequest?: number; onExitFocus?: () => void }) => {
		terminalPaneProps.value = props;
		return <div>terminal body</div>;
	},
}));

const worker = {
	id: "sess-1",
	workspaceId: "proj-1",
	workspaceName: "my-app",
	title: "do the thing",
	provider: "claude-code",
	kind: "worker",
	branch: "ao/sess-1",
	status: "working",
	updatedAt: "2026-06-10T00:00:00Z",
	prs: [],
} satisfies WorkspaceSession;

describe("CenterPane toolbar session label", () => {
	const makeShells = (count: number) =>
		Array.from({ length: count }, (_, i) => ({
			handleId: `h-${i}`,
			title: `agent-orchestrator-${i}`,
			workingDir: "/tmp/ws",
			createdAt: "2026-07-22T00:00:00Z",
		}));

	it("shows the session display name for a worker", () => {
		render(<CenterPane session={worker} theme="dark" daemonReady />);
		expect(screen.getByText("do the thing")).toBeInTheDocument();
		expect(screen.queryByText("sess-1")).not.toBeInTheDocument();
	});

	it("passes explicit focus requests through without focusing passive session mounts", () => {
		const { rerender } = render(<CenterPane session={worker} theme="dark" daemonReady />);

		expect(terminalPaneProps.value.autoFocus).toBe(false);
		expect(terminalPaneProps.value.focusRequest).toBeUndefined();

		rerender(<CenterPane focusRequest={1} session={worker} theme="dark" daemonReady />);

		expect(terminalPaneProps.value.autoFocus).toBe(true);
		expect(terminalPaneProps.value.focusRequest).toBe(1);
	});

	// The Ctrl+F6 exit-focus escape hatch resolves its target by querying
	// [data-terminal-tab="true"][aria-current="true"] inside the pane. Upstream owns
	// this tab strip's shape and reverted it to a single session tab (#3275), so the
	// attribute pairing is the seam most likely to be dropped by a future sync while
	// every focus-nonce test above keeps passing. See docs/fork.md item 2.
	it("exits terminal focus onto the active session tab, the data-terminal-tab anchor", () => {
		render(<CenterPane session={worker} theme="dark" daemonReady />);

		const sessionTab = screen.getByRole("button", { name: worker.title });
		expect(sessionTab).toHaveAttribute("data-terminal-tab", "true");
		expect(sessionTab).toHaveAttribute("aria-current", "true");
		expect(sessionTab).not.toHaveFocus();

		terminalPaneProps.value.onExitFocus?.();

		expect(sessionTab).toHaveFocus();
	});

	it("renders only this session's own tab, never a sibling session", () => {
		render(<CenterPane session={worker} theme="dark" daemonReady />);

		expect(screen.getByRole("button", { name: "do the thing" })).toHaveAttribute("aria-current", "true");
		expect(screen.queryByRole("button", { name: "review the change" })).not.toBeInTheDocument();
	});

	// The button used to open a dropdown that also listed every session across
	// every project (#3208); it now only ever creates a terminal.
	it("opens a new terminal straight from the tab-strip button", () => {
		const onNewShellTerminal = vi.fn();
		render(<CenterPane session={worker} onNewShellTerminal={onNewShellTerminal} theme="dark" daemonReady />);

		fireEvent.click(screen.getByRole("button", { name: "New terminal" }));
		expect(onNewShellTerminal).toHaveBeenCalledOnce();
		expect(screen.queryByRole("menu")).not.toBeInTheDocument();
	});

	it("shows 'Orchestrator' for an orchestrator session", () => {
		render(<CenterPane session={{ ...worker, id: "sess-orch", kind: "orchestrator" }} theme="dark" daemonReady />);
		expect(screen.getByText("Orchestrator")).toBeInTheDocument();
	});

	it("shows 'No session' when there is no session", () => {
		render(<CenterPane theme="dark" daemonReady />);
		expect(screen.getByText("No session")).toBeInTheDocument();
	});

	it("uses the inspector tab height for the terminal header", () => {
		render(<CenterPane session={worker} theme="dark" daemonReady />);

		const header = screen.getByText("TERMINAL").parentElement?.parentElement;
		expect(header).toHaveClass("h-inspector-tabs");
	});

	it("lets tabs shrink into a scrollable strip instead of overflowing onto the controls", () => {
		const shells = makeShells(8);
		render(<CenterPane session={worker} shellTerminals={shells} theme="dark" daemonReady />);

		const scrollRegion = document.querySelector(".overflow-x-auto");
		expect(scrollRegion).toHaveClass("scrollbar-none", "min-w-flex-min", "flex-1");
		for (const tab of screen.getAllByTitle(/^\/tmp\/ws/)) {
			expect(tab.parentElement).toHaveClass("min-w-shell-tab-min");
			expect(tab.parentElement).not.toHaveClass("min-w-16", "shrink-0");
			expect(tab).toHaveClass("min-w-flex-min");
			expect(tab).not.toHaveClass("min-w-0");
		}
		// jsdom reports no overflow, so the indicator stays mounted but disabled to preserve focus.
		expect(screen.getByRole("button", { name: "Scroll tabs right" })).toBeDisabled();

		// The display controls float over the terminal body, not the tab bar,
		// so tabs and controls can never overlap.
		const tabBarRow = screen.getByText("TERMINAL").closest("div")?.parentElement;
		expect(tabBarRow).not.toBeNull();
		expect(tabBarRow?.contains(screen.getByRole("button", { name: /fullscreen/i }))).toBe(false);
	});

	it("reveals scroll chevrons only when the tab strip actually overflows", () => {
		const shells = makeShells(8);
		render(<CenterPane session={worker} shellTerminals={shells} theme="dark" daemonReady />);

		const scrollRegion = document.querySelector(".overflow-x-auto") as HTMLElement;
		Object.defineProperty(scrollRegion, "clientWidth", { value: 100, configurable: true });
		Object.defineProperty(scrollRegion, "scrollWidth", { value: 500, configurable: true });
		fireEvent.scroll(scrollRegion);

		expect(screen.getByRole("button", { name: "Scroll tabs right" })).toBeEnabled();
		expect(screen.getByRole("button", { name: "Scroll tabs left" })).toBeDisabled();

		Object.defineProperty(scrollRegion, "scrollLeft", { value: 400, configurable: true });
		fireEvent.scroll(scrollRegion);
		expect(screen.getByRole("button", { name: "Scroll tabs left" })).toBeEnabled();
		expect(screen.getByRole("button", { name: "Scroll tabs right" })).toBeDisabled();
	});

	it("scrolls the tab strip horizontally with the mouse wheel", () => {
		const shells = makeShells(8);
		render(<CenterPane session={worker} shellTerminals={shells} theme="dark" daemonReady />);

		const scrollRegion = document.querySelector(".overflow-x-auto") as HTMLElement;
		Object.defineProperty(scrollRegion, "clientWidth", { value: 100, configurable: true });
		Object.defineProperty(scrollRegion, "scrollWidth", { value: 500, configurable: true });
		const scrollBy = vi.fn();
		Object.defineProperty(scrollRegion, "scrollBy", { value: scrollBy, configurable: true });

		fireEvent.wheel(scrollRegion, { deltaY: 80 });
		expect(scrollBy).toHaveBeenCalledWith({ left: 80 });

		// Ctrl+wheel is terminal font zoom, not tab scrolling.
		scrollBy.mockClear();
		fireEvent.wheel(scrollRegion, { deltaY: 80, ctrlKey: true });
		expect(scrollBy).not.toHaveBeenCalled();
	});
});
