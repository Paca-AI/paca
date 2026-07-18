import { describe, expect, it } from "vitest";
import {
	chordKeycaps,
	formatChord,
	matchShortcut,
	type ShortcutKeyEvent,
} from "./keymap";

function key(overrides: Partial<ShortcutKeyEvent>): ShortcutKeyEvent {
	return {
		key: "",
		metaKey: false,
		ctrlKey: false,
		altKey: false,
		shiftKey: false,
		...overrides,
	};
}

describe("matchShortcut", () => {
	it("ignores plain keystrokes with no modifier", () => {
		expect(matchShortcut(key({ key: "o" }))).toBeNull();
		expect(matchShortcut(key({ key: "s" }))).toBeNull();
	});

	it("ignores keystrokes with Alt held", () => {
		expect(
			matchShortcut(key({ key: "o", ctrlKey: true, altKey: true })),
		).toBeNull();
	});

	it("matches plain Mod+letter task actions", () => {
		expect(matchShortcut(key({ key: "o", ctrlKey: true }))).toBe("task.open");
		expect(matchShortcut(key({ key: "e", ctrlKey: true }))).toBe("task.epic");
		expect(matchShortcut(key({ key: "Backspace", ctrlKey: true }))).toBe(
			"task.delete",
		);
	});

	it("no longer binds the retired status/assignee/type/priority shortcuts", () => {
		expect(matchShortcut(key({ key: "s", ctrlKey: true }))).toBeNull();
		expect(matchShortcut(key({ key: "a", ctrlKey: true }))).toBeNull();
		expect(matchShortcut(key({ key: "y", ctrlKey: true }))).toBeNull();
		expect(matchShortcut(key({ key: "p", ctrlKey: true }))).toBeNull();
	});

	it("matches plain Mod+letter goto actions", () => {
		expect(matchShortcut(key({ key: "i", ctrlKey: true }))).toBe(
			"goto.backlog",
		);
		expect(matchShortcut(key({ key: "g", ctrlKey: true }))).toBe("goto.agents");
		expect(matchShortcut(key({ key: "d", ctrlKey: true }))).toBe("goto.docs");
	});

	it("does not fire plain-tier actions when Shift is also held", () => {
		// Shift+Mod+O must not accidentally trigger "task.open" (bound to plain Mod+O).
		expect(
			matchShortcut(key({ key: "O", ctrlKey: true, shiftKey: true })),
		).toBeNull();
	});

	it("matches Mod+Shift+letter goto actions", () => {
		expect(
			matchShortcut(key({ key: "h", ctrlKey: true, shiftKey: true })),
		).toBe("goto.home");
		expect(
			matchShortcut(key({ key: "w", metaKey: true, shiftKey: true })),
		).toBe("goto.automation");
	});

	it("matches the help shortcut regardless of Shift (layout-dependent '?')", () => {
		expect(matchShortcut(key({ key: "/", ctrlKey: true }))).toBe(
			"general.help",
		);
		expect(
			matchShortcut(key({ key: "?", ctrlKey: true, shiftKey: true })),
		).toBe("general.help");
	});

	it("returns null for unmapped combos", () => {
		expect(matchShortcut(key({ key: "z", ctrlKey: true }))).toBeNull();
		expect(
			matchShortcut(key({ key: "z", ctrlKey: true, shiftKey: true })),
		).toBeNull();
	});
});

describe("chordKeycaps / formatChord", () => {
	it("renders macOS symbols", () => {
		expect(chordKeycaps({ mod: true, shift: true, key: "F" }, true)).toEqual([
			"⌘",
			"⇧",
			"F",
		]);
		expect(formatChord({ mod: true, key: "S" }, true)).toBe("⌘S");
	});

	it("renders non-macOS labels with separators", () => {
		expect(chordKeycaps({ mod: true, shift: true, key: "F" }, false)).toEqual([
			"Ctrl",
			"Shift",
			"F",
		]);
		expect(formatChord({ mod: true, key: "S" }, false)).toBe("Ctrl+S");
	});
});
