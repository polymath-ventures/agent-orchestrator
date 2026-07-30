import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PropsWithChildren } from "react";
import { apiClient } from "../lib/api-client";
import { shellTerminalsQueryKey, useOpenShellTerminal, type ShellTerminal } from "./useShellTerminals";

vi.mock("../lib/api-client", () => ({
	apiClient: { POST: vi.fn() },
	hasTrustedApiBaseUrl: () => true,
}));

function wrapper(client: QueryClient) {
	return ({ children }: PropsWithChildren) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("useOpenShellTerminal", () => {
	beforeEach(() => {
		vi.mocked(apiClient.POST).mockReset();
	});

	it("optimistically inserts the opened shell before per-call success handlers run", async () => {
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		const existing: ShellTerminal = {
			handleId: "shell-old",
			title: "old",
			workingDir: "/tmp/old",
			createdAt: "2026-07-25T00:00:00Z",
		};
		const opened: ShellTerminal = {
			handleId: "shell-new",
			title: "new",
			workingDir: "/tmp/new",
			createdAt: "2026-07-25T00:00:01Z",
		};
		queryClient.setQueryData<ShellTerminal[]>(shellTerminalsQueryKey, [existing]);
		vi.mocked(apiClient.POST).mockResolvedValue({ data: { shellTerminal: opened }, error: undefined });
		const { result } = renderHook(() => useOpenShellTerminal(), { wrapper: wrapper(queryClient) });
		const onSuccess = vi.fn(() => {
			expect(queryClient.getQueryData<ShellTerminal[]>(shellTerminalsQueryKey)).toEqual([existing, opened]);
		});

		await act(async () => {
			await result.current.mutateAsync({}, { onSuccess });
		});

		expect(onSuccess).toHaveBeenCalled();
	});
});
