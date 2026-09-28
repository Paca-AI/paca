import {
	getPriority,
	type PriorityLabelKey,
} from "@/components/projects/interactions/priority";
import {
	listAllTasks,
	type ListTasksOptions,
	type Sprint,
	type Task,
} from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";

/** Task list API caps page_size at 200. */
const PAGE_SIZE = 200;
/** Safety cap so a runaway cursor loop cannot hang the browser. */
const MAX_PAGES = 100;

export interface TaskExportLookups {
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members: ProjectMember[];
	sprints: Sprint[];
	customFields: CustomFieldDefinition[];
	/** Resolves a priority i18n key (e.g. "priority.low") to a display label. */
	priorityLabel: (key: PriorityLabelKey) => string;
}

export interface TaskExportColumnLabels {
	id: string;
	title: string;
	status: string;
	type: string;
	sprint: string;
	parent: string;
	assignees: string;
	reporter: string;
	priority: string;
	importance: string;
	storyPoints: string;
	tags: string;
	startDate: string;
	dueDate: string;
	createdAt: string;
	updatedAt: string;
}

/**
 * Paginate through GET /projects/:id/tasks until every matching card is loaded.
 * Pass `opts` to mirror active board filters, or omit for the full project.
 */
export async function fetchAllTasksForExport(
	projectId: string,
	opts: ListTasksOptions = {},
): Promise<Task[]> {
	const all: Task[] = [];
	let cursor: string | undefined;
	for (let page = 0; page < MAX_PAGES; page++) {
		const result = await listAllTasks(projectId, {
			...opts,
			pageSize: PAGE_SIZE,
			cursor,
		});
		all.push(...result.items);
		const next = result.next_cursor;
		if (!next) break;
		cursor = next;
	}
	return all;
}

function escapeCsvCell(value: string): string {
	if (/[",\r\n]/.test(value)) {
		return `"${value.replaceAll('"', '""')}"`;
	}
	return value;
}

function formatDate(iso: string | null | undefined): string {
	if (!iso) return "";
	return iso.slice(0, 10);
}

function formatDateTime(iso: string | null | undefined): string {
	if (!iso) return "";
	return iso;
}

function memberName(
	membersById: Map<string, ProjectMember>,
	id: string | null | undefined,
): string {
	if (!id) return "";
	const m = membersById.get(id);
	if (!m) return id;
	return m.full_name || m.username || m.agent_name || id;
}

function formatCustomFieldValue(value: unknown): string {
	if (value == null) return "";
	if (typeof value === "boolean") return value ? "true" : "false";
	if (typeof value === "number") return String(value);
	if (typeof value === "string") return value;
	if (Array.isArray(value)) {
		return value.map((v) => formatCustomFieldValue(v)).join("; ");
	}
	try {
		return JSON.stringify(value);
	} catch {
		return String(value);
	}
}

/** Build a CSV string (UTF-8, RFC 4180-style quoting) from project tasks. */
export function tasksToCsv(
	tasks: Task[],
	lookups: TaskExportLookups,
	columnLabels: TaskExportColumnLabels,
): string {
	const statusById = new Map(lookups.statuses.map((s) => [s.id, s.name]));
	const typeById = new Map(lookups.taskTypes.map((tt) => [tt.id, tt.name]));
	const sprintById = new Map(lookups.sprints.map((s) => [s.id, s.name]));
	const membersById = new Map(lookups.members.map((m) => [m.id, m]));
	const taskNumberById = new Map(tasks.map((t) => [t.id, t.task_number]));

	const customFields = [...lookups.customFields].sort((a, b) =>
		a.display_name.localeCompare(b.display_name),
	);

	const headers = [
		columnLabels.id,
		columnLabels.title,
		columnLabels.status,
		columnLabels.type,
		columnLabels.sprint,
		columnLabels.parent,
		columnLabels.assignees,
		columnLabels.reporter,
		columnLabels.priority,
		columnLabels.importance,
		columnLabels.storyPoints,
		columnLabels.tags,
		columnLabels.startDate,
		columnLabels.dueDate,
		columnLabels.createdAt,
		columnLabels.updatedAt,
		...customFields.map((cf) => cf.display_name),
	];

	const rows = tasks.map((task) => {
		const parentNum = task.parent_task_id
			? taskNumberById.get(task.parent_task_id)
			: undefined;
		const priority = getPriority(task.importance ?? 0);
		const cells = [
			String(task.task_number),
			task.title ?? "",
			task.status_id ? (statusById.get(task.status_id) ?? task.status_id) : "",
			task.task_type_id
				? (typeById.get(task.task_type_id) ?? task.task_type_id)
				: "",
			task.sprint_id
				? (sprintById.get(task.sprint_id) ?? task.sprint_id)
				: "",
			parentNum != null ? String(parentNum) : "",
			(task.assignee_ids ?? [])
				.map((id) => memberName(membersById, id))
				.filter(Boolean)
				.join("; "),
			memberName(membersById, task.reporter_id),
			lookups.priorityLabel(priority.labelKey),
			String(task.importance ?? 0),
			task.story_points != null ? String(task.story_points) : "",
			(task.tags ?? []).join("; "),
			formatDate(task.start_date),
			formatDate(task.due_date),
			formatDateTime(task.created_at),
			formatDateTime(task.updated_at),
			...customFields.map((cf) =>
				formatCustomFieldValue(task.custom_fields?.[cf.field_key]),
			),
		];
		return cells.map(escapeCsvCell).join(",");
	});

	// BOM so Excel on Windows opens UTF-8 correctly.
	return `\uFEFF${[headers.map(escapeCsvCell).join(","), ...rows].join("\r\n")}\r\n`;
}

/** Trigger a browser download of the given CSV contents. */
export function downloadCsv(filename: string, csv: string): void {
	const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
	const url = URL.createObjectURL(blob);
	const anchor = document.createElement("a");
	anchor.href = url;
	anchor.download = filename;
	anchor.rel = "noopener";
	document.body.appendChild(anchor);
	anchor.click();
	anchor.remove();
	URL.revokeObjectURL(url);
}

export function buildExportFilename(
	projectSlug: string,
	now = new Date(),
): string {
	const safe = (projectSlug || "project")
		.trim()
		.replace(/[^\w.-]+/g, "-")
		.replace(/-+/g, "-")
		.replace(/^-|-$/g, "")
		.slice(0, 64);
	const yyyy = now.getFullYear();
	const mm = String(now.getMonth() + 1).padStart(2, "0");
	const dd = String(now.getDate()).padStart(2, "0");
	return `${safe || "project"}-tasks-${yyyy}-${mm}-${dd}.csv`;
}
