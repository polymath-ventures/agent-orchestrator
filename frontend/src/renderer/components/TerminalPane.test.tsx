import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceSession } from "../types/workspace";
import { TerminalPane, providerScrollsByKeyboard } from "./TerminalPane";

const { postMock, relaunchPrimeMock, navigateMock, terminalError, terminalState, terminalProps, replaySettled } =
	vi.hoisted(() => ({
		postMock: vi.fn(),
		relaunchPrimeMock: vi.fn(),
		navigateMock: vi.fn(),
		terminalError: { value: undefined as string | undefined },
		terminalState: { value: "idle" },
		terminalProps: { value: {} as { autoFocus?: boolean; focusRequest?: number; onExitFocus?: () => void } },
		replaySettled: { value: true },
	}));
let terminalLinkHandler: ((uri: string) => void) | undefined;

vi.mock("../lib/api-client", () => ({
	apiClient: { POST: (...args: unknown[]) => postMock(...args) },
	apiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

vi.mock("./XtermTerminal", () => ({
	XtermTerminal: (props: { onLinkOpen?: (uri: string) => void; autoFocus?: boolean; focusRequest?: number }) => {
		terminalLinkHandler = props.onLinkOpen;
		terminalProps.value = props;
		return <div data-testid="xterm" />;
	},
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
	...(await importOriginal<typeof import("@tanstack/react-router")>()),
	useNavigate: () => navigateMock,
}));

vi.mock("../lib/relaunch-prime", () => ({
	relaunchPrime: (...args: unknown[]) => relaunchPrimeMock(...args),
}));

vi.mock("../hooks/useTerminalSession", () => ({
	useTerminalSession: () => ({
		attach: vi.fn(),
		state: terminalState.value,
		error: terminalError.value,
		replaySettled: replaySettled.value,
	}),
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

const orchestrator = {
	...worker,
	id: "sess-orch",
	title: "orchestrate",
	kind: "orchestrator",
} satisfies WorkspaceSession;

beforeEach(() => {
	postMock.mockReset();
	relaunchPrimeMock.mockReset();
	relaunchPrimeMock.mockResolvedValue("ao-prime-2");
	navigateMock.mockReset();
	postMock.mockResolvedValue({ data: {} });
	terminalError.value = undefined;
	terminalState.value = "idle";
	replaySettled.value = true;
	terminalLinkHandler = undefined;
});

function renderPane(session?: WorkspaceSession) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const previousAO = window.ao;
	window.ao = {} as typeof window.ao;
	const result = render(
		<QueryClientProvider client={queryClient}>
			<TerminalPane daemonReady fontSize={12} session={session} theme="dark" />
		</QueryClientProvider>,
	);
	return {
		...result,
		queryClient,
		restore: () => {
			window.ao = previousAO;
		},
	};
}

describe("TerminalPane autoFocus", () => {
	function renderAutoFocusPane(session?: WorkspaceSession) {
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		const previousAO = window.ao;
		window.ao = {} as typeof window.ao;
		const result = render(
			<QueryClientProvider client={queryClient}>
				<TerminalPane autoFocus daemonReady fontSize={12} session={session} theme="dark" />
			</QueryClientProvider>,
		);
		return { ...result, restore: () => (window.ao = previousAO) };
	}

	// A starting session has no handle, so the pane is covered by the opaque
	// "Starting session" overlay with no PTY behind it. Focusing there would
	// swallow the user's keystrokes into nothing — and the handle arrives from a
	// background poll, so the remount is not a user switching to the terminal.
	it("does not focus a pane whose session has no terminal handle yet", () => {
		const view = renderAutoFocusPane(worker);
		try {
			expect(terminalProps.value.autoFocus).toBe(false);
		} finally {
			view.restore();
		}
	});

	it("focuses once the pane has a terminal handle to attach", () => {
		const view = renderAutoFocusPane({ ...worker, terminalHandleId: "term-1" });
		try {
			expect(terminalProps.value.autoFocus).toBe(true);
		} finally {
			view.restore();
		}
	});

	it("does not reuse an old focus request for a later terminal remount", () => {
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		const { rerender, unmount } = render(
			<QueryClientProvider client={queryClient}>
				<TerminalPane
					autoFocus
					daemonReady
					focusRequest={1}
					fontSize={12}
					terminalTarget={{ kind: "shell", handleId: "shell-1", title: "one" }}
					theme="dark"
				/>
			</QueryClientProvider>,
		);
		try {
			expect(terminalProps.value.autoFocus).toBe(true);

			rerender(
				<QueryClientProvider client={queryClient}>
					<TerminalPane
						autoFocus
						daemonReady
						focusRequest={1}
						fontSize={12}
						terminalTarget={{ kind: "shell", handleId: "shell-2", title: "two" }}
						theme="dark"
					/>
				</QueryClientProvider>,
			);

			expect(terminalProps.value.autoFocus).toBe(false);
		} finally {
			unmount();
		}
	});

	it("does not autofocus when a background terminal handle arrives without a fresh activation", () => {
		const outside = document.body.appendChild(document.createElement("button"));
		outside.focus();
		const view = renderPane(worker);
		try {
			view.rerender(
				<QueryClientProvider client={view.queryClient}>
					<TerminalPane daemonReady fontSize={12} session={{ ...worker, terminalHandleId: "term-1" }} theme="dark" />
				</QueryClientProvider>,
			);

			expect(terminalProps.value.autoFocus).toBe(false);
			expect(terminalProps.value.focusRequest).toBeUndefined();
		} finally {
			outside.remove();
			view.restore();
		}
	});

	it("does not treat a reviewer target as attachable when no session is loaded", () => {
		const view = renderPane();
		try {
			view.rerender(
				<QueryClientProvider client={view.queryClient}>
					<TerminalPane
						autoFocus
						daemonReady
						fontSize={12}
						terminalTarget={{ kind: "reviewer", handleId: "reviewer-1", harness: "codex" }}
						theme="dark"
					/>
				</QueryClientProvider>,
			);

			expect(terminalProps.value.autoFocus).toBe(false);
			expect(screen.getByText("Agent Orchestrator")).toBeInTheDocument();
			expect(screen.getByText("No session selected. Pick a worker to attach its terminal.")).toBeInTheDocument();
		} finally {
			view.restore();
		}
	});

	it("preserves a user focus request until a pending session receives its terminal handle", () => {
		const view = renderPane(worker);
		try {
			view.rerender(
				<QueryClientProvider client={view.queryClient}>
					<TerminalPane autoFocus daemonReady focusRequest={1} fontSize={12} session={worker} theme="dark" />
				</QueryClientProvider>,
			);

			expect(terminalProps.value.autoFocus).toBe(false);
			expect(terminalProps.value.focusRequest).toBeUndefined();

			view.rerender(
				<QueryClientProvider client={view.queryClient}>
					<TerminalPane
						autoFocus
						daemonReady
						focusRequest={1}
						fontSize={12}
						session={{ ...worker, terminalHandleId: "term-1" }}
						theme="dark"
					/>
				</QueryClientProvider>,
			);

			expect(terminalProps.value.autoFocus).toBe(true);
			expect(terminalProps.value.focusRequest).toBe(1);
		} finally {
			view.restore();
		}
	});
});

describe("TerminalPane empty states", () => {
	it("shows a no-selection message when no session is selected", () => {
		const view = renderPane();
		try {
			expect(screen.getByText("Agent Orchestrator")).toBeInTheDocument();
			expect(screen.getByText("No session selected. Pick a worker to attach its terminal.")).toBeInTheDocument();
		} finally {
			view.restore();
		}
	});

	it("shows a startup message when a selected session has no terminal handle yet", () => {
		const view = renderPane(worker);
		try {
			expect(screen.getByText("Starting session")).toBeInTheDocument();
			expect(
				screen.getByText(
					"Preparing the worker terminal. This can take a moment while AO creates the workspace and starts the agent.",
				),
			).toBeInTheDocument();
			expect(screen.queryByText("No session selected. Pick a worker to attach its terminal.")).not.toBeInTheDocument();
		} finally {
			view.restore();
		}
	});

	it("shows orchestrator-specific startup copy for a pending orchestrator terminal", () => {
		const view = renderPane(orchestrator);
		try {
			expect(screen.getByText("Starting session")).toBeInTheDocument();
			expect(
				screen.getByText(
					"Preparing the orchestrator terminal. This can take a moment while AO creates the workspace and starts the agent.",
				),
			).toBeInTheDocument();
			expect(screen.queryByText(/worker terminal/i)).not.toBeInTheDocument();
		} finally {
			view.restore();
		}
	});
});

// Initial-replay cover (issue #3160): xterm stays mounted and ingesting behind
// a blank cover so the pane is revealed already drawn at the tail.
describe("TerminalPane replay cover", () => {
	beforeEach(() => {
		// The cover is scoped to an attachment that is actually expecting a
		// replay, so these cases have to be in a connecting/attached state.
		terminalState.value = "connecting";
	});

	it("covers the terminal while the attachment is still buffering the replay", () => {
		replaySettled.value = false;
		const view = renderPane({ ...worker, terminalHandleId: "term-1" });
		try {
			expect(screen.getByTestId("terminal-replay-cover")).toBeInTheDocument();
			// xterm keeps rendering underneath — covered, never unmounted, so the
			// grid it measures stays correct.
			expect(screen.getByTestId("xterm")).toBeInTheDocument();
		} finally {
			view.restore();
		}
	});

	it("uncovers once the replay has settled", () => {
		replaySettled.value = true;
		const view = renderPane({ ...worker, terminalHandleId: "term-1" });
		try {
			expect(screen.queryByTestId("terminal-replay-cover")).not.toBeInTheDocument();
		} finally {
			view.restore();
		}
	});

	it("shows no loader text on a fast open", () => {
		replaySettled.value = false;
		const view = renderPane({ ...worker, terminalHandleId: "term-1" });
		try {
			// The label is delayed, so a session switch that resolves quickly never
			// flashes a spinner — the whole point of a blank cover.
			expect(screen.queryByText("Loading latest output…")).not.toBeInTheDocument();
		} finally {
			view.restore();
		}
	});

	it("stays out of the way while the pane is visibly reattaching", () => {
		replaySettled.value = false;
		terminalState.value = "reattaching";
		const view = renderPane({ ...worker, terminalHandleId: "term-1" });
		try {
			// An open timeout lifts the cover and the backoff reconnect would pull
			// it straight back down; the banner explains this window better.
			expect(screen.queryByTestId("terminal-replay-cover")).not.toBeInTheDocument();
			expect(screen.getByText("Terminal disconnected — reattaching…")).toBeInTheDocument();
		} finally {
			view.restore();
		}
	});

	it("keeps the startup card, not the blank cover, when there is no terminal handle yet", () => {
		replaySettled.value = false;
		const view = renderPane(worker);
		try {
			expect(screen.getByText("Starting session")).toBeInTheDocument();
			expect(screen.queryByTestId("terminal-replay-cover")).not.toBeInTheDocument();
		} finally {
			view.restore();
		}
	});
});

describe("terminal restore", () => {
	it.each([
		["exited", undefined],
		["error", "terminal handle missing"],
		["idle", undefined],
	])("posts restore from the terminal-ended strip when mux state is %s", async (state, error) => {
		terminalState.value = state;
		terminalError.value = error;
		const view = renderPane({ ...worker, status: "terminated", terminalHandleId: "term-1" });
		const invalidate = vi.spyOn(view.queryClient, "invalidateQueries").mockResolvedValue(undefined);
		try {
			await userEvent.click(screen.getByRole("button", { name: "Restore session" }));

			await waitFor(() =>
				expect(postMock).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/restore", {
					params: { path: { sessionId: "sess-1" } },
				}),
			);
			expect(invalidate).toHaveBeenCalledWith({ queryKey: ["workspaces"] });
		} finally {
			view.restore();
		}
	});

	it("offers restore when a merged session is terminated", () => {
		const view = renderPane({
			...worker,
			status: "merged",
			isTerminated: true,
			terminalHandleId: "term-1",
		});
		try {
			expect(screen.getByRole("button", { name: "Restore session" })).toBeInTheDocument();
		} finally {
			view.restore();
		}
	});
});

describe("providerScrollsByKeyboard", () => {
	// opencode and its fork kilocode share a TUI that scrolls its own transcript
	// by keyboard and ignores SGR wheel reports, so both must opt into the
	// PageUp/PageDown wheel routing (see XtermTerminal's paneScrollsByKeyboard).
	it("is true for keyboard-scroll TUIs (opencode and its kilocode fork)", () => {
		expect(providerScrollsByKeyboard("opencode")).toBe(true);
		expect(providerScrollsByKeyboard("kilocode")).toBe(true);
	});

	it("is false for mouse-report/native-scroll providers", () => {
		expect(providerScrollsByKeyboard("codex")).toBe(false);
		expect(providerScrollsByKeyboard("claude-code")).toBe(false);
	});

	it("is false when the provider is unknown", () => {
		expect(providerScrollsByKeyboard(undefined)).toBe(false);
	});
});

describe("terminal link preview", () => {
	it.each(["http://localhost:3000/simple", "https://app.localhost:5173", "http://127.0.0.1:8080", "http://[::1]:4173"])(
		"mirrors worker loopback link %s into the session Browser preview",
		async (url) => {
			const view = renderPane(worker);
			try {
				expect(terminalLinkHandler).toBeTypeOf("function");
				act(() => terminalLinkHandler?.(url));

				await waitFor(() =>
					expect(postMock).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/preview", {
						params: { path: { sessionId: "sess-1" } },
						body: { url },
					}),
				);
			} finally {
				view.restore();
			}
		},
	);

	it("mirrors an external (non-loopback) terminal link into the Browser preview", async () => {
		const view = renderPane(worker);
		try {
			act(() => terminalLinkHandler?.("https://example.com/pull/42"));
			await waitFor(() =>
				expect(postMock).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/preview", {
					params: { path: { sessionId: "sess-1" } },
					body: { url: "https://example.com/pull/42" },
				}),
			);
		} finally {
			view.restore();
		}
	});

	it("does not mirror a non-web (mailto:) link", () => {
		const view = renderPane(worker);
		try {
			act(() => terminalLinkHandler?.("mailto:dev@example.com"));
			expect(postMock).not.toHaveBeenCalled();
		} finally {
			view.restore();
		}
	});

	it("does not POST without a selected session", () => {
		const view = renderPane();
		try {
			act(() => terminalLinkHandler?.("http://localhost:3000"));
			expect(postMock).not.toHaveBeenCalled();
		} finally {
			view.restore();
		}
	});

	it("does not mirror orchestrator links because orchestrators have no Browser inspector", () => {
		const view = renderPane(orchestrator);
		try {
			act(() => terminalLinkHandler?.("http://localhost:3000"));
			expect(postMock).not.toHaveBeenCalled();
		} finally {
			view.restore();
		}
	});

	it("does not mirror links for terminated workers because their Browser inspector is cleared", () => {
		const view = renderPane({ ...worker, status: "terminated" });
		try {
			act(() => terminalLinkHandler?.("http://localhost:3000"));
			expect(postMock).not.toHaveBeenCalled();
		} finally {
			view.restore();
		}
	});

	it("does not mirror links for merged workers whose session is terminated", () => {
		const view = renderPane({ ...worker, status: "merged", isTerminated: true });
		try {
			act(() => terminalLinkHandler?.("http://localhost:3000"));
			expect(postMock).not.toHaveBeenCalled();
		} finally {
			view.restore();
		}
	});

	it("does not invalidate workspace data when the preview endpoint returns an error", async () => {
		postMock.mockResolvedValueOnce({ error: { code: "INVALID_PREVIEW_URL" } });
		const warning = vi.spyOn(console, "warn").mockImplementation(() => undefined);
		const view = renderPane(worker);
		const invalidate = vi.spyOn(view.queryClient, "invalidateQueries");
		try {
			act(() => terminalLinkHandler?.("http://localhost:3000"));
			await waitFor(() => expect(warning).toHaveBeenCalled());
			expect(invalidate).not.toHaveBeenCalled();
		} finally {
			warning.mockRestore();
			view.restore();
		}
	});

	it("handles a rejected preview request without an unhandled rejection", async () => {
		const error = new Error("daemon unavailable");
		postMock.mockRejectedValueOnce(error);
		const warning = vi.spyOn(console, "warn").mockImplementation(() => undefined);
		const view = renderPane(worker);
		const invalidate = vi.spyOn(view.queryClient, "invalidateQueries");
		try {
			act(() => terminalLinkHandler?.("http://localhost:3000"));
			await waitFor(() =>
				expect(warning).toHaveBeenCalledWith("Unable to open terminal link in Browser preview", error),
			);
			expect(invalidate).not.toHaveBeenCalled();
		} finally {
			warning.mockRestore();
			view.restore();
		}
	});
});

const deadPrime = {
	...worker,
	id: "ao-prime",
	title: "Prime",
	kind: "prime",
	status: "terminated",
} satisfies WorkspaceSession;

describe("TerminalPane dead Prime recovery", () => {
	// The daemon forbids generic restore for Prime, so the old restore control
	// could only ever produce a 403. Prime recovers through relaunch.
	it("offers Relaunch Prime and not Restore session for a terminated prime", async () => {
		const { restore } = renderPane(deadPrime);

		expect(await screen.findByRole("button", { name: "Relaunch Prime" })).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Restore session" })).not.toBeInTheDocument();
		restore();
	});

	// Relaunching must leave the dead session behind; otherwise the pane stays
	// bound to the terminated row and keeps showing the recovery strip even
	// though a healthy Prime now exists.
	it("relaunches Prime and navigates to the returned session", async () => {
		const user = userEvent.setup();
		const { restore } = renderPane(deadPrime);

		await user.click(await screen.findByRole("button", { name: "Relaunch Prime" }));

		await waitFor(() => expect(relaunchPrimeMock).toHaveBeenCalledTimes(1));
		await waitFor(() =>
			expect(navigateMock).toHaveBeenCalledWith({
				to: "/sessions/$sessionId",
				params: { sessionId: "ao-prime-2" },
			}),
		);
		restore();
	});

	it("surfaces a relaunch failure", async () => {
		relaunchPrimeMock.mockRejectedValue(new Error("Prime is disabled"));
		const user = userEvent.setup();
		const { restore } = renderPane(deadPrime);

		await user.click(await screen.findByRole("button", { name: "Relaunch Prime" }));

		expect(await screen.findByText("Prime is disabled")).toBeInTheDocument();
		restore();
	});

	// A terminated worker keeps the ordinary restore affordance.
	it("still offers Restore session for a terminated worker", async () => {
		const { restore } = renderPane({ ...worker, status: "terminated" });

		expect(await screen.findByRole("button", { name: "Restore session" })).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Relaunch Prime" })).not.toBeInTheDocument();
		restore();
	});
});
