import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../stores/ui-store";
import { ShellTerminalsView } from "./ShellTerminalsView";

const shellMocks = vi.hoisted(() => ({
	shellTerminals: [] as { handleId: string; title: string; workingDir: string; createdAt: string }[],
	closeShellTerminal: vi.fn(),
	terminalPaneProps: {} as { autoFocus?: boolean; focusRequest?: number; terminalTarget?: { handleId?: string } },
}));

vi.mock("../hooks/useShellTerminals", () => ({
	useCloseShellTerminal: () => ({ mutate: shellMocks.closeShellTerminal }),
	useRenameShellTerminal: () => ({ mutate: vi.fn() }),
	useShellTerminals: () => ({ data: shellMocks.shellTerminals }),
}));

vi.mock("../lib/shell-context", () => ({
	useShell: () => ({ daemonStatus: { state: "ready" } }),
}));

vi.mock("./TerminalPane", () => ({
	TerminalPane: (props: { autoFocus?: boolean; focusRequest?: number; terminalTarget?: { handleId?: string } }) => {
		shellMocks.terminalPaneProps = props;
		return <div>terminal body</div>;
	},
}));

describe("ShellTerminalsView", () => {
	beforeEach(() => {
		shellMocks.shellTerminals = [];
		shellMocks.closeShellTerminal.mockReset();
		shellMocks.terminalPaneProps = {};
		useUiStore.setState({ activeShellTerminalHandleId: null });
	});

	it("points the empty state at the visible plus tab-strip control", () => {
		render(<ShellTerminalsView />);

		expect(screen.getByText("No terminals open")).toBeInTheDocument();
		expect(screen.getByText(/use the \+ button/i)).toBeInTheDocument();
		expect(screen.queryByText(/terminal button/i)).not.toBeInTheDocument();
	});

	it("focuses on route adoption and explicit tab activation", () => {
		shellMocks.shellTerminals = [
			{ handleId: "shell-1", title: "one", workingDir: "/tmp/one", createdAt: "2026-07-25T00:00:00Z" },
		];
		useUiStore.setState({ activeShellTerminalHandleId: "shell-1" });
		const { rerender } = render(<ShellTerminalsView />);

		expect(shellMocks.terminalPaneProps.terminalTarget?.handleId).toBe("shell-1");
		expect(shellMocks.terminalPaneProps.autoFocus).toBe(true);
		expect(shellMocks.terminalPaneProps.focusRequest).toBe(1);

		fireEvent.click(screen.getByRole("tab", { name: "one" }));
		rerender(<ShellTerminalsView />);

		expect(shellMocks.terminalPaneProps.autoFocus).toBe(true);
		expect(shellMocks.terminalPaneProps.focusRequest).toBe(2);
	});

	it("clears stale selections when the shell list is empty", () => {
		useUiStore.setState({ activeShellTerminalHandleId: "shell-stale" });

		render(<ShellTerminalsView />);

		expect(screen.getByText("No terminals open")).toBeInTheDocument();
		expect(useUiStore.getState().activeShellTerminalHandleId).toBeNull();
	});
});
