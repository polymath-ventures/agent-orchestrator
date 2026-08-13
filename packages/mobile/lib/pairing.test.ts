import { describe, expect, it } from "vitest";
import type { ServerConfig } from "./config";
import { applyPairingPayload, parsePairingPayload } from "./pairing";

const cfg = (over: Partial<ServerConfig> = {}): ServerConfig => ({
	host: "",
	httpPort: "3011",
	muxPort: "14801",
	secure: false,
	password: "",
	...over,
});

describe("parsePairingPayload", () => {
	it("accepts the current password-bearing QR payload", () => {
		expect(parsePairingPayload(JSON.stringify({ v: 1, host: "100.64.1.2", port: 3011, password: "secret" }))).toEqual({
			host: "100.64.1.2",
			port: "3011",
			password: "secret",
			secure: false,
		});
	});

	it("reads secure pairing and defaults older payloads to plaintext", () => {
		expect(
			parsePairingPayload(JSON.stringify({ v: 1, host: "tailnet.example", port: 443, secure: true }))?.secure,
		).toBe(true);
		expect(parsePairingPayload(JSON.stringify({ v: 1, host: "100.64.1.2", port: "3011" }))?.secure).toBe(false);
	});

	it("rejects non-pairing payloads", () => {
		expect(parsePairingPayload("not json")).toBeNull();
		expect(parsePairingPayload(JSON.stringify({ v: 2, host: "100.64.1.2", port: 3011 }))).toBeNull();
		expect(parsePairingPayload(JSON.stringify({ v: 1, host: "", port: 3011 }))).toBeNull();
	});
});

describe("applyPairingPayload", () => {
	it("clears stale TLS while preserving a stored password for a plaintext scan", () => {
		const parsed = { host: "192.168.1.5", port: "3011", password: "", secure: false };
		expect(applyPairingPayload(cfg({ secure: true, password: "stored" }), parsed)).toMatchObject({
			host: "192.168.1.5",
			httpPort: "3011",
			password: "stored",
			secure: false,
		});
	});
});
