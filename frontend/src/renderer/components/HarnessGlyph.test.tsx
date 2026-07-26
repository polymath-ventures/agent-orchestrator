import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HarnessGlyph } from "./HarnessGlyph";

describe("HarnessGlyph", () => {
	// The chip lives inside row controls that carry their own aria-label. A
	// labelled control's descendants are dropped from its accessible name, so a
	// chip that labelled itself would simply never be announced. It is decorative
	// here; the row describes the harness (see Sidebar.test.tsx).
	it("is decorative, and names the harness on hover", () => {
		const { container } = render(<HarnessGlyph harness="claude-code" />);
		const chip = container.querySelector("[data-harness-glyph]");
		expect(chip).toHaveAttribute("aria-hidden", "true");
		expect(chip).toHaveAttribute("title", "Claude Code");
		expect(screen.queryByRole("img")).toBeNull();
	});

	it("paints the harness's brand tile", () => {
		const { container } = render(<HarnessGlyph harness="claude-code" />);
		expect(container.querySelector("[data-harness-glyph]")).toHaveStyle({ background: "#D97757" });
	});

	it("renders a knockout mark for a branded harness", () => {
		const { container } = render(<HarnessGlyph harness="codex" />);
		expect(container.querySelectorAll("path").length).toBeGreaterThan(0);
	});

	it("renders a monogram instead of a mark for an unbranded harness", () => {
		const { container } = render(<HarnessGlyph harness="aider" />);
		expect(container.querySelectorAll("path")).toHaveLength(0);
		expect(container.querySelector("[data-harness-glyph]")).toHaveTextContent("Ai");
	});

	it("renders an indicator for a harness it has never seen", () => {
		const { container } = render(<HarnessGlyph harness="brand-new-agent" />);
		const chip = container.querySelector("[data-harness-glyph]");
		expect(chip).toHaveTextContent("BN");
		expect(chip).toHaveAttribute("title", "Brand New Agent");
	});

	it("distinguishes a variant with a pip that does not depend on the row background", () => {
		const { container } = render(<HarnessGlyph harness="codex-fugu" />);
		const pip = container.querySelector("[data-harness-pip]");
		expect(pip).not.toBeNull();
		// The pip sits inside the chip, separated by the chip's own tile, so it
		// reads the same on a hovered or active row as on a resting one. An
		// outward ring would have to guess the row's current background.
		expect(pip?.className).not.toMatch(/ring-/);
		// It inherits the chip's aria-hidden rather than announcing itself.
		expect(pip?.closest("[data-harness-glyph]")).toHaveAttribute("aria-hidden", "true");
	});

	it("gives codex no pip", () => {
		const { container } = render(<HarnessGlyph harness="codex" />);
		expect(container.querySelector("[data-harness-pip]")).toBeNull();
	});
});
