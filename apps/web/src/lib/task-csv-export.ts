import { getImportanceBucket } from "@/components/projects/interactions/priority";
import { listAllTasks, type Task } from "@/lib/interaction-api";
import {
	type CustomFieldDefinition,
	getProject,
	listCustomFieldDefinitions,
} from "@/lib/project-api";

const CSV_BOM = "\uFEFF";

/** Columns the Timeline Export button writes — Identity + the PE-3 field set. */
const EXPORT_COLUMNS = [
	"ID",
	"Title",
	"Commitment",
	"Estimated Effort",
	"Priority",
	"QA by",
	"QA Start Date",
	"QA End Date",
	"Actual Dev Efforts",
	"Dev Completion Date",
] as const;

/** Custom-field display names / aliases we resolve into each export column. */
const CUSTOM_FIELD_ALIASES: Record<
	Exclude<(typeof EXPORT_COLUMNS)[number], "ID" | "Title" | "Priority">,
	string[]
> = {
	Commitment: ["commitment"],
	"Estimated Effort": [
		"estimated effort",
		"estimated efforts",
		"estimate",
		"estimation",
	],
	"QA by": ["qa by", "qa assignee", "qa"],
	"QA Start Date": ["qa start date", "qa start"],
	"QA End Date": ["qa end date", "qa end", "qa due date"],
	"Actual Dev Efforts": [
		"actual dev efforts",
		"actual dev efforts (in days)",
		"actual dev effort",
		"actual effort",
	],
	"Dev Completion Date": [
		"dev completion date",
		"development completion date",
		"dev complete date",
	],
};

const PRIORITY_LABELS = [
	"None",
	"Low",
	"Medium",
	"High",
	"Critical",
] as const;

function neutralizeFormula(value: string): string {
	return /^[=+\-@]/.test(value) ? `'${value}` : value;
}

function csvEscape(value: string): string {
	const safe = neutralizeFormula(value);
	if (/[",\n\r]/.test(safe)) return `"${safe.replace(/"/g, '""')}"`;
	return safe;
}

function normalizeName(name: string): string {
	return name.trim().toLowerCase().replace(/\s+/g, " ");
}

function formatCustomFieldValue(v: unknown): string {
	if (v == null) return "";
	if (typeof v === "string") return v;
	if (typeof v === "boolean") return v ? "true" : "false";
	if (typeof v === "number") return String(v);
	if (Array.isArray(v)) {
		return v
			.map((item) => formatCustomFieldValue(item))
			.filter(Boolean)
			.join("; ");
	}
	try {
		return JSON.stringify(v);
	} catch {
		return String(v);
	}
}

function priorityLabel(importance: number): string {
	return PRIORITY_LABELS[getImportanceBucket(importance)] ?? "None";
}

function findCustomField(
	defs: CustomFieldDefinition[],
	aliases: string[],
): CustomFieldDefinition | undefined {
	const wanted = new Set(aliases.map(normalizeName));
	return defs.find(
		(d) =>
			wanted.has(normalizeName(d.display_name)) ||
			wanted.has(normalizeName(d.field_key.replace(/_/g, " "))),
	);
}

async function fetchAllProjectTasks(projectId: string): Promise<Task[]> {
	const items: Task[] = [];
	let cursor: string | undefined;
	for (;;) {
		const page = await listAllTasks(projectId, {
			pageSize: 200,
			cursor,
		});
		items.push(...page.items);
		if (!page.next_cursor) break;
		cursor = page.next_cursor;
	}
	return items;
}

/** Builds an Excel-friendly CSV (UTF-8 BOM) with the PE-3 export columns only. */
export async function buildProjectTasksCsv(projectId: string): Promise<{
	csv: string;
	fileName: string;
	rowCount: number;
}> {
	const [project, customFields, tasks] = await Promise.all([
		getProject(projectId),
		listCustomFieldDefinitions(projectId),
		fetchAllProjectTasks(projectId),
	]);

	const prefix = project.task_id_prefix || "TASK";
	const fieldByColumn = Object.fromEntries(
		(
			Object.entries(CUSTOM_FIELD_ALIASES) as [
				keyof typeof CUSTOM_FIELD_ALIASES,
				string[],
			][]
		).map(([col, aliases]) => [col, findCustomField(customFields, aliases)]),
	) as Record<
		keyof typeof CUSTOM_FIELD_ALIASES,
		CustomFieldDefinition | undefined
	>;

	const lines = [EXPORT_COLUMNS.map((h) => csvEscape(h)).join(",")];
	for (const task of tasks) {
		const cf = task.custom_fields ?? {};
		const cell = (col: keyof typeof CUSTOM_FIELD_ALIASES): string => {
			const def = fieldByColumn[col];
			if (!def) return "";
			return formatCustomFieldValue(cf[def.field_key]);
		};

		lines.push(
			[
				`${prefix}-${task.task_number}`,
				task.title,
				cell("Commitment"),
				cell("Estimated Effort"),
				priorityLabel(task.importance ?? 0),
				cell("QA by"),
				cell("QA Start Date"),
				cell("QA End Date"),
				cell("Actual Dev Efforts"),
				cell("Dev Completion Date"),
			]
				.map(csvEscape)
				.join(","),
		);
	}

	const date = new Date().toISOString().slice(0, 10);
	const safeName = (project.name || "project")
		.replace(/[^\w\-]+/g, "-")
		.replace(/-+/g, "-")
		.replace(/^-|-$/g, "")
		.toLowerCase();

	return {
		csv: `${CSV_BOM}${lines.join("\n")}`,
		fileName: `${safeName || "project"}-tasks-${date}.csv`,
		rowCount: tasks.length,
	};
}

/** Triggers an immediate browser download of a CSV string. */
export function downloadCsv(csv: string, fileName: string) {
	const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
	const objectUrl = URL.createObjectURL(blob);
	const a = document.createElement("a");
	a.href = objectUrl;
	a.download = fileName;
	a.rel = "noopener";
	document.body.appendChild(a);
	a.click();
	a.remove();
	URL.revokeObjectURL(objectUrl);
}
