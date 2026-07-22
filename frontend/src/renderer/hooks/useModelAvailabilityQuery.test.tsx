import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getMock } = vi.hoisted(() => ({ getMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock },
	apiErrorMessage: (error: unknown) => String(error),
}));

import {
	fetchModelAvailability,
	modelAvailabilityQueryKey,
	useModelAvailabilityQuery,
	useRefreshModelAvailability,
} from "./useModelAvailabilityQuery";

const firstResponse = { checkedAt: "2026-07-22T00:00:00Z", harnesses: [] };
const refreshedResponse = { checkedAt: "2026-07-22T01:00:00Z", harnesses: [] };

function makeWrapper(client: QueryClient) {
	return function Wrapper({ children }: { children: ReactNode }) {
		return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
	};
}

beforeEach(() => getMock.mockReset());

describe("useModelAvailabilityQuery", () => {
	it("loads the generated agents/models endpoint without forcing refresh", async () => {
		getMock.mockResolvedValue({ data: firstResponse, error: undefined });
		const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		const { result } = renderHook(() => useModelAvailabilityQuery(), { wrapper: makeWrapper(client) });

		await waitFor(() => expect(result.current.isSuccess).toBe(true));
		expect(result.current.data).toEqual(firstResponse);
		expect(getMock).toHaveBeenCalledWith("/api/v1/agents/models", { params: undefined });
	});

	it("force refreshes the endpoint and replaces the shared cached response", async () => {
		getMock
			.mockResolvedValueOnce({ data: firstResponse, error: undefined })
			.mockResolvedValueOnce({ data: refreshedResponse, error: undefined });
		const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		const wrapper = makeWrapper(client);
		const query = renderHook(() => useModelAvailabilityQuery(), { wrapper });
		await waitFor(() => expect(query.result.current.isSuccess).toBe(true));
		const refresh = renderHook(() => useRefreshModelAvailability(), { wrapper });

		await act(async () => {
			await refresh.result.current.refresh();
		});

		expect(getMock).toHaveBeenLastCalledWith("/api/v1/agents/models", { params: { query: { force: true } } });
		expect(client.getQueryData(modelAvailabilityQueryKey)).toEqual(refreshedResponse);
	});

	it("surfaces API errors from the ordinary catalog request", async () => {
		getMock.mockResolvedValue({ data: undefined, error: "catalog unavailable" });

		await expect(fetchModelAvailability()).rejects.toThrow("catalog unavailable");
	});

	it("retains cached catalog rows when a force refresh fails", async () => {
		getMock
			.mockResolvedValueOnce({ data: firstResponse, error: undefined })
			.mockResolvedValueOnce({ data: undefined, error: "refresh unavailable" });
		const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		const wrapper = makeWrapper(client);
		const query = renderHook(() => useModelAvailabilityQuery(), { wrapper });
		await waitFor(() => expect(query.result.current.isSuccess).toBe(true));
		const refresh = renderHook(() => useRefreshModelAvailability(), { wrapper });

		await expect(refresh.result.current.refresh()).rejects.toThrow("refresh unavailable");

		expect(client.getQueryData(modelAvailabilityQueryKey)).toEqual(firstResponse);
		expect(refresh.result.current.isRefreshing).toBe(false);
	});
});
