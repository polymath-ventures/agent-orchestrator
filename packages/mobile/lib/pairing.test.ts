import assert from "node:assert/strict";
import test from "node:test";
import { parsePairingPayload } from "./pairing";

test("parsePairingPayload accepts the current password-bearing QR payload", () => {
	const parsed = parsePairingPayload(JSON.stringify({ v: 1, host: "100.64.1.2", port: 3011, password: "secret" }));
	assert.deepEqual(parsed, { host: "100.64.1.2", port: "3011", password: "secret" });
});

test("parsePairingPayload preserves compatibility with host and port only payloads", () => {
	const parsed = parsePairingPayload(JSON.stringify({ v: 1, host: "100.64.1.2", port: "3011" }));
	assert.deepEqual(parsed, { host: "100.64.1.2", port: "3011", password: "" });
});

test("parsePairingPayload rejects non-pairing payloads", () => {
	assert.equal(parsePairingPayload("not json"), null);
	assert.equal(parsePairingPayload(JSON.stringify({ v: 2, host: "100.64.1.2", port: 3011, password: "secret" })), null);
	assert.equal(parsePairingPayload(JSON.stringify({ v: 1, host: "", port: 3011, password: "secret" })), null);
});
