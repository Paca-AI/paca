import { ZodError } from "zod";
import type {
	Attachment,
	CustomFieldDefinition,
	Document,
	Project,
	ProjectMember,
	Sprint,
	Task,
	TaskActivity,
	TaskLink,
	TaskStatus,
	TaskType,
} from "../types/index.js";
import { blocknoteToMarkdown } from "./converters.js";

/**
 * Formats a thrown error for the calling agent. A raw ZodError's .message is
 * a JSON dump of every issue — for an array of N invalid items that's N
 * near-identical blocks (e.g. 8 nodes missing posX/posY renders as 16+
 * repeated issues), which buries the one actionable fact under noise. This
 * collapses issues that differ only by array index into a single deduped
 * line with a count, so the caller sees e.g. "nodes.set.*.posX: Required
 * (x8)" instead of the same message eight times over.
 */
export function formatToolError(error: unknown): string {
	if (error instanceof ZodError) {
		const counts = new Map<string, number>();
		for (const issue of error.issues) {
			const path = issue.path
				.map((segment) => (typeof segment === "number" ? "*" : segment))
				.join(".");
			const key = path ? `${path}: ${issue.message}` : issue.message;
			counts.set(key, (counts.get(key) ?? 0) + 1);
		}
		const lines = [...counts.entries()].map(([key, count]) =>
			count > 1 ? `${key} (x${count})` : key,
		);
		return `Invalid arguments:\n${lines.join("\n")}`;
	}
	return error instanceof Error ? error.message : "Unknown error";
}

/**
 * Turns a non-ok API response into an error message for the calling agent.
 * The backend's error envelope is JSON ({ error, error_code, ... }); when the
 * body parses as that shape, this surfaces its `error` message directly —
 * prefixed with "Permission denied" for 403s — instead of dumping the raw
 * status/body, so a permission failure reads as an actionable instruction
 * ("you don't have permission to do that") rather than looking like a server
 * malfunction the agent might retry. Falls back to the raw status/body when
 * the response isn't that JSON shape (e.g. a proxy error page).
 */
export function formatApiRequestError(
	status: number,
	statusText: string,
	errorText: string,
): string {
	let apiMessage: string | undefined;
	try {
		const parsed = JSON.parse(errorText);
		if (
			parsed &&
			typeof parsed === "object" &&
			typeof parsed.error === "string"
		) {
			apiMessage = parsed.error;
		}
	} catch {
		// Not the JSON error envelope — fall through to the raw-text format below.
	}

	if (apiMessage) {
		return status === 403
			? `Permission denied: ${apiMessage}`
			: `${apiMessage} (HTTP ${status})`;
	}

	return `API request failed: ${status} ${statusText} - ${errorText}`;
}

/**
 * Formats a task object into a readable string.
 * @param task - The task object to format
 * @returns Formatted task string with description in Markdown
 */
export function formatTask(task: Task): string {
	const description = task.description
		? blocknoteToMarkdown(task.description)
		: "No description";

	return `Task #${task.task_number}: ${task.title}
ID: ${task.id}
Status: ${task.status_id || "None"}
Type: ${task.task_type_id || "None"}
Sprint: ${task.sprint_id || "None"}
Assignees: ${task.assignee_ids && task.assignee_ids.length > 0 ? task.assignee_ids.join(", ") : "Unassigned"}
Parent Task: ${task.parent_task_id || "None"}
Importance: ${task.importance}
Story Points: ${task.story_points ?? "None"}
Tags: ${task.tags && task.tags.length > 0 ? task.tags.join(", ") : "None"}
Start Date: ${task.start_date || "None"}
Due Date: ${task.due_date || "None"}
Created: ${task.created_at}
Updated: ${task.updated_at}

Description:
${description}`;
}

/**
 * Formats a comprehensive task detail view with all related information.
 * @param task - The task object
 * @param project - The project object
 * @param status - The task status object (if available)
 * @param taskType - The task type object (if available)
 * @param sprint - The sprint object (if available)
 * @param assignees - The assignee member objects (if available)
 * @param reporter - The reporter member object (if available)
 * @param parentTask - The parent task object (if available)
 * @param subtasks - Array of subtasks (if available)
 * @param attachments - Array of attachments (if available)
 * @param activities - Array of activities (if available)
 * @param customFields - Array of custom field definitions (if available)
 * @returns Comprehensive formatted task detail string
 */
const DISPLAY_LINK_LABELS: Record<string, string> = {
	blocks: "Blocks",
	is_blocked_by: "Is blocked by",
	relates_to: "Relates to",
	duplicates: "Duplicates",
	is_duplicated_by: "Is duplicated by",
};

export function formatTaskDetail(
	task: Task,
	project?: Project,
	status?: TaskStatus,
	taskType?: TaskType,
	sprint?: Sprint,
	assignees?: ProjectMember[],
	reporter?: ProjectMember,
	parentTask?: Task,
	subtasks?: Task[],
	attachments?: Attachment[],
	activities?: TaskActivity[],
	customFields?: CustomFieldDefinition[],
	links?: TaskLink[],
): string {
	const description = task.description
		? blocknoteToMarkdown(task.description)
		: "No description";

	const taskIdPrefix = project?.task_id_prefix || "";

	const sections: string[] = [];

	sections.push(
		`# Task ${taskIdPrefix ? `${taskIdPrefix}-` : ""}${task.task_number}: ${task.title}`,
	);

	sections.push(`**ID:** ${task.id}`);

	if (status || task.status_id) {
		sections.push(
			`**Status:** ${status ? status.name : task.status_id || "None"}${status?.color ? ` (Color: ${status.color})` : ""}`,
		);
	}

	if (taskType || task.task_type_id) {
		sections.push(
			`**Type:** ${taskType ? taskType.name : task.task_type_id || "None"}${taskType?.icon ? ` (Icon: ${taskType.icon})` : ""}${taskType?.color ? ` (Color: ${taskType.color})` : ""}`,
		);
	}

	if (sprint || task.sprint_id) {
		sections.push(
			`**Sprint:** ${sprint ? sprint.name : task.sprint_id || "None"}`,
		);
	}

	if (
		(assignees && assignees.length > 0) ||
		(task.assignee_ids && task.assignee_ids.length > 0)
	) {
		const label =
			assignees && assignees.length > 0
				? assignees
						.map((a) => `${a.full_name || a.username} (@${a.username})`)
						.join(", ")
				: (task.assignee_ids ?? []).join(", ") || "Unassigned";
		sections.push(`**Assignees:** ${label}`);
	}

	if (reporter || task.reporter_id) {
		sections.push(
			`**Reporter:** ${reporter ? `${reporter.full_name || reporter.username} (@${reporter.username})` : task.reporter_id || "None"}`,
		);
	}

	if (parentTask || task.parent_task_id) {
		sections.push(
			`**Parent Task:** ${parentTask ? `${parentTask.title} (#${parentTask.task_number})` : task.parent_task_id || "None"}`,
		);
	}

	sections.push(`**Importance:** ${task.importance}`);
	sections.push(`**Story Points:** ${task.story_points ?? "None"}`);

	if (task.tags && task.tags.length > 0) {
		sections.push(`**Tags:** ${task.tags.join(", ")}`);
	} else {
		sections.push(`**Tags:** None`);
	}

	if (task.start_date) {
		sections.push(`**Start Date:** ${task.start_date}`);
	}

	if (task.due_date) {
		sections.push(`**Due Date:** ${task.due_date}`);
	}

	sections.push(`**Created:** ${task.created_at}`);
	sections.push(`**Updated:** ${task.updated_at}`);

	sections.push("");

	if (customFields && customFields.length > 0) {
		sections.push("## Custom Fields");
		for (const field of customFields) {
			const value = task.custom_fields?.[field.field_key];
			sections.push(
				`- **${field.display_name}** (${field.field_type}): ${formatCustomFieldValue(value, field.field_type)}`,
			);
		}
		sections.push("");
	}

	sections.push("## Description");
	sections.push(description);

	if (subtasks && subtasks.length > 0) {
		sections.push("");
		sections.push("## Subtasks");
		subtasks.forEach((subtask, index) => {
			sections.push(
				`${index + 1}. **${subtask.title}** (#${subtask.task_number}) - Status ID: ${subtask.status_id || "None"}, Type ID: ${subtask.task_type_id || "None"}, Assignee IDs: ${subtask.assignee_ids && subtask.assignee_ids.length > 0 ? subtask.assignee_ids.join(", ") : "Unassigned"}`,
			);
		});
	}

	if (attachments && attachments.length > 0) {
		sections.push("");
		sections.push("## Attachments");
		attachments.forEach((attachment) => {
			sections.push(
				`- **${attachment.file.file_name}** (${formatFileSize(attachment.file.file_size)}) - Uploaded: ${attachment.created_at}`,
			);
		});
	}

	if (links && links.length > 0) {
		sections.push("");
		sections.push("## Linked Tasks");
		links.forEach((link) => {
			const label =
				DISPLAY_LINK_LABELS[link.display_link_type] || link.display_link_type;
			const t = link.linked_task;
			const taskRef = t ? `#${t.task_number} — ${t.title} (ID: ${t.id})` : "";
			sections.push(`- **${label}:** ${taskRef} *(Link ID: ${link.id})*`);
		});
	}

	if (activities && activities.length > 0) {
		sections.push("");
		sections.push("## Activities");
		activities.forEach((activity) => {
			sections.push(
				`- **${activity.activity_type}** by ${activity.actor_name} (@${activity.actor_username}) - ${activity.created_at}`,
			);
			if (activity.activity_type === "comment" && activity.content) {
				const commentContent = blocknoteToMarkdown(activity.content as any);
				if (commentContent && commentContent.trim() !== "") {
					const indentedCommentContent = commentContent
						.split("\n")
						.map((line, index) => (index === 0 ? `  - ${line}` : `    ${line}`))
						.join("\n");
					sections.push(indentedCommentContent);
				}
			}
		});
	}

	return sections.join("\n");
}

function formatCustomFieldValue(value: unknown, fieldType: string): string {
	if (value === null || value === undefined) {
		return "None";
	}

	switch (fieldType) {
		case "boolean":
			return String(value);
		case "multi_select":
			if (Array.isArray(value)) {
				return value.join(", ");
			}
			return String(value);
		default:
			return String(value);
	}
}

function formatFileSize(bytes: number): string {
	if (bytes === 0) return "0 Bytes";
	const k = 1024;
	const sizes = ["Bytes", "KB", "MB", "GB"];
	const i = Math.floor(Math.log(bytes) / Math.log(k));
	return `${Math.round((bytes / k ** i) * 100) / 100} ${sizes[i]}`;
}

/**
 * Formats a document object into a readable string.
 * @param doc - The document object to format
 * @returns Formatted document string with content in Markdown
 */
export function formatDocument(doc: Document): string {
	const content = doc.content ? blocknoteToMarkdown(doc.content) : "No content";

	return `Document: ${doc.title}
ID: ${doc.id}
Project: ${doc.project_id || "None"}
Folder: ${doc.folder_id || "None"}
Position: ${doc.position}
Created by: ${doc.created_by || "Unknown"}
Updated by: ${doc.updated_by || "Unknown"}
Created: ${doc.created_at}
Updated: ${doc.updated_at}

Content:
${content}`;
}

/**
 * Formats a project object into a readable string.
 * @param project - The project object to format
 * @returns Formatted project string
 */
export function formatProject(project: Project): string {
	return `Project: ${project.name}
ID: ${project.id}
Description: ${project.description || "No description"}
Task ID Prefix: ${project.task_id_prefix || "None"}
Created by: ${project.created_by || "Unknown"}
Created: ${project.created_at}`;
}

/**
 * Formats a sprint object into a readable string.
 * @param sprint - The sprint object to format
 * @returns Formatted sprint string
 */
export function formatSprint(sprint: Sprint): string {
	return `Sprint: ${sprint.name}
ID: ${sprint.id}
Project: ${sprint.project_id}
Start Date: ${sprint.start_date || "None"}
End Date: ${sprint.end_date || "None"}
Goal: ${sprint.goal || "None"}
Status: ${sprint.status}
Created: ${sprint.created_at}
Updated: ${sprint.updated_at}`;
}

/**
 * Formats a list of items with a separator.
 * @param items - Array of items to format
 * @param formatter - Function to format each item
 * @param separator - Separator string between items
 * @returns Formatted string with all items
 */
export function formatList<T>(
	items: T[],
	formatter: (item: T) => string,
	separator: string = "\n\n---\n\n",
): string {
	return items.map(formatter).join(separator);
}
