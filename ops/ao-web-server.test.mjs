import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, beforeEach, describe, it } from "node:test";

import { createAoWebServer } from "./ao-web-server.mjs";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

let cleanup = [];

beforeEach(() => {
	cleanup = [];
});

afterEach(async () => {
	await Promise.all(cleanup.splice(0).reverse().map((item) => item()));
});

describe("ao web production server", () => {
	it("serves built assets and falls back to index.html for SPA routes", async () => {
		const distDir = await makeDist();
		const server = await listen(createAoWebServer({ distDir, apiTarget: "http://127.0.0.1:9" }));

		const index = await fetchText(`${server.url}/projects/agent/sessions/one`);
		assert.match(index.body, /<div id="root"><\/div>/);
		assert.equal(index.headers.get("cache-control"), "no-store");

		const asset = await fetchText(`${server.url}/assets/app.js`);
		assert.equal(asset.body, "console.log('ao');\n");
		assert.equal(asset.headers.get("cache-control"), "public, max-age=60");

		const hashedAsset = await fetchText(`${server.url}/assets/app-12345678.js`);
		assert.equal(hashedAsset.body, "console.log('hashed ao');\n");
		assert.equal(hashedAsset.headers.get("cache-control"), "public, max-age=31536000, immutable");

		const manifest = await fetchText(`${server.url}/ao-web-build.json`);
		assert.match(manifest.body, /frontendTree/);
		assert.equal(manifest.headers.get("cache-control"), "no-store");

		const missingAsset = await fetchText(`${server.url}/assets/missing.js`);
		assert.equal(missingAsset.status, 404);
	});

	it("returns no-content for the implicit browser favicon request", async () => {
		const distDir = await makeDist();
		const server = await listen(createAoWebServer({ distDir, apiTarget: "http://127.0.0.1:9" }));

		const response = await fetch(`${server.url}/favicon.ico`);

		assert.equal(response.status, 204);
		assert.equal(response.headers.get("cache-control"), "public, max-age=86400");
	});

	it("rejects path traversal instead of reading outside the bundle", async () => {
		const distDir = await makeDist();
		const secretName = `secret-${path.basename(distDir)}.txt`;
		const secretPath = path.join(path.dirname(distDir), secretName);
		await writeFile(secretPath, "do not serve\n");
		cleanup.push(() => rm(secretPath, { force: true }));
		const server = await listen(createAoWebServer({ distDir, apiTarget: "http://127.0.0.1:9" }));

		const response = await fetchText(`${server.url}/assets/..%2f..%2f${secretName}`);

		assert.equal(response.status, 404);
	});

	it("proxies daemon HTTP routes after validating trusted browser requests", async () => {
		let seenOrigin;
		const daemon = await listen(
			http.createServer((request, response) => {
				assert.equal(request.url, "/api/v1/projects");
				assert.equal(request.headers.host, daemon.host);
				seenOrigin = request.headers.origin;
				response.setHeader("Content-Type", "application/json");
				response.end(JSON.stringify({ projects: [{ id: "ao" }] }));
			}),
		);
		const distDir = await makeDist();
		const server = await listen(
			createAoWebServer({
				distDir,
				apiTarget: daemon.url,
				publicUrl: "https://ao.tailnet.example/",
			}),
		);

		const response = await fetch(`${server.url}/api/v1/projects`, {
			headers: { Origin: "https://ao.tailnet.example" },
		});

		assert.equal(response.status, 200);
		assert.deepEqual(await response.json(), { projects: [{ id: "ao" }] });
		assert.equal(seenOrigin, undefined);
	});

	it("tears down upstream proxy requests when the browser aborts", async () => {
		let openStreams = 0;
		const daemon = await listen(
			http.createServer((_request, response) => {
				openStreams += 1;
				response.writeHead(200, { "Content-Type": "text/event-stream" });
				response.write("event: ping\ndata: {}\n\n");
				response.on("close", () => {
					openStreams -= 1;
				});
			}),
		);
		const distDir = await makeDist();
		const server = await listen(
			createAoWebServer({
				distDir,
				apiTarget: daemon.url,
				publicUrl: "https://ao.tailnet.example/",
			}),
		);
		const controller = new AbortController();

		const pending = fetch(`${server.url}/api/v1/events`, {
			headers: { Origin: "https://ao.tailnet.example" },
			signal: controller.signal,
		}).catch((error) => error);
		await waitFor(() => openStreams === 1);
		controller.abort();
		await pending;

		await waitFor(() => openStreams === 0);
	});

	it("rejects proxied browser requests from untrusted origins before they reach the daemon", async () => {
		let daemonHit = false;
		const daemon = await listen(
			http.createServer((_request, response) => {
				daemonHit = true;
				response.end("{}");
			}),
		);
		const distDir = await makeDist();
		const server = await listen(
			createAoWebServer({
				distDir,
				apiTarget: daemon.url,
				publicUrl: "https://ao.tailnet.example/",
			}),
		);

		const response = await fetch(`${server.url}/api/v1/projects`, {
			headers: { Origin: "https://evil.example" },
		});

		assert.equal(response.status, 403);
		assert.equal(daemonHit, false);
	});

	it("rejects proxied requests for an untrusted Host header", async () => {
		let daemonHit = false;
		const daemon = await listen(
			http.createServer((_request, response) => {
				daemonHit = true;
				response.end("{}");
			}),
		);
		const distDir = await makeDist();
		const server = await listen(
			createAoWebServer({
				distDir,
				apiTarget: daemon.url,
				publicUrl: "https://ao.tailnet.example/",
			}),
		);

		const response = await rawHttp({
			port: server.port,
			host: "evil.example",
			origin: "https://ao.tailnet.example",
			pathname: "/api/v1/projects",
		});

		assert.match(response, /^HTTP\/1\.1 403 Forbidden/);
		assert.equal(daemonHit, false);
	});

	it("proxies terminal mux websocket upgrades", async () => {
		let seenOrigin;
		const daemon = await listen(
			http.createServer().on("upgrade", (request, socket) => {
				assert.equal(request.url, "/mux");
				seenOrigin = request.headers.origin;
				socket.write("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n");
				socket.end("mux-opened");
			}),
		);
		const distDir = await makeDist();
		const server = await listen(
			createAoWebServer({
				distDir,
				apiTarget: daemon.url,
				publicUrl: "https://ao.tailnet.example/",
			}),
		);

		const response = await rawUpgrade({
			port: server.port,
			host: server.host,
			origin: "https://ao.tailnet.example",
			pathname: "/mux",
		});

		assert.match(response, /^HTTP\/1\.1 101 Switching Protocols/);
		assert.match(response, /mux-opened/);
		assert.equal(seenOrigin, undefined);
	});

	it("rejects websocket upgrades from untrusted origins before they reach the daemon", async () => {
		let daemonHit = false;
		const daemon = await listen(
			http.createServer().on("upgrade", (_request, socket) => {
				daemonHit = true;
				socket.end();
			}),
		);
		const distDir = await makeDist();
		const server = await listen(
			createAoWebServer({
				distDir,
				apiTarget: daemon.url,
				publicUrl: "https://ao.tailnet.example/",
			}),
		);

		const response = await rawUpgrade({
			port: server.port,
			host: server.host,
			origin: "https://evil.example",
			pathname: "/mux",
		});

		assert.match(response, /^HTTP\/1\.1 403 Forbidden/);
		assert.equal(daemonHit, false);
	});

	it("starts when invoked through the release current symlink", async () => {
		const distDir = await makeDist();
		const server = await startReleaseSymlinkServer(distDir);

		const response = await fetchText(server.url);

		assert.equal(response.status, 200);
		assert.match(response.body, /<div id="root"><\/div>/);
	});

	it("fails fast when the executable server is bound beyond loopback", async () => {
		const distDir = await makeDist();
		const child = spawn(process.execPath, [path.join(REPO_ROOT, "ops/ao-web-server.mjs")], {
			env: { ...process.env, AO_WEB_BIND: "0.0.0.0", AO_WEB_DIST: distDir, AO_WEB_PORT: "5174" },
			stdio: ["ignore", "ignore", "pipe"],
		});
		let stderr = "";
		child.stderr.on("data", (chunk) => {
			stderr += chunk.toString("utf8");
		});

		const code = await new Promise((resolve) => child.once("exit", resolve));

		assert.notEqual(code, 0);
		assert.match(stderr, /AO_WEB_BIND must be loopback-only/);
	});

	it("fails fast when a service-style launch requires a missing public URL", async () => {
		const distDir = await makeDist();
		const child = spawn(process.execPath, [path.join(REPO_ROOT, "ops/ao-web-server.mjs")], {
			env: { ...process.env, AO_WEB_DIST: distDir, AO_WEB_PORT: "5174", AO_WEB_REQUIRE_PUBLIC_URL: "1" },
			stdio: ["ignore", "ignore", "pipe"],
		});
		let stderr = "";
		child.stderr.on("data", (chunk) => {
			stderr += chunk.toString("utf8");
		});

		const code = await new Promise((resolve) => child.once("exit", resolve));

		assert.notEqual(code, 0);
		assert.match(stderr, /AO_WEB_PUBLIC_URL is required/);
	});
});

async function makeDist() {
	const dir = await mkdtemp(path.join(os.tmpdir(), "ao-web-dist-"));
	await writeFile(path.join(dir, "index.html"), '<!doctype html><div id="root"></div>\n');
	await writeFile(path.join(dir, "ao-web-build.json"), '{"frontendTree":"fixture"}\n');
	await mkdir(path.join(dir, "assets"));
	await writeFile(path.join(dir, "assets", "app.js"), "console.log('ao');\n");
	await writeFile(path.join(dir, "assets", "app-12345678.js"), "console.log('hashed ao');\n");
	cleanup.push(() => rm(dir, { recursive: true, force: true }));
	return dir;
}

async function listen(server) {
	await new Promise((resolve, reject) => {
		server.once("error", reject);
		server.listen(0, "127.0.0.1", resolve);
	});
	cleanup.push(() => new Promise((resolve) => server.close(resolve)));
	const address = server.address();
	assert(address && typeof address === "object");
	return {
		host: `127.0.0.1:${address.port}`,
		port: address.port,
		url: `http://127.0.0.1:${address.port}`,
	};
}

async function rawUpgrade({ port, host, origin, pathname }) {
	const socket = net.connect(port, "127.0.0.1");
	return new Promise((resolve, reject) => {
		let data = "";
		socket.on("connect", () => {
			socket.write(
				[
					`GET ${pathname} HTTP/1.1`,
					`Host: ${host}`,
					"Connection: Upgrade",
					"Upgrade: websocket",
					`Origin: ${origin}`,
					"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==",
					"Sec-WebSocket-Version: 13",
					"\r\n",
				].join("\r\n"),
			);
		});
		socket.on("data", (chunk) => {
			data += chunk.toString("utf8");
		});
		socket.on("end", () => resolve(data));
		socket.on("error", reject);
	});
}

async function rawHttp({ port, host, origin, pathname }) {
	const socket = net.connect(port, "127.0.0.1");
	return new Promise((resolve, reject) => {
		let data = "";
		socket.on("connect", () => {
			socket.write(
				[
					`GET ${pathname} HTTP/1.1`,
					`Host: ${host}`,
					`Origin: ${origin}`,
					"Connection: close",
					"\r\n",
				].join("\r\n"),
			);
		});
		socket.on("data", (chunk) => {
			data += chunk.toString("utf8");
		});
		socket.on("end", () => resolve(data));
		socket.on("error", reject);
	});
}

async function startReleaseSymlinkServer(distDir) {
	const releaseRoot = await mkdtemp(path.join(os.tmpdir(), "ao-web-release-"));
	const releaseDir = path.join(releaseRoot, "releases", "abc123");
	const releaseSource = path.join(releaseDir, "source");
	const current = path.join(releaseRoot, "current");
	await mkdir(releaseDir, { recursive: true });
	await symlink(REPO_ROOT, releaseSource, "dir");
	await symlink(releaseDir, current, "dir");
	cleanup.push(async () => {
		await rm(current, { force: true });
		await rm(releaseSource, { force: true });
		await rm(releaseRoot, { recursive: true, force: true });
	});

	const port = await freePort();
	const output = { stderr: "", stdout: "" };
	const child = spawn(process.execPath, [path.join(current, "source", "ops/ao-web-server.mjs")], {
		env: { ...process.env, AO_WEB_DIST: distDir, AO_WEB_PORT: String(port) },
		stdio: ["ignore", "pipe", "pipe"],
	});
	child.stdout.on("data", (chunk) => {
		output.stdout += chunk.toString("utf8");
	});
	child.stderr.on("data", (chunk) => {
		output.stderr += chunk.toString("utf8");
	});
	cleanup.push(() => stopChild(child));

	const url = `http://127.0.0.1:${port}/`;
	await waitForHttp(url, { child, output });
	return { child, output, url };
}

async function freePort() {
	const server = http.createServer();
	await new Promise((resolve, reject) => {
		server.once("error", reject);
		server.listen(0, "127.0.0.1", resolve);
	});
	const address = server.address();
	assert(address && typeof address === "object");
	const { port } = address;
	await new Promise((resolve) => server.close(resolve));
	return port;
}

async function waitForHttp(url, options = {}) {
	const deadline = Date.now() + (options.timeoutMs ?? 3000);
	let lastError = new Error("server did not start");
	while (Date.now() < deadline) {
		if (options.child && (options.child.exitCode !== null || options.child.signalCode !== null)) {
			throw new Error(
				`server process exited before serving ${url}: exit=${options.child.exitCode} signal=${options.child.signalCode}\nstdout:\n${options.output?.stdout ?? ""}\nstderr:\n${options.output?.stderr ?? ""}`,
			);
		}
		try {
			const response = await fetch(url);
			await response.arrayBuffer();
			return response;
		} catch (error) {
			lastError = error;
			await new Promise((resolve) => setTimeout(resolve, 50));
		}
	}
	throw lastError;
}

async function waitFor(predicate, timeoutMs = 1000) {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		if (predicate()) return;
		await new Promise((resolve) => setTimeout(resolve, 20));
	}
	assert.equal(predicate(), true);
}

function stopChild(child) {
	return new Promise((resolve, reject) => {
		if (child.exitCode !== null || child.signalCode !== null) {
			resolve();
			return;
		}

		let sigkillTimer;
		let failTimer;
		const done = () => {
			clearTimeout(sigkillTimer);
			clearTimeout(failTimer);
			resolve();
		};
		sigkillTimer = setTimeout(() => {
			if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
		}, 2000);
		failTimer = setTimeout(() => {
			child.off("exit", done);
			reject(new Error(`child process did not exit after SIGTERM/SIGKILL: pid=${child.pid}`));
		}, 3000);

		child.once("exit", done);
		child.kill("SIGTERM");
	});
}

async function fetchText(url) {
	const response = await fetch(url);
	return {
		body: await response.text(),
		headers: response.headers,
		status: response.status,
	};
}
