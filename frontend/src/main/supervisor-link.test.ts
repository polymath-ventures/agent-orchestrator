// @vitest-environment node
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { describe, it, expect, afterEach } from "vitest";
import { connectSupervisor, HANDSHAKE_TOKEN, type SupervisorLinkHandle } from "./supervisor-link";

// Bounded wait: resolves when the promise resolves, rejects after timeoutMs.
function withTimeout<T>(promise: Promise<T>, timeoutMs: number, label: string): Promise<T> {
	return new Promise((resolve, reject) => {
		const timer = setTimeout(() => reject(new Error(`Timeout: ${label}`)), timeoutMs);
		promise.then(
			(v) => {
				clearTimeout(timer);
				resolve(v);
			},
			(e) => {
				clearTimeout(timer);
				reject(e);
			},
		);
	});
}

function tmpSocketPath(): string {
	return path.join(os.tmpdir(), `ao-svlink-test-${process.pid}-${Date.now()}.sock`);
}

// Promisify: resolves the next time server.on("connection") fires.
function nextConnection(server: net.Server): Promise<net.Socket> {
	return new Promise((resolve) => {
		server.once("connection", resolve);
	});
}

// Resolves once at least `want` bytes have arrived, decoded as UTF-8. A stream
// socket makes no framing promise, so waiting for a single "data" event can
// resolve on a partial token and fail a correct client. Accepted sockets start
// paused, so attaching the handler after the client has already written does
// not lose the data.
function readAtLeast(sock: net.Socket, want: number): Promise<string> {
	return new Promise((resolve) => {
		const chunks: Buffer[] = [];
		let total = 0;
		const onData = (buf: Buffer) => {
			chunks.push(buf);
			total += buf.length;
			if (total < want) return;
			sock.off("data", onData);
			resolve(Buffer.concat(chunks).toString("utf8"));
		};
		sock.on("data", onData);
	});
}

// Guards the helper the handshake assertions depend on. A stream socket may
// deliver the token in pieces, and a helper that took only the first "data"
// event would compare a partial token and fail a correct client. Splitting the
// write here reproduces that framing without needing a real slow network.
describe("readAtLeast", () => {
	it("assembles a token delivered across multiple chunks", async () => {
		const addr = tmpSocketPath();
		const server = net.createServer();
		servers.push(server);
		const connectionPromise = nextConnection(server);
		await new Promise<void>((resolve, reject) => {
			server.listen(addr, () => resolve());
			server.once("error", reject);
		});

		const client = net.connect(addr);
		const conn = await withTimeout(connectionPromise, 3_000, "readAtLeast: server did not accept");
		// try/finally, so a regression that never resolves still tears the sockets
		// down. Otherwise afterEach blocks on server.close() with a live
		// connection and the real failure is buried under a hook timeout.
		try {
			const received = readAtLeast(conn, HANDSHAKE_TOKEN.length);

			// Deliberately split mid-token, with a gap, so a one-chunk read resolves short.
			const split = 6;
			client.write(HANDSHAKE_TOKEN.slice(0, split));
			await new Promise<void>((r) => setTimeout(r, 50));
			client.write(HANDSHAKE_TOKEN.slice(split));

			expect(await withTimeout(received, 3_000, "readAtLeast: never assembled the full token")).toBe(HANDSHAKE_TOKEN);
		} finally {
			client.destroy();
			conn.destroy();
		}
	});

	const servers: net.Server[] = [];
	afterEach(async () => {
		await Promise.all(
			servers.splice(0).map(
				(s) =>
					new Promise<void>((resolve) => {
						s.close(() => resolve());
					}),
			),
		);
	});
});

describe("supervisor-link", () => {
	const handles: SupervisorLinkHandle[] = [];
	const servers: net.Server[] = [];

	afterEach(async () => {
		for (const h of handles.splice(0)) h.dispose();
		await Promise.all(
			servers.splice(0).map(
				(s) =>
					new Promise<void>((resolve) => {
						s.close(() => resolve());
					}),
			),
		);
	});

	it("retries until connected: connects after server is started later", async () => {
		const addr = tmpSocketPath();

		// Start the link BEFORE the server exists.
		const link = connectSupervisor(addr, { log: () => undefined });
		handles.push(link);

		// Wait a bit so a few retry attempts have fired against a missing socket.
		await new Promise<void>((r) => setTimeout(r, 400));

		// Now start the server.
		const server = net.createServer();
		servers.push(server);
		const connectionPromise = nextConnection(server);
		await new Promise<void>((resolve, reject) => {
			server.listen(addr, () => resolve());
			server.once("error", reject);
		});

		// The link should reconnect and the server should receive a connection.
		const conn = await withTimeout(
			connectionPromise,
			5_000,
			"retry-until-connected: server did not receive connection",
		);
		expect(conn).toBeTruthy();
		conn.destroy();
	});

	it("reconnects on drop: re-establishes after the accepted socket is closed", async () => {
		const addr = tmpSocketPath();

		// Start server first.
		const server = net.createServer();
		servers.push(server);

		let connectionCount = 0;
		const secondConnection = new Promise<net.Socket>((resolve) => {
			let first = true;
			server.on("connection", (sock) => {
				connectionCount++;
				if (first) {
					first = false;
					// Close the first accepted socket to simulate a drop.
					setTimeout(() => sock.destroy(), 50);
				} else {
					resolve(sock);
				}
			});
		});

		await new Promise<void>((resolve, reject) => {
			server.listen(addr, () => resolve());
			server.once("error", reject);
		});

		// Connect after server is up.
		const link = connectSupervisor(addr, { log: () => undefined });
		handles.push(link);

		// Wait for both the initial connection and the reconnect.
		const reconn = await withTimeout(secondConnection, 6_000, "reconnect-on-drop: second connection never arrived");
		expect(connectionCount).toBeGreaterThanOrEqual(2);
		reconn.destroy();
	});

	it("connected flag: true after connect, false after server closes connection", async () => {
		const addr = tmpSocketPath();

		const server = net.createServer();
		servers.push(server);
		const connectionPromise = nextConnection(server);
		await new Promise<void>((resolve, reject) => {
			server.listen(addr, () => resolve());
			server.once("error", reject);
		});

		const link = connectSupervisor(addr, { log: () => undefined });
		handles.push(link);

		// Wait for the server to receive the connection.
		const conn = await withTimeout(connectionPromise, 3_000, "connected-flag: server did not receive connection");

		// Poll until connected is true (the "connect" event fires asynchronously).
		await withTimeout(
			new Promise<void>((resolve) => {
				const check = () => {
					if (link.connected) {
						resolve();
						return;
					}
					setTimeout(check, 20);
				};
				check();
			}),
			1_000,
			"connected-flag: handle.connected never became true",
		);
		expect(link.connected).toBe(true);

		// Server-side close of the accepted socket triggers the client "close" event.
		conn.destroy();

		// Poll until connected drops back to false.
		await withTimeout(
			new Promise<void>((resolve) => {
				const check = () => {
					if (!link.connected) {
						resolve();
						return;
					}
					setTimeout(check, 20);
				};
				check();
			}),
			3_000,
			"connected-flag: handle.connected never became false after server closed",
		);
		expect(link.connected).toBe(false);

		link.dispose();
	});

	it("dispose stops reconnect: no connection arrives after dispose", async () => {
		const addr = tmpSocketPath();

		// Start link against a missing socket (no server), then dispose quickly.
		const link = connectSupervisor(addr, { log: () => undefined });

		// Dispose before the server exists.
		link.dispose();

		// Start a server and assert no connection arrives within a bounded window.
		const server = net.createServer();
		servers.push(server);
		let receivedConnection = false;
		server.on("connection", () => {
			receivedConnection = true;
		});
		await new Promise<void>((resolve, reject) => {
			server.listen(addr, () => resolve());
			server.once("error", reject);
		});

		// Wait long enough for at least one retry cycle to have run if dispose failed.
		await new Promise<void>((r) => setTimeout(r, 600));

		expect(receivedConnection).toBe(false);
	});

	// #147: the connection alone is not the credential. The daemon only counts a
	// client once it has read the handshake token, so a link that fails to write
	// it is invisible to the watchdog and a frontend death goes undetected.
	it("handshake: writes the token on connect", async () => {
		const addr = tmpSocketPath();

		const server = net.createServer();
		servers.push(server);
		const connectionPromise = nextConnection(server);
		await new Promise<void>((resolve, reject) => {
			server.listen(addr, () => resolve());
			server.once("error", reject);
		});

		const link = connectSupervisor(addr, { log: () => undefined });
		handles.push(link);

		const conn = await withTimeout(connectionPromise, 3_000, "handshake: server did not receive connection");
		const received = await withTimeout(
			readAtLeast(conn, HANDSHAKE_TOKEN.length),
			3_000,
			"handshake: the full token never arrived on the accepted socket",
		);

		expect(received).toBe(HANDSHAKE_TOKEN);
		conn.destroy();
	});

	it("handshake: re-sends the token on reconnect", async () => {
		const addr = tmpSocketPath();

		const server = net.createServer();
		servers.push(server);

		// Collect a full token from every accepted connection, and drop the first
		// one so the link has to reconnect and prove itself again. Reading to the
		// token's length rather than taking one chunk keeps this from resolving on
		// a partial write that a stream socket is free to deliver.
		const tokens: string[] = [];
		const twoHandshakes = new Promise<void>((resolve) => {
			let first = true;
			server.on("connection", (sock) => {
				void readAtLeast(sock, HANDSHAKE_TOKEN.length).then((token) => {
					tokens.push(token);
					if (tokens.length >= 2) resolve();
				});
				if (first) {
					first = false;
					setTimeout(() => sock.destroy(), 50);
				}
			});
		});

		await new Promise<void>((resolve, reject) => {
			server.listen(addr, () => resolve());
			server.once("error", reject);
		});

		const link = connectSupervisor(addr, { log: () => undefined });
		handles.push(link);

		await withTimeout(twoHandshakes, 6_000, "handshake-on-reconnect: fewer than two handshakes arrived");

		// slice(0, 2): a further reconnect may land before the assertion runs, and
		// the claim under test is about the first two connections.
		expect(tokens.slice(0, 2)).toEqual([HANDSHAKE_TOKEN, HANDSHAKE_TOKEN]);
	});
});
