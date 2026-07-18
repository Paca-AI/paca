// Single source of truth for the keyboard shortcut system.
//
// Every binding is a flat Mod(+Shift)+<key> combo — Mod is ⌘ on macOS, Ctrl
// elsewhere. There is no leader/sequence step; each combo maps to exactly
// one action.
//
// Roughly half the keyboard is off-limits because the browser or the OS
// swallows the keydown before page JS ever sees it, or because overriding it
// would break a near-universal user expectation:
//   - Hard-blocked, no override possible on any browser: Mod+T/N/W (tab/window
//     control), Mod+1-9 (switch tab), Mod+L/K (address bar / omnibox).
//   - OS-level app menu items on macOS, never reach the page: ⌘Q, ⌘H, ⌘M.
//   - DevTools bindings (Shift variants): Mod+Shift+I/J/C/K/M/T/N/B/D/O/P/S/R/U/G/V/Z.
//   - Avoided even though technically overridable, because stealing them
//     breaks a near-universal expectation: Mod+A/C/V/X/Z (select all, copy,
//     paste, cut, undo) as *global* bindings.
//
// Matching lives here (pure, unit-testable); dispatching lives in provider.tsx.

export type ShortcutActionId =
	| "goto.home"
	| "goto.timeline"
	| "goto.backlog"
	| "goto.activeSprint"
	| "goto.agents"
	| "goto.conversations"
	| "goto.automation"
	| "goto.team"
	| "goto.docs"
	| "goto.settings"
	| "general.help"
	| "page.prevView"
	| "page.nextView"
	| "page.search"
	| "page.viewSettings"
	| "page.createTask"
	| "task.open"
	| "task.epic"
	| "task.moveLeft"
	| "task.moveRight"
	| "task.delete";

/** The subset of KeyboardEvent the matcher reads — keeps tests dependency-free. */
export interface ShortcutKeyEvent {
	key: string;
	metaKey: boolean;
	ctrlKey: boolean;
	altKey: boolean;
	shiftKey: boolean;
}

/** key is the lowercased KeyboardEvent.key value, e.g. "arrowleft", "backspace". */
const PLAIN_ACTIONS: Record<string, ShortcutActionId> = {
	// goto
	i: "goto.backlog",
	g: "goto.agents",
	j: "goto.team",
	d: "goto.docs",
	// page
	f: "page.search",
	"[": "page.prevView",
	"]": "page.nextView",
	enter: "page.createTask",
	// task
	o: "task.open",
	e: "task.epic",
	arrowleft: "task.moveLeft",
	arrowright: "task.moveRight",
	backspace: "task.delete",
};

const SHIFT_ACTIONS: Record<string, ShortcutActionId> = {
	h: "goto.home",
	l: "goto.timeline",
	a: "goto.activeSprint",
	x: "goto.conversations",
	w: "goto.automation",
	",": "goto.settings",
	f: "page.viewSettings",
};

/**
 * Maps a keydown to a shortcut. Returns null when the event is not ours —
 * the caller must then leave the event alone so browser/OS behavior wins.
 */
export function matchShortcut(e: ShortcutKeyEvent): ShortcutActionId | null {
	if (e.altKey) return null;
	const mod = e.metaKey || e.ctrlKey;
	if (!mod) return null;

	const key = e.key.toLowerCase();
	if (key === "/" || key === "?") return "general.help";

	return e.shiftKey
		? (SHIFT_ACTIONS[key] ?? null)
		: (PLAIN_ACTIONS[key] ?? null);
}

// ── Display registry (help dialog / hints) ───────────────────────────────────

export interface DisplayChord {
	mod?: boolean;
	shift?: boolean;
	/** Literal keycap text, e.g. "G", "1–9", "Enter", "←", "→", "⌫" */
	key: string;
}

export interface ShortcutDisplayEntry {
	id: string;
	group: "goto" | "general" | "page" | "task";
	/** Key inside the "shortcuts" i18n namespace, under "actions". */
	labelKey: string;
	/** Usually one chord; two only for a combined "left/right" style entry. */
	chords: DisplayChord[];
}

/** Ordered entries rendered in the help dialog, grouped by `group`. */
export const SHORTCUT_DISPLAY: ShortcutDisplayEntry[] = [
	{
		id: "goto.home",
		group: "goto",
		labelKey: "gotoHome",
		chords: [{ mod: true, shift: true, key: "H" }],
	},
	{
		id: "goto.timeline",
		group: "goto",
		labelKey: "gotoTimeline",
		chords: [{ mod: true, shift: true, key: "L" }],
	},
	{
		id: "goto.backlog",
		group: "goto",
		labelKey: "gotoBacklog",
		chords: [{ mod: true, key: "I" }],
	},
	{
		id: "goto.activeSprint",
		group: "goto",
		labelKey: "gotoActiveSprint",
		chords: [{ mod: true, shift: true, key: "A" }],
	},
	{
		id: "goto.agents",
		group: "goto",
		labelKey: "gotoAgents",
		chords: [{ mod: true, key: "G" }],
	},
	{
		id: "goto.conversations",
		group: "goto",
		labelKey: "gotoConversations",
		chords: [{ mod: true, shift: true, key: "X" }],
	},
	{
		id: "goto.automation",
		group: "goto",
		labelKey: "gotoAutomation",
		chords: [{ mod: true, shift: true, key: "W" }],
	},
	{
		id: "goto.team",
		group: "goto",
		labelKey: "gotoTeam",
		chords: [{ mod: true, key: "J" }],
	},
	{
		id: "goto.docs",
		group: "goto",
		labelKey: "gotoDocs",
		chords: [{ mod: true, key: "D" }],
	},
	{
		id: "goto.settings",
		group: "goto",
		labelKey: "gotoSettings",
		chords: [{ mod: true, shift: true, key: "," }],
	},
	{
		id: "general.help",
		group: "general",
		labelKey: "help",
		chords: [{ mod: true, key: "/" }],
	},
	{
		id: "general.sidebar",
		group: "general",
		labelKey: "sidebar",
		chords: [{ mod: true, key: "B" }],
	},
	{
		id: "general.close",
		group: "general",
		labelKey: "close",
		chords: [{ key: "Esc" }],
	},
	{
		id: "page.prevNextView",
		group: "page",
		labelKey: "prevNextView",
		chords: [
			{ mod: true, key: "[" },
			{ mod: true, key: "]" },
		],
	},
	{
		id: "page.search",
		group: "page",
		labelKey: "search",
		chords: [{ mod: true, key: "F" }],
	},
	{
		id: "page.createTask",
		group: "page",
		labelKey: "createTask",
		chords: [{ mod: true, key: "Enter" }],
	},
	{
		id: "page.viewSettings",
		group: "page",
		labelKey: "viewSettings",
		chords: [{ mod: true, shift: true, key: "F" }],
	},
	{
		id: "task.open",
		group: "task",
		labelKey: "open",
		chords: [{ mod: true, key: "O" }],
	},
	{
		id: "task.epic",
		group: "task",
		labelKey: "epic",
		chords: [{ mod: true, key: "E" }],
	},
	{
		id: "task.moveLeftRight",
		group: "task",
		labelKey: "moveLeftRight",
		chords: [
			{ mod: true, key: "←" },
			{ mod: true, key: "→" },
		],
	},
	{
		id: "task.delete",
		group: "task",
		labelKey: "delete",
		chords: [{ mod: true, key: "⌫" }],
	},
];

export const SHORTCUT_GROUPS = ["goto", "general", "page", "task"] as const;

// ── Key formatting ───────────────────────────────────────────────────────────

export function isMacPlatform(): boolean {
	if (typeof navigator === "undefined") return false;
	return /Mac|iPhone|iPad/.test(navigator.platform ?? navigator.userAgent);
}

/** Keycap labels for one chord, e.g. ["⌘", "⇧", "F"] or ["Ctrl", "Shift", "F"]. */
export function chordKeycaps(chord: DisplayChord, isMac: boolean): string[] {
	const caps: string[] = [];
	if (chord.mod) caps.push(isMac ? "⌘" : "Ctrl");
	if (chord.shift) caps.push(isMac ? "⇧" : "Shift");
	caps.push(chord.key);
	return caps;
}

/** Compact inline hint for menus, e.g. "⌘S" or "Ctrl+S". */
export function formatChord(chord: DisplayChord, isMac: boolean): string {
	const caps = chordKeycaps(chord, isMac);
	return caps.join(isMac ? "" : "+");
}
