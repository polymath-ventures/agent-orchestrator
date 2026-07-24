import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Skeleton } from "./skeleton";

describe("Skeleton", () => {
	// `accent` is a brand colour in this design system (--color-accent maps to
	// --bridge-accent, the refined blue), not the muted hover surface it is in
	// stock shadcn/ui. Filling a loading placeholder with it renders solid blue
	// bars wherever content is pending.
	it("fills with a muted surface, not the brand accent", () => {
		render(<Skeleton data-testid="skeleton" />);

		const skeleton = screen.getByTestId("skeleton");

		expect(skeleton).not.toHaveClass("bg-accent");
		expect(skeleton).toHaveClass("bg-muted");
	});

	it("keeps the pulse animation and rounding", () => {
		render(<Skeleton data-testid="skeleton" />);

		expect(screen.getByTestId("skeleton")).toHaveClass("animate-pulse", "rounded-md");
	});

	it("merges caller classes over the defaults", () => {
		render(<Skeleton data-testid="skeleton" className="h-4 w-40" />);

		expect(screen.getByTestId("skeleton")).toHaveClass("h-4", "w-40", "bg-muted");
	});
});
