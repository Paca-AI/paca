// Pure client-side computations for the four analytics panels.
// No React, no fetch — everything here is a plain function of the data
// returned by paca-api.ts, so the honest-approximation rules live in one
// reviewable place.
//
// Domain semantics this code relies on (verified in docs/api/http-design.md):
//  - A task is "done" when its STATUS'S CATEGORY is "done" (statuses are
//    per-project; categories are the fixed platform enum).
//  - Completing a sprint MOVES incomplete tasks out (to another sprint or the
//    backlog) and leaves only done-category tasks on the completed sprint
//    "for record-keeping". Two consequences:
//      * velocity per completed sprint IS reconstructable from current data;
//      * the original commitment ("planned") of a completed sprint is NOT —
//        report tables must say so instead of pretending.
//  - There is no task history API in v1, so burndown cannot plot past days;
//    we plot the ideal line plus TODAY'S actual remaining only.
import type {
	ProjectAnalyticsData,
	Sprint,
	StatusCategory,
	Task,
	TaskStatus,
} from "./paca-api";

// ── Category helpers ─────────────────────────────────────────────────────────

/** Fixed platform order — also the stacking order of the CFD bars. */
export const CATEGORY_ORDER: StatusCategory[] = [
	"backlog",
	"refinement",
	"ready",
	"todo",
	"inprogress",
	"done",
];

export const CATEGORY_LABELS: Record<StatusCategory, string> = {
	backlog: "Backlog",
	refinement: "Refinement",
	ready: "Ready",
	todo: "To Do",
	inprogress: "In Progress",
	done: "Done",
};

export function statusById(statuses: TaskStatus[]): Map<string, TaskStatus> {
	const map = new Map<string, TaskStatus>();
	for (const s of statuses) map.set(s.id, s);
	return map;
}

/** null when the task has no status or the status is unknown to us. */
export function categoryOf(
	task: Task,
	byId: Map<string, TaskStatus>,
): StatusCategory | null {
	if (!task.status_id) return null;
	return byId.get(task.status_id)?.category ?? null;
}

export function isDone(task: Task, byId: Map<string, TaskStatus>): boolean {
	return categoryOf(task, byId) === "done";
}

export function points(task: Task): number {
	return typeof task.story_points === "number" && task.story_points > 0
		? task.story_points
		: 0;
}

function sumPoints(tasks: Task[]): number {
	return tasks.reduce((acc, t) => acc + points(t), 0);
}

function parseDate(value?: string | null): number | null {
	if (!value) return null;
	const t = Date.parse(value);
	return Number.isNaN(t) ? null : t;
}

// ── Sprint progress (honest burndown v1) ─────────────────────────────────────

export interface SprintProgress {
	sprint: Sprint;
	totalTasks: number;
	doneTasks: number;
	totalPoints: number;
	donePoints: number;
	/** true when at least one in-sprint task carries story points. */
	hasPoints: boolean;
	/** 0..1 — by points when hasPoints, else by task count; NaN-safe. */
	completionRatio: number;
	/** ms timestamps; null when the sprint has no usable dates. */
	startMs: number | null;
	endMs: number | null;
	/** 0..1 of the timebox elapsed at `nowMs` (null without both dates). */
	elapsedRatio: number | null;
	/**
	 * Where the IDEAL line says remaining work should be right now, 0..1 of
	 * the total (null without both dates). v1 has no per-day history, so this
	 * is the only "burndown" comparison we can make honestly.
	 */
	idealRemainingRatio: number | null;
	/** 0..1 actual remaining (1 - completionRatio). */
	actualRemainingRatio: number;
}

export function computeSprintProgress(
	sprint: Sprint,
	tasks: Task[],
	byId: Map<string, TaskStatus>,
	nowMs: number,
): SprintProgress {
	const inSprint = tasks.filter((t) => t.sprint_id === sprint.id);
	const done = inSprint.filter((t) => isDone(t, byId));

	const totalPoints = sumPoints(inSprint);
	const donePoints = sumPoints(done);
	const hasPoints = totalPoints > 0;

	const completionRatio = hasPoints
		? donePoints / totalPoints
		: inSprint.length > 0
			? done.length / inSprint.length
			: 0;

	const startMs = parseDate(sprint.start_date);
	const endMs = parseDate(sprint.end_date);
	let elapsedRatio: number | null = null;
	if (startMs !== null && endMs !== null && endMs > startMs) {
		elapsedRatio = Math.min(1, Math.max(0, (nowMs - startMs) / (endMs - startMs)));
	}

	return {
		sprint,
		totalTasks: inSprint.length,
		doneTasks: done.length,
		totalPoints,
		donePoints,
		hasPoints,
		completionRatio,
		startMs,
		endMs,
		elapsedRatio,
		idealRemainingRatio: elapsedRatio === null ? null : 1 - elapsedRatio,
		actualRemainingRatio: 1 - completionRatio,
	};
}

// ── Velocity ─────────────────────────────────────────────────────────────────

export interface VelocityEntry {
	sprint: Sprint;
	/** Story points on done-category tasks still attached to the sprint. */
	donePoints: number;
	doneTasks: number;
}

export interface VelocityResult {
	entries: VelocityEntry[]; // chronological (oldest -> newest)
	averagePoints: number; // 0 when there are no completed sprints
}

/** Sort key for "when did this sprint happen": end date, else start, else created. */
function sprintWhen(s: Sprint): number {
	return (
		parseDate(s.end_date) ?? parseDate(s.start_date) ?? parseDate(s.created_at) ?? 0
	);
}

export function computeVelocity(
	data: Pick<ProjectAnalyticsData, "sprints" | "tasks" | "statuses">,
): VelocityResult {
	const byId = statusById(data.statuses);
	const completed = data.sprints
		.filter((s) => s.status === "completed")
		.sort((a, b) => sprintWhen(a) - sprintWhen(b));

	const entries: VelocityEntry[] = completed.map((sprint) => {
		const doneTasks = data.tasks.filter(
			(t) => t.sprint_id === sprint.id && isDone(t, byId),
		);
		return { sprint, donePoints: sumPoints(doneTasks), doneTasks: doneTasks.length };
	});

	const averagePoints =
		entries.length > 0
			? entries.reduce((acc, e) => acc + e.donePoints, 0) / entries.length
			: 0;

	return { entries, averagePoints };
}

// ── Status distribution (CFD v1: current snapshot, stacked by category) ─────

export interface CategoryCount {
	category: StatusCategory;
	count: number;
}

export interface DistributionRow {
	/** "Backlog" scope or a sprint name. */
	label: string;
	counts: CategoryCount[]; // always in CATEGORY_ORDER, zero-filled
	/** ALL tasks in scope — categorized + uncategorized (do not re-add). */
	total: number;
	/** Tasks whose status_id is missing/unknown — kept visible, not dropped. */
	uncategorized: number;
}

function countByCategory(
	tasks: Task[],
	byId: Map<string, TaskStatus>,
): { counts: CategoryCount[]; uncategorized: number } {
	const tally = new Map<StatusCategory, number>();
	let uncategorized = 0;
	for (const t of tasks) {
		const cat = categoryOf(t, byId);
		if (cat === null) {
			uncategorized++;
			continue;
		}
		tally.set(cat, (tally.get(cat) ?? 0) + 1);
	}
	return {
		counts: CATEGORY_ORDER.map((category) => ({
			category,
			count: tally.get(category) ?? 0,
		})),
		uncategorized,
	};
}

/**
 * One row for the backlog (tasks with no sprint) and one per non-completed
 * sprint (active first, then planned). Completed sprints are excluded: they
 * only retain done tasks, so their "distribution" is a tautology.
 */
export function computeDistribution(
	data: Pick<ProjectAnalyticsData, "sprints" | "tasks" | "statuses">,
): DistributionRow[] {
	const byId = statusById(data.statuses);
	const rows: DistributionRow[] = [];

	const backlogTasks = data.tasks.filter((t) => !t.sprint_id);
	const backlog = countByCategory(backlogTasks, byId);
	rows.push({
		label: "Backlog",
		counts: backlog.counts,
		total: backlogTasks.length,
		uncategorized: backlog.uncategorized,
	});

	const liveSprints = data.sprints
		.filter((s) => s.status === "active" || s.status === "planned")
		.sort((a, b) =>
			a.status === b.status
				? sprintWhen(a) - sprintWhen(b)
				: a.status === "active"
					? -1
					: 1,
		);
	for (const sprint of liveSprints) {
		const inSprint = data.tasks.filter((t) => t.sprint_id === sprint.id);
		const c = countByCategory(inSprint, byId);
		rows.push({
			label: sprint.name,
			counts: c.counts,
			total: inSprint.length,
			uncategorized: c.uncategorized,
		});
	}
	return rows;
}

// ── Sprint report ────────────────────────────────────────────────────────────

export interface SprintReportRow {
	sprint: Sprint;
	/**
	 * Tasks currently attached to the sprint. For active/planned sprints this
	 * is the live plan; for COMPLETED sprints Paca has already moved the
	 * unfinished tasks out, so this equals what was delivered — the original
	 * commitment is not reconstructable without history (see UI footnote).
	 */
	tasksAttached: number;
	tasksDone: number;
	/** Open (not-done) tasks still attached — the carry-over candidates. */
	tasksOpen: number;
	pointsDone: number;
	pointsTotal: number;
}

export function computeSprintReport(
	data: Pick<ProjectAnalyticsData, "sprints" | "tasks" | "statuses">,
): SprintReportRow[] {
	const byId = statusById(data.statuses);
	const order: Record<Sprint["status"], number> = {
		active: 0,
		planned: 1,
		completed: 2,
	};
	const sprints = [...data.sprints].sort((a, b) =>
		order[a.status] !== order[b.status]
			? order[a.status] - order[b.status]
			: sprintWhen(b) - sprintWhen(a),
	);

	return sprints.map((sprint) => {
		const attached = data.tasks.filter((t) => t.sprint_id === sprint.id);
		const done = attached.filter((t) => isDone(t, byId));
		return {
			sprint,
			tasksAttached: attached.length,
			tasksDone: done.length,
			tasksOpen: attached.length - done.length,
			pointsDone: sumPoints(done),
			pointsTotal: sumPoints(attached),
		};
	});
}

// ── Misc formatting helpers shared by panels ─────────────────────────────────

export function formatPct(ratio: number): string {
	if (!Number.isFinite(ratio)) return "0%";
	return `${Math.round(ratio * 100)}%`;
}

export function formatPoints(n: number): string {
	return Number.isInteger(n) ? String(n) : n.toFixed(1);
}

export function formatDay(ms: number | null): string {
	if (ms === null) return "—";
	const d = new Date(ms);
	const yyyy = d.getFullYear();
	const mm = String(d.getMonth() + 1).padStart(2, "0");
	const dd = String(d.getDate()).padStart(2, "0");
	return `${yyyy}-${mm}-${dd}`;
}
