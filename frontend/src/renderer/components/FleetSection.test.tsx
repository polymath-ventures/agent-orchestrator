import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FleetSection } from "./FleetSection";

const { getMock, postMock, useWorkspaceQuery } = vi.hoisted(() => ({
	getMock: vi.fn(),
	postMock: vi.fn(),
	useWorkspaceQuery: vi.fn(() => ({
		data: [] as Array<{ id: string; pauseState?: string; drainingWorkers?: number }>,
	})),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, POST: postMock },
	apiErrorMessage: (e: unknown, fb = "Request failed") =>
		e instanceof Error ? e.message : ((e as { message?: string })?.message ?? fb),
}));
vi.mock("../hooks/useWorkspaceQuery", () => ({ useWorkspaceQuery, workspaceQueryKey: ["workspaces"] }));

function renderSection() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(
		<QueryClientProvider client={client}>
			<FleetSection />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	vi.clearAllMocks();
	useWorkspaceQuery.mockReturnValue({ data: [] });
	postMock.mockResolvedValue({ error: undefined });
});
afterEach(() => vi.restoreAllMocks());

describe("FleetSection", () => {
	it("shows Running and pauses the fleet", async () => {
		getMock.mockResolvedValue({ data: { paused: false }, error: undefined });
		renderSection();

		await waitFor(() => expect(screen.getByText("Running")).toBeInTheDocument());
		await userEvent.click(screen.getByRole("button", { name: "Pause" }));
		await waitFor(() => expect(postMock).toHaveBeenCalledWith("/api/v1/fleet/pause"));
	});

	it("shows Draining (N) aggregated across draining projects while paused", async () => {
		getMock.mockResolvedValue({ data: { paused: true }, error: undefined });
		useWorkspaceQuery.mockReturnValue({
			data: [
				{ id: "a", pauseState: "draining", drainingWorkers: 2 },
				{ id: "b", pauseState: "draining", drainingWorkers: 1 },
				{ id: "c", pauseState: "paused", drainingWorkers: 0 },
			],
		});
		renderSection();

		await waitFor(() => expect(screen.getByText("Draining (3)")).toBeInTheDocument());
		expect(screen.queryByText("Paused")).not.toBeInTheDocument();
	});

	it("keeps the hard-pause control available while paused so a drain can be escalated", async () => {
		getMock.mockResolvedValue({ data: { paused: true }, error: undefined });
		renderSection();

		await waitFor(() => expect(screen.getByRole("button", { name: "Resume" })).toBeInTheDocument());
		// The emergency hard-pause is offered alongside Resume, so an operator can
		// escalate an in-progress drain without resuming first.
		await userEvent.click(screen.getByRole("button", { name: "Pause now (hard)" }));
		await userEvent.click(screen.getByRole("button", { name: "Pause now" }));
		await waitFor(() =>
			expect(postMock).toHaveBeenCalledWith("/api/v1/fleet/pause", { params: { query: { hard: true } } }),
		);
	});

	it("states the true blast radius (orchestrators too) in the hard-pause confirmation", async () => {
		getMock.mockResolvedValue({ data: { paused: false }, error: undefined });
		renderSection();

		await waitFor(() => expect(screen.getByText("Running")).toBeInTheDocument());
		await userEvent.click(screen.getByRole("button", { name: "Pause now (hard)" }));
		expect(within(screen.getByRole("dialog")).getByText(/orchestrator/i)).toBeInTheDocument();
	});

	it("renders an unavailable state (not Running) when status can't be loaded", async () => {
		getMock.mockRejectedValue(new Error("daemon down"));
		renderSection();

		await waitFor(() => expect(screen.getByText(/Unknown/i)).toBeInTheDocument());
		expect(screen.queryByText("Running")).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Pause" })).toBeDisabled();
		expect(screen.getByRole("button", { name: "Pause now (hard)" })).toBeDisabled();
	});
});
