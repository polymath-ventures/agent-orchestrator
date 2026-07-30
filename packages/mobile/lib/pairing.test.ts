import { expect, test } from "vitest";
import { parsePairingPayload } from "./pairing";

test("parsePairingPayload accepts the current password-bearing QR payload", () => {
	const parsed = parsePairingPayload(JSON.stringify({ v: 1, host: "100.64.1.2", port: 3011, password: "secret" }));
	expect(parsed).toEqual({ host: "100.64.1.2", port: "3011", password: "secret" });
});

test("parsePairingPayload preserves compatibility with host and port only payloads", () => {
	const parsed = parsePairingPayload(JSON.stringify({ v: 1, host: "100.64.1.2", port: "3011" }));
	expect(parsed).toEqual({ host: "100.64.1.2", port: "3011", password: "" });
});

test("parsePairingPayload rejects non-pairing payloads", () => {
	expect(parsePairingPayload("not json")).toBeNull();
	expect(parsePairingPayload(JSON.stringify({ v: 2, host: "100.64.1.2", port: 3011, password: "secret" }))).toBeNull();
	expect(parsePairingPayload(JSON.stringify({ v: 1, host: "", port: 3011, password: "secret" }))).toBeNull();
});
