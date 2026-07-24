import { describe, expect, it, vi, beforeEach } from "vitest";
import { relaunchPrime } from "./relaunch-prime";
import { apiClient } from "./api-client";
import { captureRendererEvent } from "./telemetry";

vi.mock("./api-client", () => ({
	apiClient: { POST: vi.fn() },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => {
		if (typeof error === "object" && error !== null && "message" in error) {
			const body = error as { code?: unknown; message: unknown };
			const message = String(body.message);
			return typeof body.code === "string" && body.code !== "" ? `${message} (${body.code})` : message;
		}
		return fallback;
	},
}));

vi.mock("./telemetry", () => ({
	captureRendererEvent: vi.fn().mockResolvedValue(undefined),
}));

const captureMock = vi.mocked(captureRendererEvent);

describe("relaunchPrime", () => {
	beforeEach(() => vi.clearAllMocks());

	// Prime recovery must NOT go through the generic session spawn or restore
	// endpoints — the daemon forbids both for Prime, so a UI that called them
	// would surface a 403 instead of recovering.
	it("posts to the dedicated prime relaunch endpoint", async () => {
		(apiClient.POST as ReturnType<typeof vi.fn>).mockResolvedValue({
			data: { session: { id: "prime-2" } },
			error: undefined,
			response: { status: 200 },
		});

		const id = await relaunchPrime();

		expect(id).toBe("prime-2");
		expect(apiClient.POST).toHaveBeenCalledWith("/api/v1/prime/relaunch", {});
	});

	it("reports the daemon's message when the relaunch fails", async () => {
		(apiClient.POST as ReturnType<typeof vi.fn>).mockResolvedValue({
			data: undefined,
			error: { code: "PRIME_DISABLED", message: "Prime is disabled" },
			response: { status: 409 },
		});

		await expect(relaunchPrime()).rejects.toThrow("Prime is disabled (PRIME_DISABLED)");
		expect(captureMock).toHaveBeenCalledWith("ao.renderer.prime_relaunch_failed", {});
	});

	// A 200 with no session id means the daemon did not produce a Prime; the
	// caller must not be told the relaunch succeeded.
	it("treats a missing session id as a failure", async () => {
		(apiClient.POST as ReturnType<typeof vi.fn>).mockResolvedValue({
			data: { session: undefined },
			error: undefined,
			response: { status: 200 },
		});

		await expect(relaunchPrime()).rejects.toThrow(/Failed to relaunch Prime/);
	});

	it("emits the requested and succeeded telemetry on the happy path", async () => {
		(apiClient.POST as ReturnType<typeof vi.fn>).mockResolvedValue({
			data: { session: { id: "prime-3" } },
			error: undefined,
			response: { status: 200 },
		});

		await relaunchPrime();

		expect(captureMock).toHaveBeenCalledWith("ao.renderer.prime_relaunch_requested", {});
		expect(captureMock).toHaveBeenCalledWith("ao.renderer.prime_relaunch_succeeded", {});
	});
});
