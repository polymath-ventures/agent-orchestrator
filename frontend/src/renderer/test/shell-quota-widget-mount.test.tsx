/**
 * Mount guard for the agent-harness quota/usage meter (fork feature 3).
 *
 * This suite renders the real application entry point — the `/_shell` route
 * layout — with the REAL `Sidebar` and the REAL sidebar primitives, and asserts
 * the quota meter is actually on screen with live daemon data.
 *
 * `QuotaPanel.test.tsx` renders the component directly, so it kept passing for
 * the whole period the widget was unmounted from the application: the
 * 2026-08-07 upstream sync dropped the `<QuotaPanel />` mount from `Sidebar.tsx`
 * and nothing failed (agent-orchestrator#280).
 *
 * That is the regression this file exists to catch. Do not mock
 * `../components/Sidebar` or `../components/ui/sidebar` here, and do not narrow
 * these tests to render `Sidebar` (or `QuotaPanel`) directly — the point is that
 * the assertion travels the same mount path the shipped app does, so removing
 * the meter from EITHER the shell or the sidebar turns CI red.
 */
import { act, render, screen, waitFor } from "@testing-library/react";
import { Suspense, type ComponentType } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { KeybindingOverrides } from "../../shared/shortcuts";
import { useUiStore } from "../stores/ui-store";
import type { WorkspaceSummary } from "../types/workspace";

const shellMocks = vi.hoisted(() => {
	const state = {
		newSessionListener: undefined as (() => void) | undefined,
		keyboardShortcutsListener: undefined as (() => void) | undefined,
		newShellTerminalListener: undefined as (() => void) | undefined,
		openSettingsListener: undefined as (() => void) | undefined,
		previousSessionListener: undefined as (() => void) | undefined,
		nextSessionListener: undefined as (() => void) | undefined,
		focusTerminalListener: undefined as (() => void) | undefined,
		routeParams: {} as { projectId?: string; sessionId?: string },
		routeSearch: {} as Record<string, unknown>,
		workspaces: [] as WorkspaceSummary[],
		workspaceQuery: {
			data: [] as WorkspaceSummary[],
			dataUpdatedAt: 0,
			isError: false,
			isSuccess: true,
		},
		daemonStatus: { state: "stopped" } as {
			state: "ready" | "starting" | "stopped" | "error";
			port?: number;
			code?: "not_ready";
		},
	};
	return {
		navigate: vi.fn(),
		onNewSessionShortcut: vi.fn((listener: () => void) => {
			state.newSessionListener = listener;
			return vi.fn();
		}),
		onKeyboardShortcutsHelp: vi.fn((listener: () => void) => {
			state.keyboardShortcutsListener = listener;
			return vi.fn();
		}),
		onNewShellTerminalShortcut: vi.fn((listener: () => void) => {
			state.newShellTerminalListener = listener;
			return vi.fn();
		}),
		openShellTerminal: vi.fn(),
		onOpenSettingsShortcut: vi.fn((listener: () => void) => {
			state.openSettingsListener = listener;
			return vi.fn();
		}),
		onPreviousSessionShortcut: vi.fn((listener: () => void) => {
			state.previousSessionListener = listener;
			return vi.fn();
		}),
		onNextSessionShortcut: vi.fn((listener: () => void) => {
			state.nextSessionListener = listener;
			return vi.fn();
		}),
		onFocusTerminalShortcut: vi.fn((listener: () => void) => {
			state.focusTerminalListener = listener;
			return vi.fn();
		}),
		getKeybindings: vi.fn(async () => ({})),
		setKeybindings: vi.fn(async (overrides: KeybindingOverrides) => overrides),
		setKeybindingRecording: vi.fn(async () => undefined),
		state,
	};
});

const quotaMocks = vi.hoisted(() => ({
	getMock: vi.fn(),
	postMock: vi.fn(),
	state: { trusted: true, probeStatuses: [] as unknown[] },
}));

// react-query is intentionally REAL here. The quota meter's own metrics query
// and probe mutation run through the client, so stubbing useQueryClient would
// leave the widget permanently data-less and make this guard vacuous.

vi.mock("@tanstack/react-router", async (importOriginal) => ({
	...(await importOriginal<typeof import("@tanstack/react-router")>()),
	createFileRoute: () => (options: unknown) => ({ options }),
	Outlet: () => null,
	useMatchRoute: () => () => false,
	useNavigate: () => shellMocks.navigate,
	useParams: () => shellMocks.state.routeParams,
	useSearch: () => shellMocks.state.routeSearch,
	useRouterState: ({ select }: { select: (state: { location: { pathname: string } }) => unknown }) =>
		select({ location: { pathname: "/" } }),
}));

vi.mock("../lib/bridge", () => ({
	aoBridge: {
		app: {
			onNewSessionShortcut: shellMocks.onNewSessionShortcut,
			onKeyboardShortcutsHelp: shellMocks.onKeyboardShortcutsHelp,
			onNewShellTerminalShortcut: shellMocks.onNewShellTerminalShortcut,
			onOpenSettingsShortcut: shellMocks.onOpenSettingsShortcut,
			onPreviousSessionShortcut: shellMocks.onPreviousSessionShortcut,
			onNextSessionShortcut: shellMocks.onNextSessionShortcut,
			onFocusTerminalShortcut: shellMocks.onFocusTerminalShortcut,
		},
		keybindings: {
			get: shellMocks.getKeybindings,
			set: shellMocks.setKeybindings,
			setRecording: shellMocks.setKeybindingRecording,
		},
		updates: {
			getStatus: vi.fn(async () => ({ state: "idle" })),
			onStatus: vi.fn(() => vi.fn()),
		},
		// The sidebar footer carries the cloud-account row, which subscribes on mount.
		cloud: {
			getSession: vi.fn(async () => null),
			onSessionChanged: vi.fn(() => vi.fn()),
			signIn: vi.fn(async () => undefined),
			signOut: vi.fn(async () => undefined),
		},
		window: {},
		tray: {
			setAttentionState: () => undefined,
			onOpenSession: () => () => undefined,
		},
	},
}));

vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: () => shellMocks.state.workspaceQuery,
	workspaceQueryKey: ["workspaces"],
	// The shell force-fetches this through the real client once the daemon reports
	// ready, so it needs a real queryFn to settle its startup state.
	workspaceQueryOptions: { queryKey: ["workspaces"], queryFn: async () => [] },
}));

vi.mock("../hooks/usePrimeSettingsQuery", () => ({
	usePrimeEnabledQuery: () => ({ data: false }),
}));

vi.mock("../hooks/useDaemonStatus", () => ({
	useDaemonStatus: () => shellMocks.state.daemonStatus,
}));

// The shell layout opens standalone terminals; this suite only covers the
// shortcut subscriptions, so the mutation is stubbed rather than driven.
vi.mock("../hooks/useShellTerminals", () => ({
	useShellTerminals: () => ({ data: [], isSuccess: true }),
	useOpenShellTerminal: () => ({ mutate: shellMocks.openShellTerminal }),
}));

const { agentInventory } = vi.hoisted(() => ({
	agentInventory: {
		supported: [{ id: "codex", label: "Codex" }],
		installed: [{ id: "codex", label: "Codex" }],
		authorized: [{ id: "codex", label: "Codex", authStatus: "authorized" }],
	},
}));

vi.mock("../hooks/useAgentsQuery", () => ({
	agentsQueryKey: ["agents"],
	// The shell refreshes the catalog through the real client on mount, so these
	// need a real key and a resolving fetcher or react-query rejects the query.
	agentsQueryOptions: { queryKey: ["agents"], queryFn: async () => agentInventory },
	refreshAgents: vi.fn(async () => agentInventory),
	// The inventory supplies the meter's per-harness display labels.
	useAgentsQuery: () => ({ data: agentInventory }),
}));

vi.mock("../components/NotificationCenter", () => ({ NotificationRuntime: () => null }));
vi.mock("../components/CommandPalette", () => ({ CommandPalette: () => null }));
vi.mock("../components/OrchestratorReplacementDialog", () => ({ OrchestratorReplacementDialog: () => null }));
vi.mock("../components/ShellTopbar", () => ({ ShellTopbar: () => null }));
vi.mock("../components/TitlebarNav", async () => {
	const { useUiStore: useStore } = await vi.importActual<typeof import("../stores/ui-store")>("../stores/ui-store");
	return {
		TitlebarNav: ({ onSidebarPreviewEnter }: { onSidebarPreviewEnter?: () => void }) => {
			const isSidebarOpen = useStore((state) => state.isSidebarOpen);
			const toggleSidebar = useStore((state) => state.toggleSidebar);
			return (
				<button
					aria-label={isSidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
					onClick={toggleSidebar}
					onPointerEnter={onSidebarPreviewEnter}
					type="button"
				/>
			);
		},
	};
});
vi.mock("../components/WindowTitlebar", () => ({ WindowTitlebar: () => null }));
vi.mock("../components/SettingsDialog", () => ({ SettingsDialog: () => null }));
vi.mock("../components/KeyboardShortcutsDialog", () => ({
	KeyboardShortcutsDialog: ({ open }: { open: boolean }) => (open ? <div data-testid="keyboard-shortcuts" /> : null),
}));
// shell-context is intentionally REAL: the sidebar footer reads daemon status
// through useShellMaybe(), so stubbing the provider would hide the real wiring.
vi.mock("../components/GlobalNewTaskDialog", async () => {
	const { useUiStore: useStore } = await vi.importActual<typeof import("../stores/ui-store")>("../stores/ui-store");
	return {
		GlobalNewTaskDialog: () => {
			const request = useStore((state) => state.newTaskRequest);
			return request ? <div data-testid="new-task-flow" data-project={request.projectId} /> : null;
		},
	};
});

// Extra module stubs the REAL Sidebar needs, on top of the shared shell preamble.
vi.mock("../lib/rename-session", () => ({ renameSession: vi.fn().mockResolvedValue(undefined) }));
vi.mock("../lib/spawn-orchestrator", () => ({ spawnOrchestrator: vi.fn() }));
vi.mock("../hooks/useCommandPaletteEnabled", () => ({ useCommandPaletteEnabled: () => false }));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: quotaMocks.getMock, POST: quotaMocks.postMock },
	apiErrorMessage: (error: unknown) => {
		if (error instanceof Error) return error.message;
		if (typeof error === "object" && error !== null && "message" in error && typeof error.message === "string") {
			return error.message;
		}
		return "Request failed";
	},
	// The meter's metrics query is gated on a trusted API base URL.
	hasTrustedApiBaseUrl: () => quotaMocks.state.trusted,
}));

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Route } from "../routes/_shell";

const ShellRoute = Route.options.component as ComponentType;

async function renderShell() {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	let view: ReturnType<typeof render> | undefined;
	await act(async () => {
		view = render(
			<QueryClientProvider client={queryClient}>
				<Suspense fallback={null}>
					<ShellRoute />
				</Suspense>
			</QueryClientProvider>,
		);
	});
	// The router's code-splitting plugin hands back a lazy route component, so the
	// first render in a worker suspends until that chunk is transformed. Wait for
	// the real sidebar footer before asserting anything about what is inside it.
	// Kept under the suite's 20s testTimeout so a genuinely missing sidebar fails
	// as an assertion here rather than as an opaque test-level timeout.
	await waitFor(() => expect(document.querySelector('[data-sidebar="footer"]')).not.toBeNull(), {
		timeout: 15_000,
	});
	return view!;
}

beforeEach(() => {
	shellMocks.navigate.mockReset();
	shellMocks.onNewSessionShortcut.mockClear();
	shellMocks.onKeyboardShortcutsHelp.mockClear();
	shellMocks.onNewShellTerminalShortcut.mockClear();
	shellMocks.openShellTerminal.mockClear();
	shellMocks.onOpenSettingsShortcut.mockClear();
	shellMocks.onPreviousSessionShortcut.mockClear();
	shellMocks.onNextSessionShortcut.mockClear();
	shellMocks.onFocusTerminalShortcut.mockClear();
	shellMocks.state.routeParams = {};
	shellMocks.state.routeSearch = {};
	shellMocks.state.workspaces = [];
	shellMocks.state.workspaceQuery = { data: [], dataUpdatedAt: 0, isError: false, isSuccess: true };
	shellMocks.state.daemonStatus = { state: "ready", port: 4321 };

	quotaMocks.state.trusted = true;
	// One harness with real usage windows, so the assertions exercise the live
	// data path rather than an empty-state placeholder.
	quotaMocks.state.probeStatuses = [
		{
			harness: "codex",
			state: "ok",
			hasData: true,
			probedAt: "2026-08-07T10:00:00Z",
			snapshots: [
				{ windowName: "primary", used: 42, windowEnd: "2026-08-09T10:00:00Z" },
				{ windowName: "secondary", used: 7, windowEnd: "2026-08-14T10:00:00Z" },
			],
		},
	];
	quotaMocks.getMock.mockReset();
	quotaMocks.getMock.mockImplementation((url: string) => {
		if (url === "/api/v1/metrics") {
			return Promise.resolve({
				data: { history: [], probeStatuses: quotaMocks.state.probeStatuses, latest: { quotas: [] } },
				error: undefined,
				response: { status: 200 },
			});
		}
		return Promise.resolve({ data: undefined, error: undefined, response: { status: 200 } });
	});
	quotaMocks.postMock.mockReset();
	quotaMocks.postMock.mockResolvedValue({ data: { statuses: [] }, error: undefined });

	useUiStore.setState({
		createProjectNonce: 0,
		isSidebarOpen: true,
		newTaskRequest: null,
		newShellTerminalNonce: 0,
		settingsModal: null,
		isQuotaWidgetVisible: true,
		isQuotaWidgetCollapsed: false,
	});
});

describe("quota meter mount guard (application entry point)", () => {
	it("renders the quota meter in the sidebar footer above Settings", async () => {
		await renderShell();

		const quota = await screen.findByText("Quota");
		const settings = screen.getAllByRole("button", { name: "Settings" })[0];
		expect(settings).toBeInTheDocument();
		// The meter precedes the Settings row in footer DOM order — the slot the
		// fork established in fdd67ef76.
		expect(quota.compareDocumentPosition(settings) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
		expect(quota.closest('[data-sidebar="footer"]')).toBeInTheDocument();
	});

	it("shows live probe data from the daemon rather than placeholder state", async () => {
		await renderShell();

		// Harness label resolved from the agents inventory, plus the real
		// percentages and window names carried by the daemon's probe snapshots.
		expect(await screen.findByText("Codex")).toBeInTheDocument();
		expect(screen.getByText("primary")).toBeInTheDocument();
		expect(screen.getByText("42%")).toBeInTheDocument();
		expect(screen.getByText("secondary")).toBeInTheDocument();
		expect(screen.getByText("7%")).toBeInTheDocument();
		expect(screen.getByRole("progressbar", { name: "Codex primary quota usage" })).toHaveAttribute(
			"aria-valuenow",
			"42",
		);
		expect(screen.queryByText("not probed yet")).not.toBeInTheDocument();
		expect(screen.queryByText("no usage recorded yet")).not.toBeInTheDocument();
	});

	it("hides the quota meter when the settings visibility toggle is off", async () => {
		useUiStore.setState({ isQuotaWidgetVisible: false });
		await renderShell();

		await waitFor(() => expect(screen.getAllByRole("button", { name: "Settings" })[0]).toBeInTheDocument());
		expect(screen.queryByText("Quota")).not.toBeInTheDocument();
	});
});
