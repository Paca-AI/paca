import { describe, expect, it, vi } from "vitest";

// Mock all domain tool modules so we test routing, not tool implementations.
vi.mock("../../tools/project-tools.js", () => ({
	getProjectTools: vi.fn(() => [
		{ name: "list_projects" },
		{ name: "get_project" },
	]),
	handleProjectTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/task-tools.js", () => ({
	getTaskTools: vi.fn(() => [
		{ name: "list_tasks" },
		{ name: "get_task" },
		{ name: "create_task" },
	]),
	handleTaskTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/sprint-tools.js", () => ({
	getSprintTools: vi.fn(() => [
		{ name: "list_sprints" },
		{ name: "complete_sprint" },
	]),
	handleSprintTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/filesystem-doc-tools.js", () => ({
	getFilesystemDocTools: vi.fn(() => [
		{ name: "list_docs" },
		{ name: "read_doc" },
	]),
	handleFilesystemDocTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/member-tools.js", () => ({
	getProjectMemberTools: vi.fn(() => [{ name: "list_project_members" }]),
	getProjectRoleTools: vi.fn(() => [{ name: "list_project_roles" }]),
	handleProjectMemberTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/task-type-tools.js", () => ({
	getTaskTypeTools: vi.fn(() => [{ name: "list_task_types" }]),
	getTaskStatusTools: vi.fn(() => [{ name: "list_task_statuses" }]),
	handleTaskTypeTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/view-tools.js", () => ({
	getViewTools: vi.fn(() => [{ name: "list_views" }, { name: "create_view" }]),
	getCustomFieldTools: vi.fn(() => [{ name: "list_custom_fields" }]),
	handleViewTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/attachment-tools.js", () => ({
	getAttachmentTools: vi.fn(() => [{ name: "list_task_attachments" }]),
	handleAttachmentTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/task-activity-tools.js", () => ({
	getTaskActivityTools: vi.fn(() => [
		{ name: "list_task_activities" },
		{ name: "add_task_comment" },
	]),
	handleTaskActivityTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/automation-tools.js", () => ({
	getAutomationTools: vi.fn(() => [
		{ name: "get_automation" },
		{ name: "create_automation" },
	]),
	handleAutomationTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/doc-activity-tools.js", () => ({
	getDocActivityTools: vi.fn(() => [
		{ name: "list_doc_activities" },
		{ name: "add_doc_comment" },
	]),
	handleDocActivityTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));
vi.mock("../../tools/repo-tools.js", () => ({
	getRepoTools: vi.fn(() => [
		{ name: "list_repositories" },
		{ name: "clone_repository" },
		{ name: "push_branch" },
	]),
	handleRepoTool: vi
		.fn()
		.mockResolvedValue({ content: [{ type: "text", text: "ok" }] }),
}));

import { handleAttachmentTool } from "../../tools/attachment-tools.js";
import { handleAutomationTool } from "../../tools/automation-tools.js";
import { handleDocActivityTool } from "../../tools/doc-activity-tools.js";
import { handleFilesystemDocTool } from "../../tools/filesystem-doc-tools.js";
import { getAllTools, handleToolCall } from "../../tools/index.js";
import { handleProjectMemberTool } from "../../tools/member-tools.js";
import { handleProjectTool } from "../../tools/project-tools.js";
import { handleRepoTool } from "../../tools/repo-tools.js";
import { handleSprintTool } from "../../tools/sprint-tools.js";
import { handleTaskActivityTool } from "../../tools/task-activity-tools.js";
import { handleTaskTool } from "../../tools/task-tools.js";
import { handleTaskTypeTool } from "../../tools/task-type-tools.js";
import { handleViewTool } from "../../tools/view-tools.js";

// Stub clients – these are only passed through to domain handlers (which are mocked)
const stubClients = {
	apiClient: {} as any,
	extendedClient: {} as any,
	viewsClient: {} as any,
	taskExtendedClient: {} as any,
	docClient: {} as any,
	automationClient: {} as any,
};

function makeRequest(name: string, args: Record<string, unknown> = {}) {
	return { method: "tools/call", params: { name, arguments: args } } as any;
}

// ---------------------------------------------------------------------------
// getAllTools
// ---------------------------------------------------------------------------

describe("getAllTools", () => {
	it("returns a non-empty array of tools", () => {
		const tools = getAllTools();
		expect(Array.isArray(tools)).toBe(true);
		expect(tools.length).toBeGreaterThan(0);
	});

	it("includes tools from every domain module", () => {
		const names = getAllTools().map((t) => t.name);
		expect(names).toContain("list_projects");
		expect(names).toContain("list_tasks");
		expect(names).toContain("list_sprints");
		expect(names).toContain("list_docs");
		expect(names).toContain("list_project_members");
		expect(names).toContain("list_task_types");
		expect(names).toContain("list_views");
		expect(names).toContain("list_task_attachments");
		expect(names).toContain("list_task_activities");
		expect(names).toContain("get_automation");
		expect(names).toContain("list_doc_activities");
		expect(names).toContain("list_repositories");
	});
});

// ---------------------------------------------------------------------------
// handleToolCall – routing
// ---------------------------------------------------------------------------

describe("handleToolCall – project tool routing", () => {
	it("routes list_projects to handleProjectTool", async () => {
		await handleToolCall(makeRequest("list_projects"), stubClients);
		expect(handleProjectTool).toHaveBeenCalledWith(
			"list_projects",
			{},
			stubClients.apiClient,
		);
	});

	it("routes get_project to handleProjectTool", async () => {
		await handleToolCall(
			makeRequest("get_project", { project_id: "p1" }),
			stubClients,
		);
		expect(handleProjectTool).toHaveBeenCalledWith(
			"get_project",
			{ project_id: "p1" },
			stubClients.apiClient,
		);
	});
});

describe("handleToolCall – task tool routing", () => {
	it("routes list_tasks to handleTaskTool", async () => {
		await handleToolCall(makeRequest("list_tasks"), stubClients);
		expect(handleTaskTool).toHaveBeenCalledWith(
			"list_tasks",
			{},
			stubClients.apiClient,
			stubClients.taskExtendedClient,
			stubClients.viewsClient,
		);
	});

	it("routes create_task to handleTaskTool", async () => {
		await handleToolCall(
			makeRequest("create_task", { title: "T" }),
			stubClients,
		);
		expect(handleTaskTool).toHaveBeenCalledWith(
			"create_task",
			{ title: "T" },
			stubClients.apiClient,
			stubClients.taskExtendedClient,
			stubClients.viewsClient,
		);
	});
});

describe("handleToolCall – sprint tool routing", () => {
	it("routes list_sprints to handleSprintTool", async () => {
		await handleToolCall(makeRequest("list_sprints"), stubClients);
		expect(handleSprintTool).toHaveBeenCalledWith(
			"list_sprints",
			{},
			stubClients.apiClient,
		);
	});

	it("routes complete_sprint to handleSprintTool", async () => {
		await handleToolCall(makeRequest("complete_sprint"), stubClients);
		expect(handleSprintTool).toHaveBeenCalledWith(
			"complete_sprint",
			{},
			stubClients.apiClient,
		);
	});
});

describe("handleToolCall – document tool routing", () => {
	it("routes list_docs to handleFilesystemDocTool", async () => {
		await handleToolCall(makeRequest("list_docs"), stubClients);
		expect(handleFilesystemDocTool).toHaveBeenCalledWith(
			"list_docs",
			{},
			stubClients.apiClient,
			stubClients.docClient,
		);
	});
});

describe("handleToolCall – member/role tool routing", () => {
	it("routes list_project_members to handleProjectMemberTool", async () => {
		await handleToolCall(makeRequest("list_project_members"), stubClients);
		expect(handleProjectMemberTool).toHaveBeenCalledWith(
			"list_project_members",
			{},
			stubClients.extendedClient,
		);
	});

	it("routes list_project_roles to handleProjectMemberTool", async () => {
		await handleToolCall(makeRequest("list_project_roles"), stubClients);
		expect(handleProjectMemberTool).toHaveBeenCalledWith(
			"list_project_roles",
			{},
			stubClients.extendedClient,
		);
	});
});

describe("handleToolCall – task type/status routing", () => {
	it("routes list_task_types to handleTaskTypeTool", async () => {
		await handleToolCall(makeRequest("list_task_types"), stubClients);
		expect(handleTaskTypeTool).toHaveBeenCalledWith(
			"list_task_types",
			{},
			stubClients.extendedClient,
		);
	});

	it("routes list_task_statuses to handleTaskTypeTool", async () => {
		await handleToolCall(makeRequest("list_task_statuses"), stubClients);
		expect(handleTaskTypeTool).toHaveBeenCalledWith(
			"list_task_statuses",
			{},
			stubClients.extendedClient,
		);
	});
});

describe("handleToolCall – view/custom-field routing", () => {
	it("routes list_views to handleViewTool", async () => {
		await handleToolCall(makeRequest("list_views"), stubClients);
		expect(handleViewTool).toHaveBeenCalledWith(
			"list_views",
			{},
			stubClients.viewsClient,
		);
	});

	it("routes list_custom_fields to handleViewTool", async () => {
		await handleToolCall(makeRequest("list_custom_fields"), stubClients);
		expect(handleViewTool).toHaveBeenCalledWith(
			"list_custom_fields",
			{},
			stubClients.viewsClient,
		);
	});
});

describe("handleToolCall – attachment routing", () => {
	it("routes list_task_attachments to handleAttachmentTool", async () => {
		await handleToolCall(makeRequest("list_task_attachments"), stubClients);
		expect(handleAttachmentTool).toHaveBeenCalledWith(
			"list_task_attachments",
			{},
			stubClients.viewsClient,
		);
	});
});

describe("handleToolCall – activity routing", () => {
	it("routes list_task_activities to handleTaskActivityTool", async () => {
		await handleToolCall(makeRequest("list_task_activities"), stubClients);
		expect(handleTaskActivityTool).toHaveBeenCalledWith(
			"list_task_activities",
			{},
			stubClients.taskExtendedClient,
		);
	});

	it("routes add_task_comment to handleTaskActivityTool", async () => {
		await handleToolCall(makeRequest("add_task_comment"), stubClients);
		expect(handleTaskActivityTool).toHaveBeenCalledWith(
			"add_task_comment",
			{},
			stubClients.taskExtendedClient,
		);
	});
});

describe("handleToolCall – automation tool routing", () => {
	it("routes get_automation to handleAutomationTool", async () => {
		await handleToolCall(makeRequest("get_automation"), stubClients);
		expect(handleAutomationTool).toHaveBeenCalledWith(
			"get_automation",
			{},
			stubClients.automationClient,
		);
	});

	it("routes create_automation to handleAutomationTool", async () => {
		await handleToolCall(makeRequest("create_automation"), stubClients);
		expect(handleAutomationTool).toHaveBeenCalledWith(
			"create_automation",
			{},
			stubClients.automationClient,
		);
	});

	it("routes update_automation to handleAutomationTool", async () => {
		await handleToolCall(makeRequest("update_automation"), stubClients);
		expect(handleAutomationTool).toHaveBeenCalledWith(
			"update_automation",
			{},
			stubClients.automationClient,
		);
	});

	it("routes delete_automation to handleAutomationTool", async () => {
		await handleToolCall(makeRequest("delete_automation"), stubClients);
		expect(handleAutomationTool).toHaveBeenCalledWith(
			"delete_automation",
			{},
			stubClients.automationClient,
		);
	});
});

describe("handleToolCall – doc activity routing", () => {
	it("routes list_doc_activities to handleDocActivityTool", async () => {
		await handleToolCall(makeRequest("list_doc_activities"), stubClients);
		expect(handleDocActivityTool).toHaveBeenCalledWith(
			"list_doc_activities",
			{},
			stubClients.docClient,
		);
	});

	it("routes add_doc_comment to handleDocActivityTool", async () => {
		await handleToolCall(makeRequest("add_doc_comment"), stubClients);
		expect(handleDocActivityTool).toHaveBeenCalledWith(
			"add_doc_comment",
			{},
			stubClients.docClient,
		);
	});
});

describe("handleToolCall – repo tool routing", () => {
	it("routes list_repositories to handleRepoTool with a default empty repoPluginIds", async () => {
		await handleToolCall(makeRequest("list_repositories"), stubClients);
		expect(handleRepoTool).toHaveBeenCalledWith(
			"list_repositories",
			{},
			stubClients.apiClient,
			[],
		);
	});

	it("forwards the repoPluginIds argument through to handleRepoTool", async () => {
		await handleToolCall(makeRequest("clone_repository"), stubClients, [
			"com.paca.github",
		]);
		expect(handleRepoTool).toHaveBeenCalledWith(
			"clone_repository",
			{},
			stubClients.apiClient,
			["com.paca.github"],
		);
	});

	it("routes push_branch to handleRepoTool", async () => {
		await handleToolCall(makeRequest("push_branch"), stubClients);
		expect(handleRepoTool).toHaveBeenCalledWith(
			"push_branch",
			{},
			stubClients.apiClient,
			[],
		);
	});
});

// ---------------------------------------------------------------------------
// handleToolCall – error handling
// ---------------------------------------------------------------------------

describe("handleToolCall – error handling", () => {
	it("returns isError response for an unknown tool name", async () => {
		const result = await handleToolCall(
			makeRequest("completely_unknown_tool"),
			stubClients,
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("Unknown tool");
	});

	it("returns isError response when a domain handler throws synchronously", async () => {
		// The try-catch in handleToolCall catches synchronous throws but not async rejections
		// (because handlers are called with `return`, not `return await`).
		vi.mocked(handleProjectTool).mockImplementationOnce(() => {
			throw new Error("Sync error from handler");
		});
		const result = await handleToolCall(
			makeRequest("list_projects"),
			stubClients,
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("Sync error from handler");
	});

	it("propagates rejection when a domain handler rejects asynchronously", async () => {
		vi.mocked(handleProjectTool).mockRejectedValueOnce(
			new Error("Async API down"),
		);
		await expect(
			handleToolCall(makeRequest("list_projects"), stubClients),
		).rejects.toThrow("Async API down");
	});
});
