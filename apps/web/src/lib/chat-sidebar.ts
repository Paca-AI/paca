import type { InboxItem } from "./chat-api";

// The pure parts of the chat sidebar: what it remembers, how followed channels
// and direct messages are split, ordered, grouped and trimmed. The component
// (components/team-chat/chat-sidebar-section.tsx) only renders the result.

export type SectionKey = "chat" | "starred" | "dms";

export interface ChatSidebarState {
	/** Which sections are expanded. */
	open: Record<SectionKey, boolean>;
	/** Project groups the person collapsed. */
	closedProjects: Record<string, boolean>;
}

export const DEFAULT_SIDEBAR_STATE: ChatSidebarState = {
	open: { chat: true, starred: true, dms: true },
	closedProjects: {},
};

export const SIDEBAR_STORAGE_KEY = "paca:chat-sidebar";

/** Read direct messages beyond the unread ones shown before "Show more". */
export const FLAT_LIMIT = 8;

export function loadSidebarState(): ChatSidebarState {
	try {
		const raw = localStorage.getItem(SIDEBAR_STORAGE_KEY);
		if (!raw) return DEFAULT_SIDEBAR_STATE;
		const p = JSON.parse(raw) as Partial<ChatSidebarState>;
		return {
			open: {
				chat: p.open?.chat ?? true,
				starred: p.open?.starred ?? true,
				dms: p.open?.dms ?? true,
			},
			closedProjects: p.closedProjects ?? {},
		};
	} catch {
		return DEFAULT_SIDEBAR_STATE;
	}
}

export function saveSidebarState(s: ChatSidebarState): void {
	try {
		localStorage.setItem(SIDEBAR_STORAGE_KEY, JSON.stringify(s));
	} catch {
		/* remembering is a convenience */
	}
}

export const isUnread = (i: InboxItem): boolean =>
	i.unread_count > 0 || i.mention_count > 0;

/** What a row is called: the person for a direct message, else the channel. */
export const rowName = (i: InboxItem): string =>
	i.peer ? i.peer.name : i.channel_name;

/** Mentions first, then other unread, then the rest by name. A stable order, so
 * rows do not jump around while you read. */
export function sortRows(items: InboxItem[]): InboxItem[] {
	const rank = (i: InboxItem) =>
		i.mention_count > 0 ? 0 : i.unread_count > 0 ? 1 : 2;
	return [...items].sort(
		(a, b) =>
			rank(a) - rank(b) ||
			rowName(a).localeCompare(rowName(b)) ||
			a.project_name.localeCompare(b.project_name),
	);
}

export interface SidebarRows {
	starred: InboxItem[];
	channels: InboxItem[];
	dms: InboxItem[];
}

/** Starred ones (channels and direct messages) move to their own section; the
 * rest are channels or direct messages. */
export function splitRows(items: InboxItem[]): SidebarRows {
	return {
		starred: sortRows(items.filter((i) => i.starred)),
		channels: sortRows(items.filter((i) => !i.starred && !i.peer)),
		dms: sortRows(items.filter((i) => !i.starred && i.peer)),
	};
}

/** Unread rows always show; read ones up to the limit, unless `showAll`. */
export function limitRows(
	rows: InboxItem[],
	showAll: boolean,
	limit = FLAT_LIMIT,
): { shown: InboxItem[]; hidden: number } {
	if (showAll) return { shown: rows, hidden: 0 };
	let read = 0;
	const shown = rows.filter((r) => isUnread(r) || ++read <= limit);
	return { shown, hidden: rows.length - shown.length };
}

export interface ProjectGroup {
	id: string;
	name: string;
	rows: InboxItem[];
	unread: number;
	mentions: number;
}

/** Channels grouped by project: the current project first, then groups with
 * unread, then by name. */
export function groupByProject(
	rows: InboxItem[],
	currentProjectId?: string,
): ProjectGroup[] {
	const groups = new Map<string, ProjectGroup>();
	for (const r of rows) {
		if (!r.project_id) continue;
		const g = groups.get(r.project_id) ?? {
			id: r.project_id,
			name: r.project_name,
			rows: [],
			unread: 0,
			mentions: 0,
		};
		g.rows.push(r);
		g.unread += r.unread_count;
		g.mentions += r.mention_count;
		groups.set(r.project_id, g);
	}
	return [...groups.values()].sort(
		(a, b) =>
			Number(b.id === currentProjectId) - Number(a.id === currentProjectId) ||
			Number(b.unread + b.mentions > 0) - Number(a.unread + a.mentions > 0) ||
			a.name.localeCompare(b.name),
	);
}

export const totals = (rows: InboxItem[]) => ({
	unread: rows.reduce((n, r) => n + r.unread_count, 0),
	mentions: rows.reduce((n, r) => n + r.mention_count, 0),
});
