export type DayGroup = "today" | "yesterday" | "earlier";

/** Which day-bucket a notification belongs to, by the viewer's local calendar
 *  day (so "Today" means what the clock on their wall says). */
export function dayGroup(iso: string, now: Date = new Date()): DayGroup {
	const startOfToday = new Date(now);
	startOfToday.setHours(0, 0, 0, 0);
	const t = new Date(iso).getTime();
	if (t >= startOfToday.getTime()) return "today";
	const startOfYesterday = new Date(startOfToday);
	startOfYesterday.setDate(startOfYesterday.getDate() - 1);
	return t >= startOfYesterday.getTime() ? "yesterday" : "earlier";
}

/** Splits an already newest-first list into consecutive day groups, keeping
 *  order. A group is omitted when it has no items. */
export function groupByDay<T extends { created_at: string }>(
	items: T[],
	now: Date = new Date(),
): { group: DayGroup; items: T[] }[] {
	const out: { group: DayGroup; items: T[] }[] = [];
	for (const item of items) {
		const g = dayGroup(item.created_at, now);
		const last = out[out.length - 1];
		if (last && last.group === g) last.items.push(item);
		else out.push({ group: g, items: [item] });
	}
	return out;
}
