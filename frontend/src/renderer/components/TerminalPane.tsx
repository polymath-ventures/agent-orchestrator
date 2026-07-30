import { useQueryClient } from "@tanstack/react-query";
import { RotateCcw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { relaunchPrime } from "../lib/relaunch-prime";
import type { TerminalTarget } from "../types/terminal";
import { isPrimeSession, sessionIsActive, type WorkspaceSession } from "../types/workspace";
import { useUiStore, type Theme } from "../stores/ui-store";
import { useTerminalSession, type AttachableTerminal, type TerminalSessionState } from "../hooks/useTerminalSession";
import { apiClient } from "../lib/api-client";
import { createUrlWatcher, type UrlWatcher } from "../lib/detect-urls";
import { cn } from "../lib/utils";
import { workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { useRestoreSession } from "../hooks/useRestoreSession";
import { XtermTerminal } from "./XtermTerminal";
import { RestoreUnavailableDialog } from "./RestoreUnavailableDialog";

type TerminalPaneProps = {
	session?: WorkspaceSession;
	theme: Theme;
	daemonReady: boolean;
	terminalTarget?: TerminalTarget;
	fontSize: number;
	/**
	 * The mount means the user switched to this terminal, so it should take the
	 * keyboard rather than wait for a click. Opt-in: a pane also mounts behind a
	 * pop-out overlay, and re-keys in the background when a starting session is
	 * finally assigned a terminal handle — neither should move focus.
	 */
	autoFocus?: boolean;
	focusRequest?: number;
	onExitFocus?: () => void;
};

export function TerminalPane({
	session,
	theme,
	daemonReady,
	terminalTarget,
	fontSize,
	autoFocus,
	focusRequest,
	onExitFocus,
}: TerminalPaneProps) {
	const previousFocusRequestRef = useRef<number | undefined>(undefined);
	const focusRequestIsFresh = focusRequest !== undefined && focusRequest !== previousFocusRequestRef.current;
	useEffect(() => {
		if (focusRequest !== undefined) previousFocusRequestRef.current = focusRequest;
	}, [focusRequest]);
	const effectiveAutoFocus = Boolean(autoFocus) && (focusRequest === undefined || focusRequestIsFresh);
	const terminalKey =
		terminalTarget?.kind === "reviewer" || terminalTarget?.kind === "shell"
			? terminalTarget.handleId
			: (session?.terminalHandleId ?? "empty");

	return (
		<AttachedTerminal
			key={terminalKey}
			autoFocus={effectiveAutoFocus}
			focusRequest={focusRequest}
			session={session}
			theme={theme}
			daemonReady={daemonReady}
			fontSize={fontSize}
			terminalTarget={terminalTarget}
			onExitFocus={onExitFocus}
		/>
	);
}

// Agents whose full-screen TUI keeps its own transcript and scrolls it only by
// keyboard, ignoring SGR wheel reports. The terminal routes the wheel to
// PageUp/PageDown for these (see XtermTerminal's paneScrollsByKeyboard).
// kilocode is a fork of opencode and shares its TUI surface, so it scrolls the
// same way.
const KEYBOARD_SCROLL_PROVIDERS = new Set(["opencode", "kilocode"]);

// Whether the given provider's TUI is one of the keyboard-scroll agents above.
export function providerScrollsByKeyboard(provider?: string): boolean {
	return provider ? KEYBOARD_SCROLL_PROVIDERS.has(provider) : false;
}

function bannerText(state: TerminalSessionState, error?: string): string | undefined {
	if (state === "reattaching") return "Terminal disconnected — reattaching…";
	if (state === "error") return `Terminal error: ${error ?? "connection failed"}`;
	return undefined;
}

function AttachedTerminal({
	session,
	theme,
	daemonReady,
	terminalTarget,
	fontSize,
	autoFocus,
	focusRequest,
	onExitFocus,
}: TerminalPaneProps) {
	const attachSession =
		session && terminalTarget?.kind === "reviewer"
			? { ...session, terminalHandleId: terminalTarget.handleId }
			: session;
	// One terminal instance per handle-scoped pane lifetime. TerminalPane keys this
	// component by terminal handle, so session switches get a fresh xterm + mux
	// hook state instead of reusing a potentially stale screen/input binding.
	const [terminal, setTerminal] = useState<AttachableTerminal | null>(null);
	const [initFailed, setInitFailed] = useState(false);
	const [isRestoring, setIsRestoring] = useState(false);
	const [restoreError, setRestoreError] = useState<string | undefined>();
	const [restoreUnavailable, setRestoreUnavailable] = useState(false);
	const [isRelaunchingPrime, setIsRelaunchingPrime] = useState(false);
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const restoreSessionById = useRestoreSession();
	// A shell pane has no session, so it hands the hook its handle directly
	// instead of reading one off `attachSession`.
	const shellTerminalHandleId = terminalTarget?.kind === "shell" ? terminalTarget.handleId : undefined;
	// Glow the Browser tab when the agent prints a URL in this worker's terminal
	// (e.g. a pushed-PR link). Detection only badges — the user still chooses to
	// open it — and is skipped while they are already looking at the Browser tab.
	const watchLinks = Boolean(session?.id && session.kind === "worker" && terminalTarget?.kind !== "shell");
	const urlWatcherRef = useRef<UrlWatcher | null>(null);
	const handleOutput = useCallback(
		(text: string) => {
			const sessionId = session?.id;
			if (!sessionId) return;
			if (!urlWatcherRef.current) {
				urlWatcherRef.current = createUrlWatcher(() => {
					const store = useUiStore.getState();
					const current = store.inspectorSessions[sessionId];
					const viewingBrowser = (current?.isOpen ?? true) && (current?.view ?? "summary") === "browser";
					if (!viewingBrowser) store.setBrowserUnseen(sessionId, true);
				});
			}
			urlWatcherRef.current.push(text);
		},
		[session?.id],
	);
	const { attach, state, error, replaySettled } = useTerminalSession(attachSession, {
		daemonReady,
		shellTerminalHandleId,
		onOutput: watchLinks ? handleOutput : undefined,
	});
	const handleId = shellTerminalHandleId ?? attachSession?.terminalHandleId;
	const provider = terminalTarget?.kind === "reviewer" ? terminalTarget.harness : session?.provider;
	const hadAttachmentRef = useRef(false);
	const isSessionActive = session ? sessionIsActive(session) : false;
	// A standalone shell is never restorable: there is no session row to restore.
	// Prime is excluded too: the daemon forbids generic restore for Prime
	// (PRIME_MANUAL_RESTORE_FORBIDDEN), so offering the restore control here
	// only ever produced a 403. Prime recovers through relaunch instead.
	const isDeadPrime = !!session && isPrimeSession(session) && session.status === "terminated";
	const canRestoreSession =
		terminalTarget?.kind !== "reviewer" &&
		terminalTarget?.kind !== "shell" &&
		session !== undefined &&
		!isSessionActive &&
		!isDeadPrime;

	const handleReady = useCallback((handle: AttachableTerminal) => {
		setTerminal(handle);
	}, []);
	const handleInitError = useCallback((err: unknown) => {
		console.error("xterm failed to initialize", err);
		setInitFailed(true);
	}, []);
	const setInspectorViewForSession = useUiStore((state) => state.setInspectorView);
	const setInspectorOpenForSession = useUiStore((state) => state.setInspectorOpen);
	const handleLinkOpen = useCallback(
		(uri: string) => {
			if (!session?.id || session.kind !== "worker" || !isSessionActive) return;
			try {
				const url = new URL(uri);
				if (url.protocol !== "http:" && url.protocol !== "https:") return;
			} catch {
				return;
			}
			const linkSessionId = session.id;
			// A left-click is an explicit request to view the link, so open the
			// Browser tab now (unlike a passive `ao preview`, which only badges it).
			setInspectorViewForSession(linkSessionId, "browser");
			setInspectorOpenForSession(linkSessionId, true);
			void (async () => {
				try {
					const { error: previewError } = await apiClient.POST("/api/v1/sessions/{sessionId}/preview", {
						params: { path: { sessionId: linkSessionId } },
						body: { url: uri },
					});
					if (previewError) {
						console.warn("Unable to open terminal link in Browser preview", previewError);
						return;
					}
					await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
				} catch (error) {
					console.warn("Unable to open terminal link in Browser preview", error);
				}
			})();
		},
		[isSessionActive, queryClient, session?.id, session?.kind, setInspectorOpenForSession, setInspectorViewForSession],
	);
	const restoreSession = useCallback(async () => {
		if (!session?.id || !canRestoreSession || isRestoring) return;
		setIsRestoring(true);
		setRestoreError(undefined);
		try {
			const result = await restoreSessionById(session.id);
			if (result.status === "not_resumable") {
				setRestoreUnavailable(true);
				return;
			}
			if (result.status === "error") {
				setRestoreError(result.message);
			}
		} catch (err) {
			setRestoreError(err instanceof Error ? err.message : "Unable to restore session");
		} finally {
			setIsRestoring(false);
		}
	}, [canRestoreSession, isRestoring, restoreSessionById, session?.id]);

	useEffect(() => {
		if (!terminal) return;
		// Reuse means the previous session's screen would linger; clear before
		// re-pointing. Screen-clear only, never reset(): every pane PTY is
		// `zellij attach` with identical modes, so the previous session's mouse
		// tracking stays valid while the new attach's handshake + repaint stream
		// in — a full RIS would leave wheel scroll dead for that window (yyork's
		// frozen-scroll regression, solved there the same way). Skipped on the
		// very first attachment: the buffer is empty and the first fit may not
		// have run yet.
		if (hadAttachmentRef.current) {
			terminal.clear();
		}
		hadAttachmentRef.current = true;
		return attach(terminal);
	}, [terminal, handleId, attach, attachSession?.id]);

	// Declared above every conditional return: a hook after the initFailed early
	// return changes the hook count between renders and crashes React with
	// "Rendered fewer hooks than expected".
	const relaunchDeadPrime = useCallback(async () => {
		if (isRelaunchingPrime) return;
		setIsRelaunchingPrime(true);
		setRestoreError(undefined);
		try {
			const sessionId = await relaunchPrime();
			await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
			// Leave the dead session behind. Without this the pane stays bound to
			// the terminated row and keeps showing the recovery strip even though
			// a healthy Prime now exists.
			void navigate({ to: "/sessions/$sessionId", params: { sessionId } });
		} catch (err) {
			setRestoreError(err instanceof Error ? err.message : "Unable to relaunch Prime");
		} finally {
			setIsRelaunchingPrime(false);
		}
	}, [isRelaunchingPrime, queryClient, navigate]);

	if (initFailed) {
		return (
			<div className="grid h-full place-items-center bg-terminal p-4 font-mono text-xs text-muted-foreground">
				Terminal failed to initialize on this GPU/driver. Restart the app to retry.
			</div>
		);
	}

	const banner = bannerText(state, error);
	const showEmptyState = !handleId;
	// Cover xterm while the attachment buffers the initial replay, so the pane
	// appears already drawn at the tail instead of visibly scrolling down to it.
	// Deliberately NOT the empty state above: that renders a centered "Starting
	// session" card, and flashing it on every session switch would be worse than
	// the scroll it replaces.
	// Only while a replay is actually imminent. Gating on the state as well as
	// the gate keeps the cover from reappearing over a pane that is visibly
	// disconnected: an open timeout lifts it, the backoff reconnect would
	// otherwise pull it straight back down, and the "reattaching" banner already
	// explains that window better than a blank overlay does.
	const showReplayCover = Boolean(handleId) && !replaySettled && (state === "connecting" || state === "attached");
	const showEndedState = state === "exited" || canRestoreSession || isDeadPrime;
	const emptyStateTitle = session ? "Starting session" : "Agent Orchestrator";
	const emptyStateMessage = session
		? isPrimeSession(session)
			? "Preparing the prime terminal. This can take a moment while AO creates the workspace and starts the agent."
			: session.kind === "orchestrator"
				? "Preparing the orchestrator terminal. This can take a moment while AO creates the workspace and starts the agent."
				: "Preparing the worker terminal. This can take a moment while AO creates the workspace and starts the agent."
		: "No session selected. Pick a worker to attach its terminal.";

	return (
		<div className="flex h-full min-h-0 flex-col bg-terminal" data-testid="session-terminal">
			{showEndedState && (
				<TerminalEndedStrip
					canRestore={canRestoreSession}
					canRelaunchPrime={isDeadPrime}
					isRelaunchingPrime={isRelaunchingPrime}
					onRelaunchPrime={relaunchDeadPrime}
					error={restoreError}
					isRestoring={isRestoring}
					onRestore={restoreSession}
					variant={
						terminalTarget?.kind === "reviewer" ? "reviewer" : terminalTarget?.kind === "shell" ? "shell" : "session"
					}
				/>
			)}
			{/* p-2 keeps the xterm content off the pane edges; the host fills the
			    remaining content box, so FitAddon still measures it correctly and
			    the absolute overlays (empty state, banner) keep covering the
			    full padding box. */}
			<div className="relative min-h-0 flex-1 p-2">
				<XtermTerminal
					ariaLabel={terminalTarget?.kind === "shell" ? "Shell terminal" : "Session terminal"}
					// No handle means no PTY behind the pane and the empty-state overlay
					// covering it, so focusing would swallow keystrokes into nothing.
					// Today's opt-in call sites are shell targets, which always carry a
					// handle; this keeps the prop's contract true for any owner that opts
					// a handle-less pane in later.
					autoFocus={Boolean(autoFocus) && !showEmptyState}
					focusRequest={showEmptyState ? undefined : focusRequest}
					fontSize={fontSize}
					onError={handleInitError}
					onExitFocus={onExitFocus}
					onLinkOpen={handleLinkOpen}
					onReady={handleReady}
					paneScrollsByKeyboard={providerScrollsByKeyboard(provider)}
					theme={theme}
				/>
				{showEmptyState && (
					<div className="absolute inset-0 grid place-items-center bg-terminal font-mono text-control">
						<div className="text-center">
							<div className="text-terminal">{emptyStateTitle}</div>
							<div className="mt-2 text-terminal-dim">{emptyStateMessage}</div>
						</div>
					</div>
				)}
				{showReplayCover && <ReplayCover />}
				{banner && (
					<div className="absolute inset-x-3 top-2 rounded-md border border-border bg-surface/95 px-3 py-1.5 font-mono text-caption text-muted-foreground">
						{banner}
					</div>
				)}
			</div>
			{session && (
				<RestoreUnavailableDialog
					open={restoreUnavailable}
					session={session}
					onOpenChange={setRestoreUnavailable}
					onRecreated={async () => {
						await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
					}}
				/>
			)}
		</div>
	);
}

// Blank terminal-coloured cover held over xterm while the initial replay is
// buffered. A fast open (the common case) shows nothing at all — the label only
// appears if the wait is long enough to read as a stall rather than a repaint,
// so normal session switching never flashes a loader.
const REPLAY_COVER_LABEL_MS = 120;

function ReplayCover() {
	const [showLabel, setShowLabel] = useState(false);
	useEffect(() => {
		const timer = window.setTimeout(() => setShowLabel(true), REPLAY_COVER_LABEL_MS);
		return () => window.clearTimeout(timer);
	}, []);
	return (
		// pointer-events-none: the cover is purely visual and xterm underneath is
		// live the whole time, so clicks, selection and wheel must pass through
		// rather than being swallowed for the length of the gate.
		<div
			className="pointer-events-none absolute inset-0 grid place-items-center bg-terminal"
			data-testid="terminal-replay-cover"
		>
			{showLabel && <div className="font-mono text-caption text-terminal-dim">Loading latest output…</div>}
		</div>
	);
}

type TerminalEndedStripProps = {
	canRestore: boolean;
	canRelaunchPrime?: boolean;
	isRelaunchingPrime?: boolean;
	onRelaunchPrime?: () => void;
	error?: string;
	isRestoring: boolean;
	onRestore: () => void;
	variant: "reviewer" | "session" | "shell";
};

function TerminalEndedStrip({
	canRestore,
	canRelaunchPrime = false,
	isRelaunchingPrime = false,
	onRelaunchPrime,
	error,
	isRestoring,
	onRestore,
	variant,
}: TerminalEndedStripProps) {
	const message = canRelaunchPrime
		? "Prime has stopped. Relaunch Prime to start a fresh supervisor on the canonical branch."
		: canRestore
			? "Restore the session to attach a live terminal and continue writing."
			: variant === "reviewer"
				? "This reviewer terminal has ended. Re-run review from the summary panel, or switch back to the agent terminal."
				: variant === "shell"
					? "This shell exited. Close the tab, or open a new terminal."
					: "This terminal process ended, but the session is not marked terminated yet.";

	return (
		<div className="shrink-0 border-b border-border bg-surface/80 px-4 py-2">
			<div className="flex min-h-control-board items-center gap-3">
				<div className="min-w-0 flex-1">
					<div className="font-mono text-caption font-medium uppercase tracking-wide-md text-muted-foreground">
						Terminal ended
					</div>
					<div className="mt-0.5 truncate text-xs text-muted-foreground">{message}</div>
				</div>
				{error && <div className="max-w-content-max truncate text-xs text-destructive">{error}</div>}
				{canRelaunchPrime && (
					<button
						type="button"
						aria-label="Relaunch Prime"
						title="Relaunch Prime"
						className="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-border bg-raised px-2.5 py-1 text-xs text-foreground transition hover:bg-interactive-hover disabled:cursor-not-allowed disabled:opacity-50"
						disabled={isRelaunchingPrime}
						onClick={() => onRelaunchPrime?.()}
					>
						<RotateCcw className={cn("size-icon-base", isRelaunchingPrime && "animate-spin")} aria-hidden="true" />
						Relaunch Prime
					</button>
				)}
				{canRestore && (
					<button
						type="button"
						aria-label="Restore session"
						title="Restore session"
						className="inline-flex size-control-form shrink-0 items-center justify-center rounded-md border border-border bg-raised text-foreground transition hover:bg-interactive-hover disabled:cursor-not-allowed disabled:opacity-50"
						disabled={isRestoring}
						onClick={onRestore}
					>
						<RotateCcw className={cn("size-icon-base", isRestoring && "animate-spin")} aria-hidden="true" />
					</button>
				)}
			</div>
		</div>
	);
}
