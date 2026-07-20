import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DAEMON_SERVICE_NAME } from "../../shared/daemon-attach";
import type { DaemonStatus } from "../../shared/daemon-status";

const bridgeGetStatus = vi.fn<() => Promise<DaemonStatus>>();

vi.mock("./bridge", () => ({
	aoBridge: {
		daemon: {
			getStatus: bridgeGetStatus,
		},
	},
}));

describe("renderer daemon status", () => {
	beforeEach(() => {
		bridgeGetStatus.mockReset();
	});

	afterEach(async () => {
		vi.restoreAllMocks();
		vi.unstubAllGlobals();
		vi.resetModules();
		delete window.ao;
		const { setApiBaseUrl } = await import("./api-client");
		setApiBaseUrl("http://127.0.0.1:3001");
	});

	it("uses the Electron bridge when preload is available", async () => {
		window.ao = {} as NonNullable<typeof window.ao>;
		bridgeGetStatus.mockResolvedValue({ state: "ready", port: 3001, pid: 123 });
		const { readDaemonStatus } = await import("./daemon-status");

		await expect(readDaemonStatus()).resolves.toEqual({ state: "ready", port: 3001, pid: 123 });
		expect(bridgeGetStatus).toHaveBeenCalledTimes(1);
	});

	it("polls same-origin /healthz in browser mode", async () => {
		const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
			new Response(JSON.stringify({ status: "ok", service: DAEMON_SERVICE_NAME, pid: 42 }), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		vi.stubGlobal("fetch", fetchMock);
		const { setApiBaseUrl } = await import("./api-client");
		setApiBaseUrl("");
		const { readDaemonStatus } = await import("./daemon-status");

		await expect(readDaemonStatus()).resolves.toEqual({ state: "ready", pid: 42 });
		expect(fetchMock).toHaveBeenCalledWith("/healthz", { cache: "no-store" });
		expect(bridgeGetStatus).not.toHaveBeenCalled();
	});

	it("reports an invalid browser health payload as an identity mismatch", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ status: "ok", pid: 42 }), { status: 200 })),
		);
		const { setApiBaseUrl } = await import("./api-client");
		setApiBaseUrl("");
		const { readDaemonStatus } = await import("./daemon-status");

		await expect(readDaemonStatus()).resolves.toMatchObject({
			state: "error",
			code: "identity_mismatch",
		});
	});

	it("keeps the browser API base same-origin across ready and error status transitions", async () => {
		const { getApiBaseUrl, setApiBaseUrl } = await import("./api-client");
		const { applyDaemonStatus } = await import("./daemon-status");
		setApiBaseUrl(window.location.origin);

		applyDaemonStatus({ state: "ready", pid: 42 });
		expect(getApiBaseUrl()).toBe("");

		applyDaemonStatus({ state: "error", code: "daemon_unreachable", message: "offline" });
		expect(getApiBaseUrl()).toBe("");
	});
});
