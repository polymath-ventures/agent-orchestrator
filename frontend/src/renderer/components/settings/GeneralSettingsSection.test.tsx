/**
 * The General settings row that governs the sidebar quota/usage meter.
 *
 * The 2026-08-07 upstream sync dropped this row along with the meter's mount
 * (agent-orchestrator#280), leaving `isQuotaWidgetVisible` in the ui-store with
 * nothing able to change it. This renders the settings page the app actually
 * mounts, not the section in isolation, so the row has to survive the whole
 * path from page to switch.
 *
 * The meter's own mount guard lives in `test/shell-quota-widget-mount.test.tsx`.
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../../stores/ui-store";
import { GlobalSettingsForm } from "../GlobalSettingsForm";

vi.mock("../../hooks/useSettings", () => ({
	useSettings: () => ({ settings: { chatHarnesses: [], defaultSessionMode: "tui" }, isLoading: false, error: null }),
	useUpdateSessionInterface: () => ({ update: vi.fn(), saving: false, error: null }),
}));

vi.mock("./UpdatesSection", () => ({ UpdatesSection: () => null }));
vi.mock("./ReportProblemDialog", () => ({ ReportProblemDialog: () => null }));

beforeEach(() => {
	useUiStore.setState({ isQuotaWidgetVisible: true });
});

function renderGeneralSettings() {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={queryClient}>
			<GlobalSettingsForm section="general" />
		</QueryClientProvider>,
	);
}

describe("General settings — quota widget toggle", () => {
	it("reflects the persisted visibility flag", () => {
		useUiStore.setState({ isQuotaWidgetVisible: false });
		renderGeneralSettings();

		expect(screen.getByRole("switch", { name: "Show quota widget" })).not.toBeChecked();
	});

	it("turns the quota meter off and back on", async () => {
		const user = userEvent.setup();
		renderGeneralSettings();

		const toggle = screen.getByRole("switch", { name: "Show quota widget" });
		expect(toggle).toBeChecked();

		await user.click(toggle);
		expect(useUiStore.getState().isQuotaWidgetVisible).toBe(false);
		expect(toggle).not.toBeChecked();

		await user.click(toggle);
		expect(useUiStore.getState().isQuotaWidgetVisible).toBe(true);
		expect(toggle).toBeChecked();
	});
});
