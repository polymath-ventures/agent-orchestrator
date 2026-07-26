import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HarnessGlyph } from "./HarnessGlyph";

describe("HarnessGlyph", () => {
	it("names the harness for assistive technology and on hover", () => {
		render(<HarnessGlyph harness="claude-code" />);
		const glyph = screen.getByRole("img", { name: "Claude Code" });
		expect(glyph).toHaveAttribute("title", "Claude Code");
	});

	it("paints the harness's brand tile", () => {
		render(<HarnessGlyph harness="claude-code" />);
		expect(screen.getByRole("img", { name: "Claude Code" })).toHaveStyle({ background: "#D97757" });
	});

	it("renders a knockout mark for a branded harness", () => {
		const { container } = render(<HarnessGlyph harness="codex" />);
		expect(container.querySelectorAll("path").length).toBeGreaterThan(0);
	});

	it("renders a monogram instead of a mark for an unbranded harness", () => {
		const { container } = render(<HarnessGlyph harness="aider" />);
		expect(container.querySelectorAll("path")).toHaveLength(0);
		expect(screen.getByRole("img", { name: "Aider" })).toHaveTextContent("Ai");
	});

	it("renders an indicator for a harness it has never seen", () => {
		render(<HarnessGlyph harness="brand-new-agent" />);
		expect(screen.getByRole("img", { name: "Brand New Agent" })).toHaveTextContent("BN");
	});

	it("marks the variant pip decorative so it is not announced separately", () => {
		const { container } = render(<HarnessGlyph harness="codex-fugu" />);
		expect(screen.getByRole("img", { name: "Codex Fugu" })).toBeInTheDocument();
		expect(container.querySelector("[data-harness-pip]")).toHaveAttribute("aria-hidden", "true");
	});

	it("gives codex no pip", () => {
		const { container } = render(<HarnessGlyph harness="codex" />);
		expect(container.querySelector("[data-harness-pip]")).toBeNull();
	});
});
