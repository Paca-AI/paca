/** A follow-up from the same author within this window drops its author line. */
export const GROUP_WINDOW_MS = 5 * 60 * 1000;

/** What the timeline needs to know about one entry. */
export interface TimelineEntry {
	id: string;
	authorId: string;
	/** ISO time. */
	at: string;
	deleted?: boolean;
	/** Position in the conversation, for the "new" divider. */
	seq?: number;
}

export function isFollowUp(
	prev: TimelineEntry | undefined,
	cur: TimelineEntry,
	windowMs = GROUP_WINDOW_MS,
): boolean {
	return (
		!!prev &&
		!prev.deleted &&
		prev.authorId === cur.authorId &&
		new Date(cur.at).getTime() - new Date(prev.at).getTime() < windowMs
	);
}

const dayOf = (iso: string) => {
	const d = new Date(iso);
	return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
};

export interface Slot {
	id: string;
	index: number;
	/** The entry starts a new day: a day divider goes above it. */
	dayStart: boolean;
	/** The entry is the first one past the reader's read position. */
	newStart: boolean;
	/** A follow-up of the entry above, shown without its author line. */
	compact: boolean;
}

/**
 * Places dividers and groups follow-ups. `readSeq` is how far the reader had read
 * when they arrived; the first later entry gets the "new" divider (none when
 * everything is read, or when nothing was read before: then all of it is new and
 * a divider above it all says nothing).
 */
export function layout(entries: TimelineEntry[], readSeq?: number): Slot[] {
	let marked = false;
	return entries.map((e, index) => {
		const prev = entries[index - 1];
		const dayStart = !prev || dayOf(prev.at) !== dayOf(e.at);
		const newStart =
			!marked &&
			readSeq !== undefined &&
			readSeq > 0 &&
			e.seq !== undefined &&
			e.seq > readSeq;
		if (newStart) marked = true;
		return {
			id: e.id,
			index,
			dayStart,
			newStart,
			// A divider breaks a run of one author's messages.
			compact: !dayStart && !newStart && isFollowUp(prev, e),
		};
	});
}
