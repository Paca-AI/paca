import type { ChatMessage, MessagePage } from "./chat-api";

// The cached messages of a channel (or of one thread): the newest messages
// loaded so far, oldest first, and whether older ones exist on the server.
// Every update goes through these pure functions so live announcements, a
// catch-up after a reconnect and the user's own sends all merge the same way.

export interface MessageWindow {
	items: ChatMessage[];
	/** More messages exist before the first loaded one. */
	hasOlder: boolean;
}

export const EMPTY_WINDOW: MessageWindow = { items: [], hasOlder: false };

/** Merges `incoming` into `items` by id (the newer copy wins), oldest first. */
export function mergeMessages(
	items: ChatMessage[],
	incoming: ChatMessage[],
): ChatMessage[] {
	if (incoming.length === 0) return items;
	const byId = new Map(items.map((m) => [m.id, m]));
	for (const m of incoming) byId.set(m.id, m);
	return [...byId.values()].sort((a, b) => a.seq - b.seq);
}

/** The window after the latest page was fetched or after a catch-up. */
export function withNewest(
	cur: MessageWindow | undefined,
	page: MessagePage,
	replace: boolean,
): MessageWindow {
	if (!cur || replace) {
		return { items: mergeMessages([], page.items), hasOlder: page.has_more };
	}
	return {
		items: mergeMessages(cur.items, page.items),
		hasOlder: cur.hasOlder,
	};
}

/** The window after an older page was loaded. */
export function withOlder(
	cur: MessageWindow | undefined,
	page: MessagePage,
): MessageWindow {
	const base = cur ?? EMPTY_WINDOW;
	return {
		items: mergeMessages(base.items, page.items),
		hasOlder: page.has_more,
	};
}

/** The highest seq held, or 0. */
export function lastSeq(w: MessageWindow | undefined): number {
	const items = w?.items ?? [];
	return items.length ? (items[items.length - 1] as ChatMessage).seq : 0;
}

/** The lowest seq held, or 0. */
export function firstSeq(w: MessageWindow | undefined): number {
	return w?.items[0]?.seq ?? 0;
}

/** Replaces one message in the window, if it is there. */
export function replaceMessage(
	w: MessageWindow | undefined,
	m: ChatMessage,
): MessageWindow | undefined {
	if (!w?.items.some((x) => x.id === m.id)) return w;
	return { ...w, items: mergeMessages(w.items, [m]) };
}

/** Adjusts a reaction count locally, ahead of the server's answer. */
export function toggleReaction(
	m: ChatMessage,
	emoji: string,
	on: boolean,
): ChatMessage {
	const existing = m.reactions.find((r) => r.emoji === emoji);
	if (existing?.mine === on) return m;
	let reactions = m.reactions;
	if (existing) {
		const count = existing.count + (on ? 1 : -1);
		reactions = m.reactions
			.map((r) => (r.emoji === emoji ? { ...r, count, mine: on } : r))
			.filter((r) => r.count > 0);
	} else if (on) {
		reactions = [...m.reactions, { emoji, count: 1, mine: true }];
	}
	return { ...m, reactions };
}
