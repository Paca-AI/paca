import type { Task } from "@/lib/interaction-api";

const DAY_MS = 24 * 60 * 60 * 1000;

/** How far ahead the home page looks for upcoming deadlines. */
export const DUE_SOON_WINDOW_DAYS = 14;
/** The window behind the header's "due this week" count. */
export const DUE_THIS_WEEK_DAYS = 7;

/**
 * Whole calendar days from the viewer's local today to a task's due date:
 * 0 = today, 1 = tomorrow, -2 = two days overdue.
 *
 * Due dates are calendar dates stored as "YYYY-MM-DDT00:00:00Z" (see
 * formatDate's dateOnly note), so the due day is read in UTC while "today"
 * is the viewer's local date. Otherwise the day shifts for negative UTC
 * offsets. Returns null for a missing or unparseable date.
 */
export function daysUntilDue(
	dueDate: string | null | undefined,
	now: Date = new Date(),
): number | null {
	if (!dueDate) return null;
	const due = new Date(dueDate);
	if (Number.isNaN(due.getTime())) return null;
	const dueDay = Date.UTC(
		due.getUTCFullYear(),
		due.getUTCMonth(),
		due.getUTCDate(),
	);
	const today = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
	return Math.round((dueDay - today) / DAY_MS);
}

export interface DueTask {
	task: Task;
	dueDate: string;
	days: number;
}

/** Tasks that are overdue or due within `windowDays`, soonest first. */
export function upcomingDueTasks(
	tasks: Task[],
	windowDays: number = DUE_SOON_WINDOW_DAYS,
	now: Date = new Date(),
): DueTask[] {
	const result: DueTask[] = [];
	for (const task of tasks) {
		if (!task.due_date) continue;
		const days = daysUntilDue(task.due_date, now);
		if (days !== null && days <= windowDays) {
			result.push({ task, dueDate: task.due_date, days });
		}
	}
	return result.sort((a, b) => a.days - b.days);
}

export interface DueSummary {
	overdue: number;
	dueThisWeek: number;
}

/** Counts for the header's summary line. "This week" means today through
 *  the next DUE_THIS_WEEK_DAYS days, not the calendar week. */
export function summarizeDueDates(
	tasks: Task[],
	now: Date = new Date(),
): DueSummary {
	let overdue = 0;
	let dueThisWeek = 0;
	for (const task of tasks) {
		const days = daysUntilDue(task.due_date, now);
		if (days === null) continue;
		if (days < 0) overdue += 1;
		else if (days <= DUE_THIS_WEEK_DAYS) dueThisWeek += 1;
	}
	return { overdue, dueThisWeek };
}
