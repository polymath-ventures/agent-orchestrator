/**
 * Mount guard for the agent-harness quota/usage meter (fork feature 3).
 *
 * This suite renders the real application entry point — the `/_shell` route
 * layout — with the REAL `Sidebar` and the REAL sidebar primitives, and asserts
 * the quota meter is actually on screen. `QuotaPanel.test.tsx` renders the
 * component directly, so it kept passing for the entire period the widget was
 * unmounted from the app (agent-orchestrator#280): the 2026-08-07 upstream sync
 * dropped the `<QuotaPanel />` mount from `Sidebar.tsx` and nothing failed.
 *
 * That is the regression this file exists to catch. Do not mock `../components/Sidebar`
 * here, and do not narrow these tests to render `Sidebar` (or `QuotaPanel`)
 * directly — the whole point is that the assertion travels the same mount path
 * the shipped app does, so removing the widget from EITHER the shell or the
 * sidebar turns CI red.
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { Suspense, type ComponentType, type PropsWithChildren } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../stores/ui-store";
import type { WorkspaceSummary } from "../types/workspace";

const shellMocks = vi.hoisted(() => {
	const state = {
		routeParams: {} as { projectId?: string; sessionId?: string },
		routeSearch: {} as Record<string, unknown>,
		daemonStatus: { state: "ready", port: 4321 } as {
			state: "ready" | "starting" | "stopped" | "error";
			port?: number;
			code?: "not_ready";
		},
		trusted: true,
		probeStatuses: [] as unknown[],
	};
	return {
		navigate: vi.fn(),
		getMock: vi.fn(),
		postMock: vi.fn(),
		subscribe: vi.fn(() => vi.fn()),
		openShellTerminal: vi.fn(),
		state,
	};
});

// AnimatePresence exit animations would keep a removed widget alive past the
// assertion; unmount children immediately instead.
vi.mock("motion/react", async (importOriginal) => {
	const actual = await importOriginal<typeof import("motion/react")>();
	return {
		...actual,
		AnimatePresence: ({ children }: { children: React.ReactNode }) => children,
	};
});

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
			onNewSessionShortcut: shellMocks.subscribe,
			onKeyboardShortcutsHelp: shellMocks.subscribe,
			onNewShellTerminalShortcut: shellMocks.subscribe,
			onOpenSettingsShortcut: shellMocks.subscribe,
			onPreviousSessionShortcut: shellMocks.subscribe,
			onNextSessionShortcut: shellMocks.subscribe,
			onFocusTerminalShortcut: shellMocks.subscribe,
		},
		keybindings: {
			get: vi.fn(async () => ({})),
			set: vi.fn(async (overrides: unknown) => overrides),
			setRecording: vi.fn(async () => undefined),
		},
		updates: {
			getStatus: vi.fn(async () => ({ state: "idle" })),
			onStatus: shellMocks.subscribe,
		},
		// The sidebar footer also carries the cloud-account row, which subscribes on
		// mount; without this the footer throws before the quota meter can render.
		cloud: {
			getSession: vi.fn(async () => null),
			onSessionChanged: shellMocks.subscribe,
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

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: shellMocks.getMock, POST: shellMocks.postMock },
	apiErrorMessage: (error: unknown) => {
		if (error instanceof Error) return error.message;
		if (typeof error === "object" && error !== null && "message" in error && typeof error.message === "string") {
			return error.message;
		}
		return "Request failed";
	},
	// The quota meter's metrics query is gated on a trusted API base URL.
	hasTrustedApiBaseUrl: () => shellMocks.state.trusted,
}));

vi.mock("../lib/rename-session", () => ({ renameSession: vi.fn().mockResolvedValue(undefined) }));
vi.mock("../lib/spawn-orchestrator", () => ({ spawnOrchestrator: vi.fn() }));
vi.mock("../hooks/useCommandPaletteEnabled", () => ({ useCommandPaletteEnabled: () => false }));

const workspaces = [] as unknown as WorkspaceSummary[];

vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: () => ({ data: [], dataUpdatedAt: 0, isError: false, isSuccess: true }),
	workspaceQueryKey: ["workspaces"],
	// The shell force-fetches this on daemon-ready to settle its startup state;
	// it needs a real queryFn or the shell stays in its loading shell forever.
	workspaceQueryOptions: { queryKey: ["workspaces"], queryFn: async () => [] },
}));

vi.mock("../hooks/usePrimeSettingsQuery", () => ({
	usePrimeEnabledQuery: () => ({ data: false }),
}));

vi.mock("../hooks/useDaemonStatus", () => ({
	useDaemonStatus: () => shellMocks.state.daemonStatus,
}));

vi.mock("../hooks/useShellTerminals", () => ({
	useShellTerminals: () => ({ data: [], isSuccess: true }),
	useOpenShellTerminal: () => ({ mutate: shellMocks.openShellTerminal }),
}));

// The harness inventory supplies the meter's per-harness display labels.
vi.mock("../hooks/useAgentsQuery", () => ({
	agentsQueryKey: ["agents"],
	agentsQueryOptions: {},
	refreshAgents: vi.fn(),
	useAgentsQuery: () => ({
		data: {
			supported: [{ id: "codex", label: "Codex" }],
			installed: [{ id: "codex", label: "Codex" }],
			authorized: [{ id: "codex", label: "Codex", authStatus: "authorized" }],
		},
	}),
}));

// Chrome the shell mounts around the sidebar but which this guard does not cover.
vi.mock("../components/NotificationCenter", () => ({ NotificationRuntime: () => null }));
vi.mock("../components/CommandPalette", () => ({ CommandPalette: () => null }));
vi.mock("../components/OrchestratorReplacementDialog", () => ({ OrchestratorReplacementDialog: () => null }));
vi.mock("../components/ShellTopbar", () => ({ ShellTopbar: () => null }));
vi.mock("../components/TitlebarNav", () => ({ TitlebarNav: () => null }));
vi.mock("../components/WindowTitlebar", () => ({ WindowTitlebar: () => null }));
vi.mock("../components/SettingsDialog", () => ({ SettingsDialog: () => null }));
vi.mock("../components/KeyboardShortcutsDialog", () => ({ KeyboardShortcutsDialog: () => null }));
vi.mock("../components/GlobalNewTaskDialog", () => ({ GlobalNewTaskDialog: () => null }));
vi.mock("../lib/shell-context", async (importOriginal) => ({
	...(await importOriginal<typeof import("../lib/shell-context")>()),
	ShellProvider: ({ children }: PropsWithChildren) => children,
}));

import { Route } from "../routes/_shell";

const ShellRoute = Route.options.component as ComponentType;

function metricsResponse() {
	return {
		data: {
			history: [],
			probeStatuses: shellMocks.state.probeStatuses,
			latest: { quotas: [] },
		},
		error: undefined,
		response: { status: 200 },
	};
}

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
	return view!;
}

beforeEach(() => {
	shellMocks.navigate.mockReset();
	shellMocks.openShellTerminal.mockReset();
	shellMocks.state.routeParams = {};
	shellMocks.state.routeSearch = {};
	shellMocks.state.daemonStatus = { state: "ready", port: 4321 };
	shellMocks.state.trusted = true;
	// One harness with a real usage window, so the assertions below exercise the
	// live-data path rather than an empty-state placeholder.
	shellMocks.state.probeStatuses = [
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
	shellMocks.getMock.mockReset();
	shellMocks.getMock.mockImplementation((url: string) => {
		if (url === "/api/v1/metrics") return Promise.resolve(metricsResponse());
		return Promise.resolve({ data: undefined, error: undefined, response: { status: 200 } });
	});
	shellMocks.postMock.mockReset();
	shellMocks.postMock.mockResolvedValue({ data: { statuses: [] }, error: undefined });
	useUiStore.setState({
		isSidebarOpen: true,
		createProjectNonce: 0,
		newTaskRequest: null,
		newShellTerminalNonce: 0,
		settingsModal: null,
		isQuotaWidgetVisible: true,
		isQuotaWidgetCollapsed: false,
	});
	void workspaces;
});

describe("quota meter mount guard (application entry point)", () => {
	it("renders the quota meter in the sidebar footer above Settings", async () => {
		await renderShell();

		const quota = await screen.findByText("Quota");
		const settings = screen.getAllByRole("button", { name: "Settings" })[0];
		expect(settings).toBeInTheDocument();
		// The meter precedes the Settings row in the footer DOM order, which is the
		// slot the fork established in fdd67ef76.
		expect(quota.compareDocumentPosition(settings) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
		expect(quota.closest('[data-sidebar="footer"]')).toBeInTheDocument();
	});

	it("shows live probe data from the daemon rather than placeholder state", async () => {
		await renderShell();

		// Harness label resolved from the agents inventory, plus the real percentages
		// and window names carried by the daemon's probe snapshots.
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
