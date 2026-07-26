import net from "node:net";

// ponytail: no heartbeat. The open socket IS the liveness signal. When the
// Electron process dies the kernel closes the fd and the daemon detects EOF
// immediately (proven against the real daemon with a write-free held
// connection). A heartbeat adds nothing for a Unix domain socket or named
// pipe and is omitted deliberately.
//
// HANDSHAKE_TOKEN does not change that. It is written exactly once, on connect,
// as an identity proof — not a heartbeat and not a keepalive. Nothing is ever
// written again for the life of the connection, and the daemon never expects
// anything more. The token exists because the connection alone is too weak a
// credential: without it, any transient probe of the socket would count as a
// live supervisor client and its close would schedule a daemon shutdown. The
// daemon only counts a connection once it has read these exact bytes; see
// HandshakeToken in backend/internal/daemon/supervisor/supervisor.go, which is
// the source of truth for the value.
export const HANDSHAKE_TOKEN = "ao-supervisor/1\n";

const BACKOFF_INIT_MS = 200;
const BACKOFF_MAX_MS = 2_000;

export interface SupervisorLinkHandle {
	readonly connected: boolean;
	dispose(): void;
}

/**
 * Hold one client connection to the daemon's supervisor socket for the
 * lifetime of the Electron process. When this process exits for any reason
 * (Cmd+Q, crash, SIGKILL), the OS closes the fd. The daemon detects EOF and
 * self-stops after its ~5s grace period, leaving tmux/ConPTY sessions alive
 * for the next boot to adopt.
 *
 * Retry semantics: if the daemon has not created the socket yet (or restarts),
 * we reconnect with bounded exponential backoff so the link re-establishes
 * automatically. dispose() cancels any pending retry and destroys the socket.
 */
export function connectSupervisor(addr: string, opts?: { log?: (msg: string) => void }): SupervisorLinkHandle {
	const log = opts?.log ?? (() => undefined);

	let disposed = false;
	let connected = false;
	let socket: net.Socket | null = null;
	let retryTimer: ReturnType<typeof setTimeout> | null = null;
	let backoff = BACKOFF_INIT_MS;

	function clearRetry() {
		if (retryTimer !== null) {
			clearTimeout(retryTimer);
			retryTimer = null;
		}
	}

	function destroySocket() {
		if (socket !== null) {
			socket.removeAllListeners();
			socket.destroy();
			socket = null;
		}
	}

	function scheduleReconnect() {
		if (disposed) return;
		clearRetry();
		const delay = backoff;
		backoff = Math.min(backoff * 2, BACKOFF_MAX_MS);
		log(`supervisor-link: reconnecting in ${delay}ms`);
		retryTimer = setTimeout(() => {
			retryTimer = null;
			if (!disposed) connect();
		}, delay);
	}

	function connect() {
		if (disposed) return;

		destroySocket();

		const s = net.connect(addr);
		socket = s;

		s.on("connect", () => {
			if (disposed) {
				s.destroy();
				return;
			}
			// Write the handshake before anything else: until the daemon has read
			// these bytes the connection does not count as a live client, so any
			// delay here is a window in which a frontend death goes unnoticed.
			// Re-sent on every reconnect, since each new connection must prove
			// itself again.
			s.write(HANDSHAKE_TOKEN);
			connected = true;
			log("supervisor-link: connected");
			// Reset backoff on successful connection.
			backoff = BACKOFF_INIT_MS;
		});

		// Drain inbound data: the daemon never sends payload; discard so the
		// socket buffer never stalls. ponytail: no payload to process.
		s.on("data", () => undefined);

		s.on("error", (err) => {
			log(`supervisor-link: error: ${err.message}`);
			// close fires after error, which schedules the reconnect.
		});

		s.on("close", () => {
			connected = false;
			if (disposed) return;
			log("supervisor-link: connection closed, will retry");
			scheduleReconnect();
		});
	}

	connect();

	return {
		get connected() {
			return connected;
		},
		dispose() {
			disposed = true;
			connected = false;
			clearRetry();
			destroySocket();
		},
	};
}
