// Client operation ids: every mutation this tab sends carries one in
// X-Client-Op-Id, the API stamps it on the changes the request causes, and the
// change comes back through the sync feed carrying it. A change with an id this
// tab sent is the echo of its own mutation: the mutation has already refreshed
// what it needs, so the echo is applied without refetching again.
//
// Ids live a minute: an echo that arrives later than that is treated like
// anyone else's change, which is only ever a redundant refetch, never a missed one.

const OWN_OP_TTL_MS = 60_000;
const MAX_OWN_OPS = 500;

const ownOps = new Map<string, number>();

/** A fresh id: short, URL- and header-safe. */
export function newClientOp(): string {
	const bytes = new Uint8Array(12);
	crypto.getRandomValues(bytes);
	return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/** Remembers an id this tab sent. */
export function rememberOwnOp(id: string, now = Date.now()): void {
	ownOps.set(id, now + OWN_OP_TTL_MS);
	// A Map iterates in insertion order, so the first keys are the oldest.
	while (ownOps.size > MAX_OWN_OPS) {
		const oldest = ownOps.keys().next().value;
		if (oldest === undefined) break;
		ownOps.delete(oldest);
	}
}

/** Whether `id` was sent by this tab, and not long ago. */
export function isOwnOp(id: string | undefined, now = Date.now()): boolean {
	if (!id) return false;
	const expires = ownOps.get(id);
	if (expires === undefined) return false;
	if (expires <= now) {
		ownOps.delete(id);
		return false;
	}
	return true;
}

/** Forgets every id (tests, logout). */
export function clearOwnOps(): void {
	ownOps.clear();
}

export const CLIENT_OP_HEADER = "X-Client-Op-Id";

// The realtime socket id this tab sends on its mutations (X-Collab-Socket).
// The collaborative-editing hub uses it to recognise a save made by one of a
// live room's editors (its own content coming back) and to merge every other
// write, including one made with the same account's API key by an agent.
export const COLLAB_SOCKET_HEADER = "X-Collab-Socket";

let collabSocketProvider: (() => string | undefined) | null = null;

/** Registers where the current socket id comes from (socket-client.ts). */
export function setCollabSocketProvider(
	provider: (() => string | undefined) | null,
): void {
	collabSocketProvider = provider;
}

/** The id of this tab's connected realtime socket, if there is one. */
export function collabSocketId(): string | undefined {
	return collabSocketProvider?.() || undefined;
}
