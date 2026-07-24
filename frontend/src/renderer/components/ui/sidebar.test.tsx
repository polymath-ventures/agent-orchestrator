import { render, screen } from "@testing-library/react";
import type * as React from "react";
import { describe, expect, it } from "vitest";
import { SidebarMenuButton, SidebarProvider } from "./sidebar";

describe("SidebarMenuButton", () => {
	function renderButton(props: React.ComponentProps<typeof SidebarMenuButton> = {}) {
		render(
			<SidebarProvider>
				<SidebarMenuButton data-testid="menu-button" {...props} />
			</SidebarProvider>,
		);
		return screen.getByTestId("menu-button");
	}

	// #127: this variant used to name `var(--sidebar-border)` / `var(--sidebar-accent)`,
	// upstream shadcn's bare token names. The fork remaps those to `--color-sidebar-*`
	// inside `@theme inline`, and `inline` means Tailwind resolves the mapping into the
	// utilities it generates rather than emitting a `:root` custom property — so neither
	// the bare name nor the `--color-` one resolves in a `var()`. A box-shadow whose
	// var() has no value and no fallback is invalid at computed-value time, so the whole
	// declaration is dropped and no ring renders at all. Hence the token utilities.
	it("draws the outline ring through the sidebar border token", () => {
		const button = renderButton({ variant: "outline" });

		// Geometry and colour are separate utilities: `shadow-[0_0_0_1px]` emits
		// `0 0 0 1px var(--tw-shadow-color, currentcolor)`, and the colour utility
		// fills that slot from the theme.
		expect(button).toHaveClass("shadow-[0_0_0_1px]", "shadow-sidebar-border", "hover:shadow-sidebar-accent");
	});

	it("leaves the default variant without a ring", () => {
		const button = renderButton();

		expect(button).not.toHaveClass("shadow-[0_0_0_1px]");
		expect(button).toHaveClass("hover:bg-sidebar-accent");
	});
});
