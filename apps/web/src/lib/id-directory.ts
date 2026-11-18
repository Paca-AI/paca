import { useCallback, useSyncExternalStore } from "react";

// How long a resolved item is served without refreshing it, and how long to
// wait before retrying ids whose lookup failed.
const FRESH_MS = 5 * 60 * 1000;
const RETRY_MS = 15 * 1000;
// Ids requested within this window are fetched together.
const BATCH_DELAY_MS = 10;
/** Most ids one batch lookup asks the server for. */
export const MAX_IDS_PER_LOOKUP = 100;

interface Entry<T> {
	/** null: looked up and not found (deleted, not visible, no longer a member…). */
	item: T | null;
	/** When the id was last confirmed by the server. */
	at: number;
	/** When `item` itself was last replaced. */
	itemAt: number;
}

// Presigned storage URLs (avatars) are re-signed on every response, so the
// same avatar arrives with a different query string each time. Compared with
// the signature stripped, a record whose only difference is a fresh signature
// is unchanged: swapping it in would change every <img src> that shows it and
// make the browser fetch each avatar again. The kept URL is replaced once it
// is this old, well before the signature (an hour) expires.
const SIGNED_URL_KEEP_MS = 20 * 60 * 1000;

function stableJson(value: unknown): string {
	return JSON.stringify(value, (_key, v) =>
		typeof v === "string" && v.includes("X-Amz-Signature=")
			? v.split("?")[0]
			: v,
	);
}

/** Resolves ids to records without loading whole lists: ids asked for while
 *  rendering are collected and fetched in batches through a list endpoint's
 *  `ids` filter, then cached.
 *
 *  `get` returns undefined until an id has been resolved (and for ids that
 *  don't exist); subscribers are notified as each batch lands. */
export class IdDirectory<T> {
	private entries = new Map<string, Entry<T>>();
	private failedAt = new Map<string, number>();
	private pending = new Set<string>();
	private inflight = new Set<string>();
	private listeners = new Set<() => void>();
	private version = 0;
	private timer: ReturnType<typeof setTimeout> | undefined;

	private readonly fetchBatch: (ids: string[]) => Promise<T[]>;
	private readonly keysOf: (item: T) => (string | null | undefined)[];

	/**
	 * @param fetchBatch fetches the records for up to MAX_IDS_PER_LOOKUP ids.
	 * @param keysOf every id an item can be looked up by (its own id, an
	 *   alternate id…).
	 */
	constructor(
		fetchBatch: (ids: string[]) => Promise<T[]>,
		keysOf: (item: T) => (string | null | undefined)[],
	) {
		this.fetchBatch = fetchBatch;
		this.keysOf = keysOf;
	}

	subscribe = (listener: () => void): (() => void) => {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	};

	getVersion = (): number => this.version;

	get(id: string | null | undefined): T | undefined {
		if (!id) return undefined;
		const entry = this.entries.get(id);
		const now = Date.now();
		if (!entry || now - entry.at > FRESH_MS) this.request(id, now);
		return entry?.item ?? undefined;
	}

	/** Records items already fetched elsewhere (a picker page, an embedded
	 *  list) so they needn't be asked for again. */
	seed(items: T[]): void {
		const now = Date.now();
		let changed = false;
		for (const item of items) {
			if (this.store(item, now)) changed = true;
		}
		if (changed) this.bump();
	}

	/** Records `item` under each of its ids. Returns whether anything a
	 *  subscriber could render differently was replaced. */
	private store(item: T, now: number, onlyKnown?: Set<string>): boolean {
		const serialized = stableJson(item);
		let changed = false;
		for (const key of this.keysOf(item)) {
			if (!key) continue;
			if (onlyKnown && !onlyKnown.has(key)) continue;
			const existing = this.entries.get(key);
			// Every page of tasks re-seeds the people it embeds. Re-seeding an
			// unchanged record must not notify subscribers: each notification
			// re-renders every card and picker showing a member, so a page
			// load would redraw the whole board.
			if (
				existing?.item != null &&
				now - existing.itemAt < SIGNED_URL_KEEP_MS &&
				stableJson(existing.item) === serialized
			) {
				existing.at = now;
				continue;
			}
			this.entries.set(key, { item, at: now, itemAt: now });
			changed = true;
		}
		return changed;
	}

	/** Drops everything cached; ids in use are fetched again on next read. */
	invalidate(): void {
		this.entries.clear();
		this.failedAt.clear();
		this.pending.clear();
		this.bump();
	}

	private request(id: string, now: number): void {
		if (this.pending.has(id) || this.inflight.has(id)) return;
		const failed = this.failedAt.get(id);
		if (failed !== undefined && now - failed < RETRY_MS) return;
		this.pending.add(id);
		if (this.timer === undefined) {
			this.timer = setTimeout(() => void this.flush(), BATCH_DELAY_MS);
		}
	}

	private async flush(): Promise<void> {
		this.timer = undefined;
		const ids = [...this.pending];
		this.pending.clear();
		for (const id of ids) this.inflight.add(id);
		for (let i = 0; i < ids.length; i += MAX_IDS_PER_LOOKUP) {
			await this.fetchChunk(ids.slice(i, i + MAX_IDS_PER_LOOKUP));
		}
	}

	private async fetchChunk(chunk: string[]): Promise<void> {
		try {
			const items = await this.fetchBatch(chunk);
			const now = Date.now();
			const requested = new Set(chunk);
			const found = new Set<string>();
			for (const item of items) {
				for (const key of this.keysOf(item)) if (key) found.add(key);
				this.store(item, now, requested);
			}
			for (const id of chunk) {
				if (!found.has(id))
					this.entries.set(id, { item: null, at: now, itemAt: now });
			}
			for (const id of chunk) this.failedAt.delete(id);
		} catch {
			const now = Date.now();
			for (const id of chunk) this.failedAt.set(id, now);
		} finally {
			for (const id of chunk) this.inflight.delete(id);
			this.bump();
		}
	}

	private bump(): void {
		this.version++;
		for (const listener of this.listeners) listener();
	}
}

/** Lazy id → item lookup backed by `dir`. The returned function changes
 *  identity whenever new items resolve, so memos and callbacks that depend on
 *  it recompute. */
export function useIdDirectory<T>(dir: IdDirectory<T>) {
	const version = useSyncExternalStore(dir.subscribe, dir.getVersion);
	// biome-ignore lint/correctness/useExhaustiveDependencies: version is the invalidation signal
	return useCallback(
		(id: string | null | undefined) => dir.get(id),
		[dir, version],
	);
}
