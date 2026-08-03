import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

export const PERMISSION_MODE_OPTIONS = [
	{ value: "default", label: "Default" },
	{ value: "accept-edits", label: "Accept edits" },
	{ value: "auto", label: "Auto" },
	{ value: "bypass-permissions", label: "Bypass permissions" },
] as const;

const DEFAULT_VALUE = "__default__";

export function PermissionModeSelect({
	ariaLabel,
	className,
	defaultLabel,
	disabled = false,
	id,
	onChange,
	size = "default",
	value,
}: {
	ariaLabel?: string;
	className?: string;
	defaultLabel: string;
	disabled?: boolean;
	id?: string;
	onChange: (value: string) => void;
	size?: "sm" | "default";
	value: string;
}) {
	return (
		<Select
			value={value || DEFAULT_VALUE}
			onValueChange={(next) => onChange(next === DEFAULT_VALUE ? "" : next)}
			disabled={disabled}
		>
			<SelectTrigger id={id} size={size} className={className} aria-label={ariaLabel}>
				<SelectValue />
			</SelectTrigger>
			<SelectContent>
				<SelectItem value={DEFAULT_VALUE}>{defaultLabel}</SelectItem>
				{PERMISSION_MODE_OPTIONS.map((option) => (
					<SelectItem key={option.value} value={option.value}>
						{option.label}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}
