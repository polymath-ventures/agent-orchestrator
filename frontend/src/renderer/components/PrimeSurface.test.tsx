import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const navigate = vi.fn();
const invalidateQueries = vi.fn().mockResolvedValue(undefined);
const relaunchPrimeMock = vi.fn();
const workspaceState: { data: unknown; isLoading: boolean } = { data: [], isLoading: false };
const primeEnabledState: { data: boolean | undefined; isLoading: boolean } = { data: true, isLoading: false };

vi.mock("@tanstack/react-router", async (importOriginal) => ({
	...(await importOriginal<typeof import("@tanstack/react-router")>()),
	useNavigate: () => navigate,
}));

vi.mock("@tanstack/react-query", () => ({
	useQueryClient: () => ({ invalidateQueries }),
}));

vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: () => workspaceState,
	workspaceQueryKey: ["workspaces"],
}));

vi.mock("../hooks/usePrimeSettingsQuery", () => ({
	usePrimeEnabledQuery: () => primeEnabledState,
	primeSettingsQueryKey: ["prime", "settings"],
}));

vi.mock("../lib/relaunch-prime", () => ({
	relaunchPrime: (...args: unknown[]) => relaunchPrimeMock(...args),
}));

const { PrimeBoard: PrimeRoute } = await import("./PrimeBoard");

const primeSession = {
	id: "ao-prime",
	title: "Prime",
	kind: "prime",
	status: "working",
	workspaceId: "fleet",
	workspaceName: "AO Fleet",
};

function workspacesWith(sessions: unknown[]) {
	return [{ id: "fleet", name: "AO Fleet", path: "", sessions }];
}

describe("Prime surface", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		workspaceState.data = [];
		workspaceState.isLoading = false;
		primeEnabledState.data = true;
		primeEnabledState.isLoading = false;
	});

	// The regression this whole change exists to prevent: Prime enabled, no live
	// session, and the operator has somewhere to go and something to press.
	it("explains the not-running state and offers Relaunch Prime", async () => {
		render(<PrimeRoute />);

		expect(await screen.findByTestId("prime-not-running")).toBeInTheDocument();
		expect(screen.getByText(/Prime is enabled but not running/i)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Relaunch Prime/i })).toBeInTheDocument();
	});

	it("relaunches Prime and refreshes workspace state", async () => {
		relaunchPrimeMock.mockResolvedValue("ao-prime-2");
		const user = userEvent.setup();
		render(<PrimeRoute />);

		await user.click(await screen.findByRole("button", { name: /Relaunch Prime/i }));

		await waitFor(() => expect(relaunchPrimeMock).toHaveBeenCalledTimes(1));
		expect(invalidateQueries).toHaveBeenCalled();
	});

	it("surfaces a relaunch failure instead of failing silently", async () => {
		relaunchPrimeMock.mockRejectedValue(new Error("Prime is disabled"));
		const user = userEvent.setup();
		render(<PrimeRoute />);

		await user.click(await screen.findByRole("button", { name: /Relaunch Prime/i }));

		expect(await screen.findByText("Prime is disabled")).toBeInTheDocument();
	});

	// A live Prime has a real terminal; the route hands off rather than showing
	// a recovery surface over a healthy session.
	it("navigates to the live Prime terminal when one is running", async () => {
		workspaceState.data = workspacesWith([primeSession]);
		render(<PrimeRoute />);

		await waitFor(() =>
			expect(navigate).toHaveBeenCalledWith({
				to: "/sessions/$sessionId",
				params: { sessionId: "ao-prime" },
				replace: true,
			}),
		);
	});

	it("says Prime is disabled when settings say so", async () => {
		primeEnabledState.data = false;
		render(<PrimeRoute />);

		expect(await screen.findByTestId("prime-disabled")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: /Relaunch Prime/i })).not.toBeInTheDocument();
	});
});
