import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { XtermTerminal } from "./XtermTerminal";

const state = vi.hoisted(() => ({
	linkHandler: null as null | ((event: MouseEvent, uri: string) => void),
	lastTerminal: null as null | {
		keyHandler?: (event: KeyboardEvent) => boolean;
		wheelHandler?: (event: WheelEvent) => boolean;
		selection: string;
		options: Record<string, unknown>;
		modes: { bracketedPasteMode: boolean; mouseTrackingMode: string };
		buffer: { active: { type: string } };
		scrollLines: ReturnType<typeof vi.fn>;
		clear: ReturnType<typeof vi.fn>;
		focus: ReturnType<typeof vi.fn>;
		selectAll: ReturnType<typeof vi.fn>;
		dataListeners: Set<(data: string) => void>;
		keyListeners: Set<(event: { key: string }) => void>;
		selectionListeners: Set<() => void>;
		_core: {
			element: { classList: { add: ReturnType<typeof vi.fn>; remove: ReturnType<typeof vi.fn> } };
			_selectionService: {
				enable: ReturnType<typeof vi.fn>;
				shouldForceSelection: (event: MouseEvent) => boolean;
			};
		};
	},
}));

vi.mock("@xterm/xterm", () => ({
	Terminal: class FakeTerminal {
		options: Record<string, unknown>;
		cols = 80;
		rows = 24;
		selection = "";
		keyHandler?: (event: KeyboardEvent) => boolean;
		wheelHandler?: (event: WheelEvent) => boolean;
		modes = { bracketedPasteMode: false, mouseTrackingMode: "vt200" };
		buffer = { active: { type: "normal" } };
		scrollLines = vi.fn();
		clear = vi.fn();
		// Mirrors xterm: focus() moves DOM focus to the hidden helper textarea, so
		// tests can assert on document.activeElement the way the user experiences
		// it rather than only on the spy.
		focus = vi.fn(() => {
			this.helperTextarea?.focus();
		});
		selectAll = vi.fn();
		helperTextarea: HTMLTextAreaElement | null = null;
		dataListeners = new Set<(data: string) => void>();
		keyListeners = new Set<(event: { key: string }) => void>();
		selectionListeners = new Set<() => void>();
		_core = {
			element: { classList: { add: vi.fn(), remove: vi.fn() } },
			_selectionService: {
				enable: vi.fn(),
				shouldForceSelection: () => false,
			},
		};

		constructor(options: Record<string, unknown>) {
			this.options = options;
			state.lastTerminal = this;
		}

		loadAddon() {}
		open(host: HTMLElement) {
			this.helperTextarea = host.appendChild(document.createElement("textarea"));
		}
		get textarea() {
			return this.helperTextarea ?? undefined;
		}
		write() {}
		writeln() {}
		dispose() {}
		onData(listener: (data: string) => void) {
			this.dataListeners.add(listener);
			return { dispose: () => this.dataListeners.delete(listener) };
		}
		onResize() {
			return { dispose: () => undefined };
		}
		onRender() {
			return { dispose: () => undefined };
		}
		onKey(listener: (event: { key: string }) => void) {
			this.keyListeners.add(listener);
			return { dispose: () => this.keyListeners.delete(listener) };
		}
		onSelectionChange(listener: () => void) {
			this.selectionListeners.add(listener);
			return { dispose: () => this.selectionListeners.delete(listener) };
		}
		hasSelection() {
			return this.selection.length > 0;
		}
		getSelection() {
			return this.selection;
		}
		attachCustomKeyEventHandler(listener: (event: KeyboardEvent) => boolean) {
			this.keyHandler = listener;
		}
		attachCustomWheelEventHandler(listener: (event: WheelEvent) => boolean) {
			this.wheelHandler = listener;
		}
		unicode = { activeVersion: "" };
	},
}));

vi.mock("@xterm/addon-fit", () => ({
	FitAddon: class FakeFitAddon {
		fit() {}
	},
}));

vi.mock("@xterm/addon-search", () => ({
	SearchAddon: class FakeSearchAddon {},
}));

vi.mock("@xterm/addon-unicode11", () => ({
	Unicode11Addon: class FakeUnicode11Addon {},
}));

vi.mock("@xterm/addon-web-links", () => ({
	WebLinksAddon: class FakeWebLinksAddon {
		constructor(handler?: (event: MouseEvent, uri: string) => void) {
			state.linkHandler = handler ?? null;
		}
	},
}));

vi.mock("@xterm/addon-canvas", () => ({
	CanvasAddon: class FakeCanvasAddon {},
}));

vi.mock("@xterm/addon-webgl", () => ({
	WebglAddon: class FakeWebglAddon {
		onContextLoss() {}
		dispose() {}
	},
}));

function setNavigatorPlatform(platform: string) {
	Object.defineProperty(window.navigator, "platform", {
		configurable: true,
		value: platform,
	});
	Object.defineProperty(window.navigator, "userAgentData", {
		configurable: true,
		value: { platform },
	});
}

describe("XtermTerminal", () => {
	beforeEach(() => {
		state.lastTerminal = null;
		state.linkHandler = null;
		setNavigatorPlatform("Linux x86_64");
		window.ao!.clipboard.writeText = vi.fn().mockResolvedValue(undefined);
		window.ao!.clipboard.readText = vi.fn().mockResolvedValue("");
	});

	// An autoFocus mount is the owner saying "the user just switched to this
	// terminal", so keystrokes must reach the shell straight away. Without it the
	// terminals screen renders attached but deaf until a second click.
	it("takes keyboard focus when an autoFocus pane mounts", () => {
		const { container } = render(<XtermTerminal autoFocus theme="dark" />);

		expect(document.activeElement).toBe(container.querySelector("textarea"));
	});

	// Session panes mount for reasons that are not a user switching to them —
	// behind a pop-out overlay, or re-keyed by a background poll assigning a
	// terminal handle. They must not move focus.
	it("leaves focus alone when the owner did not ask for autoFocus", () => {
		const sidebarButton = document.body.appendChild(document.createElement("button"));
		sidebarButton.focus();

		render(<XtermTerminal theme="dark" />);

		expect(document.activeElement).toBe(sidebarButton);
		sidebarButton.remove();
	});

	// Selecting another shell tab remounts the pane (TerminalPane keys mounts by
	// terminal handle) while focus sits on the tab button that was just clicked.
	it("takes focus from the control that mounted it, such as a shell tab button", () => {
		const tab = document.body.appendChild(document.createElement("button"));
		tab.focus();

		const { container } = render(<XtermTerminal autoFocus theme="dark" />);

		expect(document.activeElement).toBe(container.querySelector("textarea"));
		tab.remove();
	});

	it("focuses an already-mounted terminal when the owner sends a focus request", () => {
		const tab = document.body.appendChild(document.createElement("button"));
		const { container, rerender } = render(<XtermTerminal focusRequest={0} theme="dark" />);
		tab.focus();

		rerender(<XtermTerminal focusRequest={1} theme="dark" />);

		expect(document.activeElement).toBe(container.querySelector("textarea"));
		tab.remove();
	});

	it("keeps focus on a text field that is not inside an overlay", () => {
		const filter = document.body.appendChild(document.createElement("input"));
		filter.focus();

		render(<XtermTerminal autoFocus theme="dark" />);

		expect(document.activeElement).toBe(filter);
		filter.remove();
	});

	// The shared open-overlay predicate covers menus too, so a mount underneath an
	// open dropdown does not yank the keyboard out of it.
	it.each([
		["dialog", "dialog"],
		["alertdialog", "alertdialog"],
		["menu", "menu"],
	])("keeps focus inside an open %s overlay", (_label, role) => {
		const overlay = document.createElement("div");
		overlay.setAttribute("role", role);
		overlay.setAttribute("data-state", "open");
		const item = overlay.appendChild(document.createElement("button"));
		document.body.appendChild(overlay);
		item.focus();

		render(<XtermTerminal autoFocus theme="dark" />);

		expect(document.activeElement).toBe(item);
		overlay.remove();
	});

	// The mount effect runs once, so yielding to an overlay that merely exists
	// would leave the pane deaf for good once that overlay closed.
	it("still focuses when an overlay is open but holds no keyboard focus", () => {
		const overlay = document.createElement("div");
		overlay.setAttribute("role", "menu");
		overlay.setAttribute("data-state", "open");
		document.body.appendChild(overlay);

		const { container } = render(<XtermTerminal autoFocus theme="dark" />);

		expect(document.activeElement).toBe(container.querySelector("textarea"));
		overlay.remove();
	});

	it("preserves the agent TUI palette without contrast remapping", () => {
		render(<XtermTerminal theme="dark" />);

		expect(state.lastTerminal!.options.drawBoldTextInBrightColors).toBe(true);
		expect(state.lastTerminal!.options.minimumContrastRatio).toBe(1);
	});

	it("copies selected terminal text on the terminal copy shortcut", () => {
		render(<XtermTerminal theme="dark" />);
		state.lastTerminal!.selection = "copied selection";

		const event = {
			key: "c",
			metaKey: true,
			ctrlKey: false,
			shiftKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);

		expect(allowed).toBe(false);
		expect(event.preventDefault).toHaveBeenCalled();
		expect(window.ao!.clipboard.writeText).toHaveBeenCalledWith("copied selection");
	});

	it("leaves bare Escape for the terminal but exits focus on Ctrl+F6", () => {
		const onExitFocus = vi.fn();
		render(<XtermTerminal onExitFocus={onExitFocus} theme="dark" />);
		const escape = {
			key: "Escape",
			ctrlKey: false,
			metaKey: false,
			altKey: false,
			shiftKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const exit = {
			key: "F6",
			ctrlKey: true,
			metaKey: false,
			altKey: false,
			shiftKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;

		expect(state.lastTerminal!.keyHandler!(escape)).toBe(true);
		expect(onExitFocus).not.toHaveBeenCalled();
		expect(state.lastTerminal!.keyHandler!(exit)).toBe(false);
		expect(onExitFocus).toHaveBeenCalledTimes(1);
		expect(exit.preventDefault).toHaveBeenCalled();
	});

	it("leaves Ctrl+F6 for the terminal when there is no focus exit target", () => {
		render(<XtermTerminal theme="dark" />);
		const exit = {
			key: "F6",
			ctrlKey: true,
			metaKey: false,
			altKey: false,
			shiftKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;

		expect(state.lastTerminal!.keyHandler!(exit)).toBe(true);
		expect(exit.preventDefault).not.toHaveBeenCalled();
	});

	it("only advertises the focus-exit shortcut when a focus exit target exists", () => {
		const { container, rerender } = render(<XtermTerminal ariaLabel="Agent terminal" theme="dark" />);
		const host = container.firstElementChild;
		const textarea = () => container.querySelector("textarea");

		expect(host).toHaveAttribute("aria-label", "Agent terminal");
		expect(textarea()).toHaveAttribute("aria-label", "Agent terminal");
		expect(screen.queryByLabelText(/press Ctrl\+F6/)).not.toBeInTheDocument();

		rerender(<XtermTerminal ariaLabel="Agent terminal" onExitFocus={() => undefined} theme="dark" />);

		expect(host).toHaveAttribute("aria-label", "Agent terminal; press Ctrl+F6 to move focus out");
		expect(textarea()).toHaveAttribute("aria-label", "Agent terminal; press Ctrl+F6 to move focus out");
	});

	it("handles native copy events from inside the terminal", () => {
		const { container } = render(<XtermTerminal theme="dark" />);
		state.lastTerminal!.selection = "native copied selection";
		const setData = vi.fn();
		const event = new Event("copy", { bubbles: true, cancelable: true }) as ClipboardEvent;
		Object.defineProperty(event, "clipboardData", {
			value: { setData },
		});

		container.firstElementChild!.dispatchEvent(event);

		expect(event.defaultPrevented).toBe(true);
		expect(setData).toHaveBeenCalledWith("text/plain", "native copied selection");
		expect(window.ao!.clipboard.writeText).toHaveBeenCalledWith("native copied selection");
	});

	it("copies from the focused xterm textarea when the window receives the copy shortcut", () => {
		const { container } = render(<XtermTerminal theme="dark" />);
		state.lastTerminal!.selection = "focused copied selection";
		container.querySelector("textarea")!.focus();

		const event = new KeyboardEvent("keydown", {
			bubbles: true,
			cancelable: true,
			key: "c",
			metaKey: true,
		});
		window.dispatchEvent(event);

		expect(event.defaultPrevented).toBe(true);
		expect(window.ao!.clipboard.writeText).toHaveBeenCalledWith("focused copied selection");
	});

	it("opens a themed context menu on right-click and disables Copy without a selection", async () => {
		const { container } = render(<XtermTerminal theme="dark" />);
		const host = container.firstElementChild!;

		expect(fireEvent.contextMenu(host, { clientX: 120, clientY: 88 })).toBe(false);

		expect(await screen.findByText("Paste")).toBeInTheDocument();
		expect(screen.getByText("Copy")).toHaveAttribute("data-disabled");
		const trigger = container.querySelector("button[aria-hidden='true']") as HTMLButtonElement;
		expect(trigger.style.left).toBe("120px");
		expect(trigger.style.top).toBe("88px");
	});

	it("runs context menu copy, select all, and clear against the xterm instance", async () => {
		const { container } = render(<XtermTerminal theme="dark" />);
		const host = container.firstElementChild!;
		state.lastTerminal!.selection = "menu copy";

		fireEvent.contextMenu(host);
		fireEvent.click(await screen.findByText("Copy"));
		await waitFor(() => expect(window.ao!.clipboard.writeText).toHaveBeenCalledWith("menu copy"));
		expect(state.lastTerminal!.focus).toHaveBeenCalled();

		fireEvent.contextMenu(host);
		fireEvent.click(await screen.findByText("Select All"));
		expect(state.lastTerminal!.selectAll).toHaveBeenCalled();

		fireEvent.contextMenu(host);
		fireEvent.click(await screen.findByText("Clear"));
		expect(state.lastTerminal!.clear).toHaveBeenCalled();
	});

	it("pastes from the context menu through the terminal paste path", async () => {
		const onInput = vi.fn();
		window.ao!.clipboard.readText = vi.fn().mockResolvedValue("menu\npaste");
		const { container } = render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		fireEvent.contextMenu(container.firstElementChild!);
		fireEvent.click(await screen.findByText("Paste"));

		await waitFor(() => expect(onInput).toHaveBeenCalledWith("menu\rpaste", "paste"));
		expect(window.ao!.clipboard.readText).toHaveBeenCalledTimes(1);
	});

	it("honors bracketed paste mode from the context menu", async () => {
		const onInput = vi.fn();
		window.ao!.clipboard.readText = vi.fn().mockResolvedValue("bracketed\npaste");
		const { container } = render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);
		state.lastTerminal!.modes.bracketedPasteMode = true;

		fireEvent.contextMenu(container.firstElementChild!);
		fireEvent.click(await screen.findByText("Paste"));

		await waitFor(() => expect(onInput).toHaveBeenCalledWith("\x1b[200~bracketed\rpaste\x1b[201~", "paste"));
	});

	it("auto-copies new selections and retries explicit copy if the auto-copy failed", async () => {
		render(<XtermTerminal theme="dark" />);
		const writeText = vi.fn().mockRejectedValueOnce(new Error("clipboard failed")).mockResolvedValueOnce(undefined);
		window.ao!.clipboard.writeText = writeText;

		state.lastTerminal!.selection = "retry me";
		state.lastTerminal!.selectionListeners.forEach((listener) => listener());
		await new Promise((resolve) => window.setTimeout(resolve, 0));

		const event = {
			key: "c",
			metaKey: true,
			ctrlKey: false,
			shiftKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);

		expect(allowed).toBe(false);
		expect(writeText).toHaveBeenCalledTimes(2);
		expect(writeText).toHaveBeenLastCalledWith("retry me");
	});

	it("leaves plain Ctrl+C as terminal input on non-Windows even when text is selected", () => {
		render(<XtermTerminal theme="dark" />);
		state.lastTerminal!.selection = "selected text";

		const event = {
			key: "c",
			metaKey: false,
			ctrlKey: true,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);

		expect(allowed).toBe(true);
		expect(event.preventDefault).not.toHaveBeenCalled();
		expect(event.stopPropagation).not.toHaveBeenCalled();
		expect(window.ao!.clipboard.writeText).not.toHaveBeenCalled();
	});

	it("copies selected text with plain Ctrl+C on Windows", () => {
		setNavigatorPlatform("Win32");
		render(<XtermTerminal theme="dark" />);
		state.lastTerminal!.selection = "windows copy";

		const event = {
			key: "c",
			metaKey: false,
			ctrlKey: true,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);

		expect(allowed).toBe(false);
		expect(event.preventDefault).toHaveBeenCalled();
		expect(event.stopPropagation).toHaveBeenCalled();
		expect(window.ao!.clipboard.writeText).toHaveBeenCalledWith("windows copy");
	});

	it("leaves plain Ctrl+C as terminal input on Windows when nothing is selected", () => {
		setNavigatorPlatform("Win32");
		render(<XtermTerminal theme="dark" />);
		state.lastTerminal!.selection = "";

		const event = {
			key: "c",
			metaKey: false,
			ctrlKey: true,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);

		expect(allowed).toBe(true);
		expect(event.preventDefault).not.toHaveBeenCalled();
		expect(event.stopPropagation).not.toHaveBeenCalled();
		expect(window.ao!.clipboard.writeText).not.toHaveBeenCalled();
	});

	it.each(["Linux x86_64", "Win32"])(
		"pastes once from the Electron clipboard on Ctrl+Shift+V for %s",
		async (platform) => {
			setNavigatorPlatform(platform);
			const onInput = vi.fn();
			window.ao!.clipboard.readText = vi.fn().mockResolvedValue("hello\nworld");
			const { container } = render(
				<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />,
			);

			const event = {
				key: "v",
				metaKey: false,
				ctrlKey: true,
				shiftKey: true,
				altKey: false,
				preventDefault: vi.fn(),
				stopPropagation: vi.fn(),
			} as unknown as KeyboardEvent;
			const allowed = state.lastTerminal!.keyHandler!(event);
			const pasteEvent = new Event("paste", { bubbles: true, cancelable: true }) as ClipboardEvent;
			Object.defineProperty(pasteEvent, "clipboardData", {
				value: { getData: vi.fn().mockReturnValue("native paste") },
			});
			container.firstElementChild!.dispatchEvent(pasteEvent);
			await Promise.resolve();

			expect(allowed).toBe(false);
			expect(event.preventDefault).toHaveBeenCalled();
			expect(event.stopPropagation).toHaveBeenCalled();
			expect(window.ao!.clipboard.readText).toHaveBeenCalledTimes(1);
			expect(pasteEvent.defaultPrevented).toBe(true);
			expect(onInput).toHaveBeenCalledTimes(1);
			expect(onInput).toHaveBeenCalledWith("hello\rworld", "paste");
		},
	);

	it("supports plain Ctrl+V paste on Windows", async () => {
		setNavigatorPlatform("Win32");
		const onInput = vi.fn();
		window.ao!.clipboard.readText = vi.fn().mockResolvedValue("windows paste");
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		const event = {
			key: "v",
			metaKey: false,
			ctrlKey: true,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);
		await Promise.resolve();

		expect(allowed).toBe(false);
		expect(event.preventDefault).toHaveBeenCalled();
		expect(event.stopPropagation).toHaveBeenCalled();
		expect(window.ao!.clipboard.readText).toHaveBeenCalled();
		expect(onInput).toHaveBeenCalledWith("windows paste", "paste");
	});

	it("suppresses a queued native paste event after a handled paste shortcut", async () => {
		const onInput = vi.fn();
		window.ao!.clipboard.readText = vi.fn().mockResolvedValue("shortcut paste");
		const { container } = render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		const event = {
			key: "v",
			metaKey: false,
			ctrlKey: true,
			shiftKey: true,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(event)).toBe(false);
		await new Promise((resolve) => window.setTimeout(resolve, 0));

		const pasteEvent = new Event("paste", { bubbles: true, cancelable: true }) as ClipboardEvent;
		Object.defineProperty(pasteEvent, "clipboardData", {
			value: { getData: vi.fn().mockReturnValue("native paste") },
		});
		container.firstElementChild!.dispatchEvent(pasteEvent);
		await Promise.resolve();

		expect(pasteEvent.defaultPrevented).toBe(true);
		expect(onInput).toHaveBeenCalledTimes(1);
		expect(onInput).toHaveBeenCalledWith("shortcut paste", "paste");
	});

	it("supports classic Windows terminal copy and paste shortcuts", async () => {
		const onInput = vi.fn();
		window.ao!.clipboard.readText = vi.fn().mockResolvedValue("insert paste");
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);
		state.lastTerminal!.selection = "insert copy";

		const copyEvent = {
			key: "Insert",
			metaKey: false,
			ctrlKey: true,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(copyEvent)).toBe(false);
		expect(window.ao!.clipboard.writeText).toHaveBeenCalledWith("insert copy");

		const pasteEvent = {
			key: "Insert",
			metaKey: false,
			ctrlKey: false,
			shiftKey: true,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(pasteEvent)).toBe(false);
		await Promise.resolve();

		expect(window.ao!.clipboard.readText).toHaveBeenCalled();
		expect(onInput).toHaveBeenCalledWith("insert paste", "paste");
	});

	it.each([
		["Option/Alt+Left", { key: "ArrowLeft", altKey: true }, "\x1bb"],
		["Option/Alt+Right", { key: "ArrowRight", altKey: true }, "\x1bf"],
		["Option/Alt+Backspace", { key: "Backspace", altKey: true }, "\x1b\x7f"],
		["Option/Alt+Delete", { key: "Delete", altKey: true }, "\x1bd"],
		["Ctrl+Left", { key: "ArrowLeft", ctrlKey: true }, "\x1b[1;5D"],
		["Ctrl+Right", { key: "ArrowRight", ctrlKey: true }, "\x1b[1;5C"],
		["Ctrl+Backspace", { key: "Backspace", ctrlKey: true }, "\x1b\x7f"],
		["Ctrl+Delete", { key: "Delete", ctrlKey: true }, "\x1bd"],
	])("normalizes %s into terminal input", (_name, init, expected) => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		const event = {
			metaKey: false,
			ctrlKey: false,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
			...init,
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);

		expect(allowed).toBe(false);
		expect(event.preventDefault).toHaveBeenCalled();
		expect(event.stopPropagation).toHaveBeenCalled();
		expect(onInput).toHaveBeenCalledWith(expected, "shortcut");
	});

	it("does not re-fire a shortcut on the keyup that follows its keydown", () => {
		// xterm.js invokes attachCustomKeyEventHandler on keydown, keyup, AND
		// keypress for the same physical key press. Without gating on event.type,
		// releasing Ctrl+Backspace would emit the escape sequence a second time.
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		const keyDown = {
			type: "keydown",
			key: "Backspace",
			ctrlKey: true,
			metaKey: false,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(keyDown)).toBe(false);
		expect(onInput).toHaveBeenCalledTimes(1);

		const keyUp = { ...keyDown, type: "keyup" } as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(keyUp)).toBe(true);
		expect(onInput).toHaveBeenCalledTimes(1);
	});

	it("does not re-paste on the keyup that follows a Cmd+V keydown", async () => {
		window.ao!.clipboard.readText = vi.fn().mockResolvedValue("pasted once");
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		const keyDown = {
			type: "keydown",
			key: "v",
			ctrlKey: false,
			metaKey: true,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(keyDown)).toBe(false);
		await Promise.resolve();

		const keyUp = { ...keyDown, type: "keyup" } as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(keyUp)).toBe(true);
		await Promise.resolve();

		expect(window.ao!.clipboard.readText).toHaveBeenCalledTimes(1);
		expect(onInput).toHaveBeenCalledTimes(1);
	});

	it("sends the meta-return escape sequence for Shift+Enter and consumes the event", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		const event = {
			type: "keydown",
			key: "Enter",
			metaKey: false,
			ctrlKey: false,
			shiftKey: true,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);

		expect(allowed).toBe(false);
		expect(event.preventDefault).toHaveBeenCalled();
		expect(event.stopPropagation).toHaveBeenCalled();
		expect(onInput).toHaveBeenCalledTimes(1);
		expect(onInput).toHaveBeenCalledWith("\x1b\r", "keyboard");
	});

	it("does not re-send the meta-return sequence on the keyup that follows Shift+Enter", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		const keyDown = {
			type: "keydown",
			key: "Enter",
			metaKey: false,
			ctrlKey: false,
			shiftKey: true,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(keyDown)).toBe(false);
		expect(onInput).toHaveBeenCalledTimes(1);

		const keyUp = { ...keyDown, type: "keyup" } as unknown as KeyboardEvent;
		expect(state.lastTerminal!.keyHandler!(keyUp)).toBe(true);
		expect(onInput).toHaveBeenCalledTimes(1);
	});

	it("leaves plain Enter as normal terminal input rather than intercepting it", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		const event = {
			type: "keydown",
			key: "Enter",
			metaKey: false,
			ctrlKey: false,
			shiftKey: false,
			altKey: false,
			preventDefault: vi.fn(),
			stopPropagation: vi.fn(),
		} as unknown as KeyboardEvent;
		const allowed = state.lastTerminal!.keyHandler!(event);

		expect(allowed).toBe(true);
		expect(event.preventDefault).not.toHaveBeenCalled();
		expect(event.stopPropagation).not.toHaveBeenCalled();
		expect(onInput).not.toHaveBeenCalled();
	});

	it("forwards keyboard input from explicit key events", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		state.lastTerminal!.keyListeners.forEach((listener) => listener({ key: "a" }));

		expect(onInput).toHaveBeenCalledWith("a", "keyboard");
	});

	it("does not forward raw xterm data/control bytes as user input", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		expect(state.lastTerminal!.dataListeners.size).toBe(0);
		state.lastTerminal!.dataListeners.forEach((listener) => listener("\x1b[A"));
		expect(onInput).not.toHaveBeenCalled();
	});

	it("translates wheel motion into SGR wheel reports for zellij scrollback", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);
		// rowHeight = fontSize(12) * lineHeight(1.35) = 16.2px; -50px => 3 lines up.
		const suppressed = state.lastTerminal!.wheelHandler!({ deltaY: -50 } as WheelEvent);

		expect(suppressed).toBe(false);
		expect(onInput).toHaveBeenCalledWith("\x1b[<64;1;1M\x1b[<64;1;1M\x1b[<64;1;1M", "wheel");
	});

	it("handles line- and page-mode wheels (Linux/Windows mice), not just pixel deltas", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		// DOM_DELTA_LINE: deltaY is already in lines, so one notch up => one report.
		expect(state.lastTerminal!.wheelHandler!({ deltaY: -1, deltaMode: 1 } as WheelEvent)).toBe(false);
		expect(onInput).toHaveBeenLastCalledWith("\x1b[<64;1;1M", "wheel");

		// DOM_DELTA_PAGE: one page down => rows (24) line reports down.
		onInput.mockClear();
		expect(state.lastTerminal!.wheelHandler!({ deltaY: 1, deltaMode: 2 } as WheelEvent)).toBe(false);
		expect(onInput).toHaveBeenLastCalledWith("\x1b[<65;1;1M".repeat(24), "wheel");
	});

	it("scrolls down on positive wheel delta and leaves zoom (ctrl/meta) wheel alone", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);

		expect(state.lastTerminal!.wheelHandler!({ deltaY: 20 } as WheelEvent)).toBe(false);
		expect(onInput).toHaveBeenCalledWith("\x1b[<65;1;1M", "wheel");

		onInput.mockClear();
		expect(state.lastTerminal!.wheelHandler!({ deltaY: -50, ctrlKey: true } as WheelEvent)).toBe(false);
		expect(onInput).not.toHaveBeenCalled();
	});

	it("scrolls xterm's own viewport for normal-buffer panes with mouse tracking off (codex, plain shell)", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);
		state.lastTerminal!.modes.mouseTrackingMode = "none";
		state.lastTerminal!.buffer.active.type = "normal";

		// rowHeight = 16.2px; -50px => 3 lines up. The pane never sees these bytes;
		// we scroll the terminal's retained scrollback locally instead.
		expect(state.lastTerminal!.wheelHandler!({ deltaY: -50 } as WheelEvent)).toBe(false);
		expect(state.lastTerminal!.scrollLines).toHaveBeenLastCalledWith(-3);
		expect(onInput).not.toHaveBeenCalled();

		expect(state.lastTerminal!.wheelHandler!({ deltaY: 20 } as WheelEvent)).toBe(false);
		expect(state.lastTerminal!.scrollLines).toHaveBeenLastCalledWith(1);
		expect(onInput).not.toHaveBeenCalled();
	});

	it("falls back to PageUp/PageDown for alt-buffer panes with mouse tracking off", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);
		state.lastTerminal!.modes.mouseTrackingMode = "none";
		// Alt buffer: no local scrollback to move, and no keyboard-scroll hint, so a
		// page key per notch is the best fallback.
		state.lastTerminal!.buffer.active.type = "alternate";

		expect(state.lastTerminal!.wheelHandler!({ deltaY: -50 } as WheelEvent)).toBe(false);
		expect(onInput).toHaveBeenLastCalledWith("\x1b[5~", "wheel");
		expect(state.lastTerminal!.scrollLines).not.toHaveBeenCalled();

		expect(state.lastTerminal!.wheelHandler!({ deltaY: 20 } as WheelEvent)).toBe(false);
		expect(onInput).toHaveBeenLastCalledWith("\x1b[6~", "wheel");
	});

	it("sends SGR reports on Windows when the pane tracks the mouse (conpty delivers them to the app)", () => {
		setNavigatorPlatform("Win32");
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" onReady={(terminal) => terminal.onUserInput(onInput)} />);
		// A mouse-tracking pane gets SGR reports on every platform; on Windows conpty
		// forwards them straight to the app. Keyboard-scroll panes (opencode) opt out
		// via the paneScrollsByKeyboard hint, tested separately.
		state.lastTerminal!.modes.mouseTrackingMode = "any";

		expect(state.lastTerminal!.wheelHandler!({ deltaY: -50 } as WheelEvent)).toBe(false);
		expect(onInput).toHaveBeenLastCalledWith("\x1b[<64;1;1M".repeat(3), "wheel");
	});

	it("sends PageUp/PageDown for keyboard-scroll panes even under a mux (opencode on macOS/Linux)", () => {
		const onInput = vi.fn();
		render(<XtermTerminal theme="dark" paneScrollsByKeyboard onReady={(terminal) => terminal.onUserInput(onInput)} />);
		// Linux (beforeEach) + mouse tracking on: without the paneScrollsByKeyboard
		// hint this would send SGR reports; the hint forces page keys.
		state.lastTerminal!.modes.mouseTrackingMode = "any";

		expect(state.lastTerminal!.wheelHandler!({ deltaY: -50 } as WheelEvent)).toBe(false);
		expect(onInput).toHaveBeenLastCalledWith("\x1b[5~", "wheel");
	});

	it("routes web links to the AO browser and does not open the system browser", () => {
		const open = vi.spyOn(window, "open").mockReturnValue(null);
		const onLinkOpen = vi.fn();
		render(<XtermTerminal onLinkOpen={onLinkOpen} theme="dark" />);

		// A left-click on an http(s) link is reported to the parent (which shows it
		// in the AO Browser panel); it must NOT spawn a system-browser window.
		expect(state.linkHandler).toBeTypeOf("function");
		state.linkHandler!({} as MouseEvent, "https://example.com");

		expect(onLinkOpen).toHaveBeenCalledWith("https://example.com");
		expect(open).not.toHaveBeenCalled();
		open.mockRestore();
	});

	it("routes OSC 8 web links to the AO browser without a system-browser window", () => {
		const open = vi.spyOn(window, "open").mockReturnValue(null);
		const onLinkOpen = vi.fn();
		render(<XtermTerminal onLinkOpen={onLinkOpen} theme="dark" />);
		const oscLinkHandler = state.lastTerminal!.options.linkHandler as {
			activate: (event: MouseEvent, uri: string) => void;
		};

		oscLinkHandler.activate({} as MouseEvent, "http://localhost:3000");

		expect(onLinkOpen).toHaveBeenCalledWith("http://localhost:3000");
		expect(open).not.toHaveBeenCalled();
		open.mockRestore();
	});

	it.each(["plain", "OSC 8"])("opens %s web links in the system browser on Option/Alt+Click", (kind) => {
		const openExternal = vi.fn().mockResolvedValue(undefined);
		window.ao!.app.openExternal = openExternal;
		const onLinkOpen = vi.fn();
		render(<XtermTerminal onLinkOpen={onLinkOpen} theme="dark" />);
		const oscHandler = state.lastTerminal!.options.linkHandler as {
			activate: (event: MouseEvent, uri: string) => void;
		};
		const handler = kind === "plain" ? state.linkHandler! : oscHandler.activate;
		handler({ altKey: true } as MouseEvent, "https://example.com");
		expect(openExternal).toHaveBeenCalledWith("https://example.com");
		expect(onLinkOpen).not.toHaveBeenCalled();
	});

	it("opens non-web links (mailto:) in the system browser, not the AO browser", () => {
		const open = vi.spyOn(window, "open").mockReturnValue(null);
		const onLinkOpen = vi.fn();
		render(<XtermTerminal onLinkOpen={onLinkOpen} theme="dark" />);

		expect(state.linkHandler).toBeTypeOf("function");
		state.linkHandler!({} as MouseEvent, "mailto:dev@example.com");

		expect(open).toHaveBeenCalledWith("mailto:dev@example.com", "_blank", "noopener");
		expect(onLinkOpen).not.toHaveBeenCalled();
		open.mockRestore();
	});

	it("forces plain drag selection without raw xterm data forwarding", () => {
		render(<XtermTerminal theme="dark" />);

		expect(state.lastTerminal!.options.macOptionClickForcesSelection).toBe(true);
		expect(state.lastTerminal!._core._selectionService.enable).toHaveBeenCalled();
		expect(state.lastTerminal!._core.element.classList.remove).toHaveBeenCalledWith("enable-mouse-events");
		expect(state.lastTerminal!._core._selectionService.shouldForceSelection({} as MouseEvent)).toBe(true);
	});
});
