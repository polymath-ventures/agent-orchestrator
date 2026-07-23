import { Gauge, Monitor, Moon, Palette, Smartphone, Sun } from "lucide-react";
import type { ThemePreference } from "../../lib/theme";
import { useUiStore } from "../../stores/ui-store";
import { Switch } from "../ui/switch";
import { SettingsOptionMenu, type SettingsOption } from "./SettingsOptionMenu";
import { SettingsLinkRow, SettingsRow } from "./SettingsRow";
import { SettingsSection } from "./SettingsSection";

const THEME_OPTIONS = [
	{ value: "light", label: "Light", icon: <Sun className="size-icon-lg" aria-hidden="true" /> },
	{ value: "dark", label: "Dark", icon: <Moon className="size-icon-lg" aria-hidden="true" /> },
	{ value: "system", label: "System", icon: <Monitor className="size-icon-lg" aria-hidden="true" /> },
] satisfies SettingsOption<ThemePreference>[];

export function GeneralSettingsSection({ onConnectMobile }: { onConnectMobile: () => void }) {
	const themePreference = useUiStore((state) => state.themePreference);
	const setThemePreference = useUiStore((state) => state.setThemePreference);
	const isQuotaWidgetVisible = useUiStore((state) => state.isQuotaWidgetVisible);
	const setQuotaWidgetVisible = useUiStore((state) => state.setQuotaWidgetVisible);

	return (
		<SettingsSection title="General">
			<SettingsRow icon={Palette} label="Theme">
				<SettingsOptionMenu
					aria-label="Theme"
					value={themePreference}
					options={THEME_OPTIONS}
					onChange={setThemePreference}
				/>
			</SettingsRow>
			<SettingsRow icon={Gauge} label="Show quota widget">
				<Switch aria-label="Show quota widget" checked={isQuotaWidgetVisible} onCheckedChange={setQuotaWidgetVisible} />
			</SettingsRow>
			<SettingsLinkRow icon={Smartphone} label="Connect Mobile" onClick={onConnectMobile} />
		</SettingsSection>
	);
}
