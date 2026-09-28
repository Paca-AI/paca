import { describe, expect, it } from "vitest";

import {
	buildExportFilename,
	tasksToCsv,
	type TaskExportColumnLabels,
	type TaskExportLookups,
} from "./export-tasks-csv";
import type { Task } from "./interaction-api";

const labels: TaskExportColumnLabels = {
	id: "ID",
	title: "Title",
	status: "Status",
	type: "Type",
	sprint: "Sprint",
	parent: "Parent",
	assignees: "Assignees",
	reporter: "Reporter",
	priority: "Priority",
	importance: "Importance",
	storyPoints: "Story Points",
	tags: "Tags",
	startDate: "Start Date",
	dueDate: "Due Date",
	createdAt: "Created",
	updatedAt: "Updated",
};

const lookups: TaskExportLookups = {
	statuses: [
		{
			id: "s1",
			project_id: "p1",
			name: "In Progress",
			position: 1,
			category: "inprogress",
			created_at: "",
			updated_at: "",
		},
	],
	taskTypes: [
		{
			id: "tt1",
			project_id: "p1",
			name: "Story",
			created_at: "",
			updated_at: "",
		},
	],
	members: [
		{
			id: "m1",
			project_id: "p1",
			user_id: "u1",
			project_role_id: "r1",
			username: "alice",
			full_name: "Alice A",
			role_name: "Member",
			description: "",
		},
	],
	sprints: [
		{
			id: "sp1",
			project_id: "p1",
			name: "Sprint 1",
			status: "active",
			created_at: "",
			updated_at: "",
		},
	],
	customFields: [
		{
			id: "cf1",
			project_id: "p1",
			field_key: "priority_note",
			display_name: "Priority Note",
			field_type: "text",
			options: [],
			is_required: false,
			created_at: "",
			updated_at: "",
		},
	],
	priorityLabel: (key) => key.replace("priority.", ""),
};

function makeTask(overrides: Partial<Task> = {}): Task {
	return {
		id: "t1",
		project_id: "p1",
		title: "Hello",
		task_number: 3,
		importance: 25,
		custom_fields: {},
		assignment_mode: "manual",
		created_at: "2026-09-25T10:00:00Z",
		updated_at: "2026-09-28T10:00:00Z",
		...overrides,
	};
}

describe("tasksToCsv", () => {
	it("emits a BOM, headers, and resolved name columns", () => {
		const csv = tasksToCsv(
			[
				makeTask({
					status_id: "s1",
					task_type_id: "tt1",
					sprint_id: "sp1",
					assignee_ids: ["m1"],
					reporter_id: "m1",
					story_points: 5,
					tags: ["a", "b"],
					start_date: "2026-09-01T00:00:00Z",
					due_date: "2026-09-30T00:00:00Z",
					custom_fields: { priority_note: "needs review" },
				}),
			],
			lookups,
			labels,
		);

		expect(csv.startsWith("\uFEFF")).toBe(true);
		expect(csv).toContain("ID,Title,Status,Type,Sprint");
		expect(csv).toContain("Priority Note");
		expect(csv).toContain("In Progress");
		expect(csv).toContain("Story");
		expect(csv).toContain("Sprint 1");
		expect(csv).toContain("Alice A");
		expect(csv).toContain("medium");
		expect(csv).toContain("needs review");
		expect(csv).toContain("2026-09-01");
	});

	it("escapes commas, quotes, and newlines in cells", () => {
		const csv = tasksToCsv(
			[makeTask({ title: 'Say "hi", please\nnow' })],
			lookups,
			labels,
		);
		expect(csv).toContain('"Say ""hi"", please\nnow"');
	});

	it("resolves parent task numbers from the exported set", () => {
		const parent = makeTask({ id: "parent", task_number: 1, title: "Epic" });
		const child = makeTask({
			id: "child",
			task_number: 2,
			title: "Child",
			parent_task_id: "parent",
		});
		const csv = tasksToCsv([parent, child], lookups, labels);
		const childLine = csv
			.split("\r\n")
			.find((line) => line.startsWith("2,Child"));
		expect(childLine).toBeDefined();
		// Parent column sits after Sprint (empty here)
		expect(childLine).toMatch(/^2,Child,,,,1,/);
	});
});

describe("buildExportFilename", () => {
	it("sanitizes the project slug and adds a date", () => {
		const name = buildExportFilename("PACA Enhancements!", new Date("2026-09-28T12:00:00Z"));
		expect(name).toBe("PACA-Enhancements-tasks-2026-09-28.csv");
	});
});
