import type { DoNotDisturb } from "./push-api";

/** Whether Do Not Disturb is on at `now`. The server's flag can be stale (it
 *  was read minutes ago and the end time has passed since), so the end time is
 *  checked again here. */
export function isDoNotDisturbActive(
	dnd: DoNotDisturb | undefined,
	now: number = Date.now(),
): boolean {
	if (!dnd?.active) return false;
	if (dnd.indefinite || !dnd.until) return dnd.indefinite;
	return new Date(dnd.until).getTime() > now;
}

/** The end times the Do Not Disturb buttons offer. "Tomorrow morning" is the
 *  next 08:00 local time, which is today's when it is still before 08:00. */
export function dndEndTimes(now: Date = new Date()) {
	const inHours = (h: number) => new Date(now.getTime() + h * 3_600_000);
	const morning = new Date(now);
	morning.setHours(8, 0, 0, 0);
	if (morning.getTime() <= now.getTime())
		morning.setDate(morning.getDate() + 1);
	return { oneHour: inHours(1), eightHours: inHours(8), nextMorning: morning };
}
