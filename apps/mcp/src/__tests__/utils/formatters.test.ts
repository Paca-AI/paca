import { describe, expect, it, vi } from "vitest";

// Mock the converters module so tests don't require a live BlockNote/JSDOM instance.
// formatters.ts imports from "./converters.js" (relative to src/utils/), which
// resolves to the same absolute path as "../../utils/converters.js" from here.
vi.mock("../../utils/converters.js", () => ({
	blocknoteToMarkdown: vi.fn(() => "mocked markdown"),
}));

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
	TaskStatus,
	TaskType,
} from "../../types/index.js";
import {
	formatDocument,
	formatList,
	formatProject,
	formatSprint,
	formatTask,
	formatTaskDetail,
	formatToolError,
} from "../../utils/formatters.js";

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const baseTask: Task = {
	id: "task-1",
	project_id: "proj-1",
	title: "Fix the bug",
	task_number: 42,
	importance: 3,
	custom_fields: {},
	created_at: "2024-01-01T00:00:00Z",
	updated_at: "2024-01-02T00:00:00Z",
};

const baseProject: Project = {
	id: "proj-1",
	name: "My Project",
	description: "A test project",
	task_id_prefix: "MP",
	settings: {},
	created_at: "2024-01-01T00:00:00Z",
};

const baseSprint: Sprint = {
	id: "sprint-1",
	project_id: "proj-1",
	name: "Sprint 1",
	status: "active",
	created_at: "2024-01-01T00:00:00Z",
	updated_at: "2024-01-02T00:00:00Z",
};

const baseDocument: Document = {
	id: "doc-1",
	project_id: "proj-1",
	title: "Design Doc",
	content: null,
	position: 1,
	created_at: "2024-01-01T00:00:00Z",
	updated_at: "2024-01-02T00:00:00Z",
};

// ---------------------------------------------------------------------------
// formatTask
// ---------------------------------------------------------------------------

describe("formatTask", () => {
	it("includes task number and title in output", () => {
		const result = formatTask(baseTask);
		expect(result).toContain("Task #42: Fix the bug");
	});

	it("includes task id", () => {
		expect(formatTask(baseTask)).toContain("ID: task-1");
	});

	it("shows 'None' for missing optional fields", () => {
		const result = formatTask(baseTask);
		expect(result).toContain("Status: None");
		expect(result).toContain("Sprint: None");
		expect(result).toContain("Assignees: Unassigned");
		expect(result).toContain("Parent Task: None");
		expect(result).toContain("Story Points: None");
		expect(result).toContain("Tags: None");
	});

	it("shows 'No description' when description is null", () => {
		const result = formatTask({ ...baseTask, description: null });
		expect(result).toContain("No description");
	});

	it("calls blocknoteToMarkdown and includes its output when description is set", () => {
		const result = formatTask({
			...baseTask,
			description: [{ type: "paragraph" }],
		});
		expect(result).toContain("mocked markdown");
	});

	it("shows importance value", () => {
		expect(formatTask({ ...baseTask, importance: 5 })).toContain(
			"Importance: 5",
		);
	});

	it("shows story points when set", () => {
		expect(formatTask({ ...baseTask, story_points: 8 })).toContain(
			"Story Points: 8",
		);
	});

	it("shows tags when present", () => {
		const result = formatTask({ ...baseTask, tags: ["bug", "critical"] });
		expect(result).toContain("Tags: bug, critical");
	});

	it("shows start and due dates when set", () => {
		const result = formatTask({
			...baseTask,
			start_date: "2024-02-01",
			due_date: "2024-02-15",
		});
		expect(result).toContain("Start Date: 2024-02-01");
		expect(result).toContain("Due Date: 2024-02-15");
	});

	it("shows created and updated timestamps", () => {
		const result = formatTask(baseTask);
		expect(result).toContain("Created: 2024-01-01T00:00:00Z");
		expect(result).toContain("Updated: 2024-01-02T00:00:00Z");
	});
});

// ---------------------------------------------------------------------------
// formatProject
// ---------------------------------------------------------------------------

describe("formatProject", () => {
	it("includes project name", () => {
		expect(formatProject(baseProject)).toContain("Project: My Project");
	});

	it("includes project id", () => {
		expect(formatProject(baseProject)).toContain("ID: proj-1");
	});

	it("includes description", () => {
		expect(formatProject(baseProject)).toContain("Description: A test project");
	});

	it("shows 'No description' when description is falsy", () => {
		const result = formatProject({ ...baseProject, description: "" });
		expect(result).toContain("Description: No description");
	});

	it("includes task_id_prefix", () => {
		expect(formatProject(baseProject)).toContain("Task ID Prefix: MP");
	});

	it("shows 'None' when task_id_prefix is empty", () => {
		expect(formatProject({ ...baseProject, task_id_prefix: "" })).toContain(
			"Task ID Prefix: None",
		);
	});

	it("includes created_at", () => {
		expect(formatProject(baseProject)).toContain(
			"Created: 2024-01-01T00:00:00Z",
		);
	});

	it("shows 'Unknown' for missing created_by", () => {
		expect(formatProject({ ...baseProject, created_by: undefined })).toContain(
			"Created by: Unknown",
		);
	});
});

// ---------------------------------------------------------------------------
// formatSprint
// ---------------------------------------------------------------------------

describe("formatSprint", () => {
	it("includes sprint name", () => {
		expect(formatSprint(baseSprint)).toContain("Sprint: Sprint 1");
	});

	it("includes sprint id", () => {
		expect(formatSprint(baseSprint)).toContain("ID: sprint-1");
	});

	it("includes project_id", () => {
		expect(formatSprint(baseSprint)).toContain("Project: proj-1");
	});

	it("shows 'None' for missing optional fields", () => {
		const result = formatSprint(baseSprint);
		expect(result).toContain("Start Date: None");
		expect(result).toContain("End Date: None");
		expect(result).toContain("Goal: None");
	});

	it("includes status", () => {
		expect(formatSprint(baseSprint)).toContain("Status: active");
	});

	it("shows start and end dates when set", () => {
		const result = formatSprint({
			...baseSprint,
			start_date: "2024-01-01",
			end_date: "2024-01-14",
		});
		expect(result).toContain("Start Date: 2024-01-01");
		expect(result).toContain("End Date: 2024-01-14");
	});

	it("shows goal when set", () => {
		expect(formatSprint({ ...baseSprint, goal: "Ship feature X" })).toContain(
			"Goal: Ship feature X",
		);
	});
});

// ---------------------------------------------------------------------------
// formatDocument
// ---------------------------------------------------------------------------

describe("formatDocument", () => {
	it("includes document title", () => {
		expect(formatDocument(baseDocument)).toContain("Document: Design Doc");
	});

	it("includes document id", () => {
		expect(formatDocument(baseDocument)).toContain("ID: doc-1");
	});

	it("shows 'No content' when content is null", () => {
		expect(formatDocument(baseDocument)).toContain("No content");
	});

	it("calls blocknoteToMarkdown and shows output when content is present", () => {
		const result = formatDocument({
			...baseDocument,
			content: [{ type: "paragraph" }],
		});
		expect(result).toContain("mocked markdown");
	});

	it("shows 'None' for missing folder_id", () => {
		expect(formatDocument(baseDocument)).toContain("Folder: None");
	});

	it("shows position", () => {
		expect(formatDocument(baseDocument)).toContain("Position: 1");
	});

	it("shows 'Unknown' for missing created_by and updated_by", () => {
		const result = formatDocument(baseDocument);
		expect(result).toContain("Created by: Unknown");
		expect(result).toContain("Updated by: Unknown");
	});
});

// ---------------------------------------------------------------------------
// formatList
// ---------------------------------------------------------------------------

describe("formatList", () => {
	it("returns empty string for an empty array", () => {
		expect(formatList([], (x) => x as string)).toBe("");
	});

	it("formats a single item without a separator", () => {
		expect(formatList(["a"], (x) => x.toUpperCase())).toBe("A");
	});

	it("joins multiple items with the default separator", () => {
		const result = formatList(["a", "b"], (x) => x.toUpperCase());
		expect(result).toBe("A\n\n---\n\nB");
	});

	it("uses a custom separator when provided", () => {
		const result = formatList([1, 2, 3], (n) => String(n), " | ");
		expect(result).toBe("1 | 2 | 3");
	});

	it("applies the formatter to every item", () => {
		const formatter = vi.fn((x: string) => x.toUpperCase());
		const result = formatList(["x", "y"], formatter);
		// Array.map passes (item, index, array) – check call count and output
		expect(formatter).toHaveBeenCalledTimes(2);
		expect(result).toContain("X");
		expect(result).toContain("Y");
	});
});

// ---------------------------------------------------------------------------
// formatTaskDetail
// ---------------------------------------------------------------------------

describe("formatTaskDetail", () => {
	it("includes task number and title as a heading", () => {
		const result = formatTaskDetail(baseTask);
		expect(result).toContain("# Task 42: Fix the bug");
	});

	it("includes task id", () => {
		expect(formatTaskDetail(baseTask)).toContain("**ID:** task-1");
	});

	it("includes project task_id_prefix in heading when project is provided", () => {
		const project: Project = { ...baseProject };
		const result = formatTaskDetail(baseTask, project);
		expect(result).toContain("MP-42");
	});

	it("shows 'No description' when description is null", () => {
		expect(formatTaskDetail({ ...baseTask, description: null })).toContain(
			"No description",
		);
	});

	it("calls blocknoteToMarkdown when description is set", () => {
		formatTaskDetail({ ...baseTask, description: [{ type: "paragraph" }] });
		// The mock is already set up to return "mocked markdown"
		const result = formatTaskDetail({ ...baseTask, description: [{}] });
		expect(result).toContain("mocked markdown");
	});

	it("shows status name when status object is provided", () => {
		const status: TaskStatus = {
			id: "s1",
			project_id: "p1",
			name: "In Progress",
			position: 1,
			category: "inprogress",
			created_at: "2024-01-01T00:00:00Z",
			updated_at: "2024-01-01T00:00:00Z",
		};
		const result = formatTaskDetail(
			{ ...baseTask, status_id: "s1" },
			undefined,
			status,
		);
		expect(result).toContain("In Progress");
	});

	it("falls back to status_id when no status object is given", () => {
		const result = formatTaskDetail({ ...baseTask, status_id: "s-fallback" });
		expect(result).toContain("s-fallback");
	});

	it("shows task type name when taskType object is provided", () => {
		const taskType: TaskType = {
			id: "ty1",
			project_id: "p1",
			name: "Story",
			created_at: "2024-01-01T00:00:00Z",
			updated_at: "2024-01-01T00:00:00Z",
		};
		const result = formatTaskDetail(
			{ ...baseTask, task_type_id: "ty1" },
			undefined,
			undefined,
			taskType,
		);
		expect(result).toContain("Story");
	});

	it("shows sprint name when sprint object is provided", () => {
		const sprint: Sprint = { ...baseSprint };
		const result = formatTaskDetail(
			{ ...baseTask, sprint_id: "sprint-1" },
			undefined,
			undefined,
			undefined,
			sprint,
		);
		expect(result).toContain("Sprint 1");
	});

	it("shows assignee full name when assignee is provided", () => {
		const assignee: ProjectMember = {
			id: "m1",
			project_id: "p1",
			user_id: "u1",
			project_role_id: "r1",
			username: "alice",
			full_name: "Alice Smith",
			role_name: "Developer",
		};
		const result = formatTaskDetail(
			{ ...baseTask, assignee_ids: ["u1"] },
			undefined,
			undefined,
			undefined,
			undefined,
			[assignee],
		);
		expect(result).toContain("Alice Smith");
		expect(result).toContain("@alice");
	});

	it("shows parent task title when parentTask is provided", () => {
		const parentTask = {
			...baseTask,
			id: "parent-1",
			title: "Epic Task",
			task_number: 10,
		};
		const result = formatTaskDetail(
			{ ...baseTask, parent_task_id: "parent-1" },
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			parentTask,
		);
		expect(result).toContain("Epic Task");
		expect(result).toContain("#10");
	});

	it("includes subtasks section when subtasks are provided", () => {
		const subtask = {
			...baseTask,
			id: "sub-1",
			title: "Subtask",
			task_number: 2,
		};
		const result = formatTaskDetail(
			baseTask,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			[subtask],
		);
		expect(result).toContain("## Subtasks");
		expect(result).toContain("Subtask");
	});

	it("includes attachments section when attachments are provided", () => {
		const attachment: Attachment = {
			id: "att-1",
			task_id: "t1",
			file: {
				id: "f1",
				file_name: "screenshot.png",
				file_size: 2048,
				content_type: "image/png",
				storage_key: "key",
			},
			created_by: "u1",
			created_at: "2024-01-01T00:00:00Z",
		};
		const result = formatTaskDetail(
			baseTask,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			[attachment],
		);
		expect(result).toContain("## Attachments");
		expect(result).toContain("screenshot.png");
		expect(result).toContain("att-1");
	});

	it("includes custom fields section when customFields are provided", () => {
		const field: CustomFieldDefinition = {
			id: "cf1",
			project_id: "p1",
			field_key: "priority",
			display_name: "Priority",
			field_type: "select",
			options: ["low", "high"],
			is_required: false,
			created_at: "2024-01-01T00:00:00Z",
			updated_at: "2024-01-01T00:00:00Z",
		};
		const result = formatTaskDetail(
			{ ...baseTask, custom_fields: { priority: "high" } },
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			[field],
		);
		expect(result).toContain("## Custom Fields");
		expect(result).toContain("Priority");
		expect(result).toContain("high");
	});

	it("formats boolean custom field values as strings", () => {
		const field: CustomFieldDefinition = {
			id: "cf2",
			project_id: "p1",
			field_key: "urgent",
			display_name: "Urgent",
			field_type: "boolean",
			options: [],
			is_required: false,
			created_at: "2024-01-01T00:00:00Z",
			updated_at: "2024-01-01T00:00:00Z",
		};
		const result = formatTaskDetail(
			{ ...baseTask, custom_fields: { urgent: true } },
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			[field],
		);
		expect(result).toContain("true");
	});

	it("includes activities section when activities are provided", () => {
		const activity: TaskActivity = {
			id: "act-1",
			task_id: "t1",
			actor_id: "u1",
			actor_name: "Alice",
			actor_username: "alice",
			activity_type: "status_change",
			content: null,
			created_at: "2024-01-01T00:00:00Z",
			updated_at: "2024-01-01T00:00:00Z",
		};
		const result = formatTaskDetail(
			baseTask,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			[activity],
		);
		expect(result).toContain("## Activities");
		expect(result).toContain("status_change");
		expect(result).toContain("Alice");
	});

	it("includes tags when present", () => {
		const result = formatTaskDetail({
			...baseTask,
			tags: ["urgent", "backend"],
		});
		expect(result).toContain("urgent");
		expect(result).toContain("backend");
	});

	it("shows 'None' for tags when empty", () => {
		const result = formatTaskDetail({ ...baseTask, tags: [] });
		expect(result).toContain("**Tags:** None");
	});

	it("shows start and due dates when set", () => {
		const result = formatTaskDetail({
			...baseTask,
			start_date: "2024-03-01",
			due_date: "2024-03-31",
		});
		expect(result).toContain("**Start Date:** 2024-03-01");
		expect(result).toContain("**Due Date:** 2024-03-31");
	});

	it("omits start/due date lines when not set", () => {
		const result = formatTaskDetail(baseTask);
		expect(result).not.toContain("**Start Date:**");
		expect(result).not.toContain("**Due Date:**");
	});
});

// ---------------------------------------------------------------------------
// formatToolError
// ---------------------------------------------------------------------------

describe("formatToolError", () => {
	it("returns a plain Error's message unchanged", () => {
		expect(formatToolError(new Error("boom"))).toBe("boom");
	});

	it("falls back to a generic message for a non-Error thrown value", () => {
		expect(formatToolError("oops")).toBe("Unknown error");
	});

	it("collapses ZodError issues that differ only by array index into one deduped, counted line", () => {
		const error = new ZodError([
			{
				code: "invalid_type",
				expected: "number",
				received: "undefined",
				path: ["nodes", "set", 0, "posX"],
				message: "Required",
			} as any,
			{
				code: "invalid_type",
				expected: "number",
				received: "undefined",
				path: ["nodes", "set", 1, "posX"],
				message: "Required",
			} as any,
			{
				code: "invalid_type",
				expected: "number",
				received: "undefined",
				path: ["nodes", "set", 0, "posY"],
				message: "Required",
			} as any,
		]);
		const formatted = formatToolError(error);
		expect(formatted).toBe(
			"Invalid arguments:\nnodes.set.*.posX: Required (x2)\nnodes.set.*.posY: Required",
		);
	});

	it("surfaces an unrecognized-key issue distinctly from missing-field issues", () => {
		const error = new ZodError([
			{
				code: "invalid_type",
				expected: "number",
				received: "undefined",
				path: ["nodes", "set", 0, "posX"],
				message: "Required",
			} as any,
			{
				code: "unrecognized_keys",
				keys: ["position"],
				path: ["nodes", "set", 0],
				message: "Unrecognized key(s) in object: 'position'",
			} as any,
		]);
		const formatted = formatToolError(error);
		expect(formatted).toContain(
			"nodes.set.*: Unrecognized key(s) in object: 'position'",
		);
		expect(formatted).toContain("nodes.set.*.posX: Required");
	});

	it("keeps a single issue's path unmodified (no trailing/leading dots)", () => {
		const error = new ZodError([
			{
				code: "invalid_type",
				expected: "string",
				received: "undefined",
				path: ["name"],
				message: "Required",
			} as any,
		]);
		expect(formatToolError(error)).toBe("Invalid arguments:\nname: Required");
	});
});
