import { describe, expect, it, vi } from "vitest";

vi.mock("../../utils/index.js", () => ({
	formatList: vi.fn((items: any[], fn: any) => items.map(fn).join("---")),
}));

import {
	getCustomFieldTools,
	getViewTools,
	handleViewTool,
} from "../../tools/view-tools.js";

const view = {
	id: "v1",
	project_id: "p1",
	name: "Board View",
	view_type: "board" as const,
	context: "sprint",
	sprint_id: null,
	position: 0,
	created_at: "2024-01-01T00:00:00Z",
	updated_at: "2024-01-01T00:00:00Z",
};

const customField = {
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

function makeClient(overrides: Record<string, any> = {}) {
	return {
		listViews: vi.fn().mockResolvedValue([view]),
		createView: vi.fn().mockResolvedValue(view),
		reorderViews: vi.fn().mockResolvedValue(undefined),
		getView: vi.fn().mockResolvedValue(view),
		updateView: vi.fn().mockResolvedValue({ ...view, name: "Updated View" }),
		deleteView: vi.fn().mockResolvedValue(undefined),
		listTaskPositions: vi
			.fn()
			.mockResolvedValue([{ task_id: "t1", position: 0 }]),
		bulkMoveTasks: vi.fn().mockResolvedValue(undefined),
		listCustomFieldDefinitions: vi.fn().mockResolvedValue([customField]),
		createCustomFieldDefinition: vi.fn().mockResolvedValue(customField),
		getCustomFieldDefinition: vi.fn().mockResolvedValue(customField),
		updateCustomFieldDefinition: vi.fn().mockResolvedValue(customField),
		deleteCustomFieldDefinition: vi.fn().mockResolvedValue(undefined),
		...overrides,
	} as any;
}

// ---------------------------------------------------------------------------
// getViewTools / getCustomFieldTools
// ---------------------------------------------------------------------------

describe("getViewTools", () => {
	it("returns 9 view tools", () => {
		expect(getViewTools()).toHaveLength(9);
	});
});

describe("getCustomFieldTools", () => {
	it("returns 5 custom field tools", () => {
		expect(getCustomFieldTools()).toHaveLength(5);
	});
});

// ---------------------------------------------------------------------------
// list_views
// ---------------------------------------------------------------------------

describe("handleViewTool – list_views", () => {
	it("calls client.listViews with projectId, defaults context to 'backlog'", async () => {
		const client = makeClient();
		await handleViewTool("list_views", { projectId: "p1" }, client);
		expect(client.listViews).toHaveBeenCalledWith("p1", "backlog", undefined);
	});

	it("uses provided context instead of default", async () => {
		const client = makeClient();
		await handleViewTool(
			"list_views",
			{ projectId: "p1", context: "sprint", sprintId: "s1" },
			client,
		);
		expect(client.listViews).toHaveBeenCalledWith("p1", "sprint", "s1");
	});

	it("includes 'Views:' header in response", async () => {
		const result = await handleViewTool(
			"list_views",
			{ projectId: "p1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Views:");
		expect(result.content[0].text).toContain("Board View");
	});
});

// ---------------------------------------------------------------------------
// create_view
// ---------------------------------------------------------------------------

describe("handleViewTool – create_view", () => {
	it("calls client.createView with body input and context/sprintId as query params", async () => {
		const client = makeClient();
		await handleViewTool(
			"create_view",
			{
				projectId: "p1",
				name: "Board View",
				context: "sprint",
				viewType: "board",
			},
			client,
		);
		expect(client.createView).toHaveBeenCalledWith(
			"p1",
			{
				name: "Board View",
				context: "sprint",
				view_type: "board",
				sprint_id: null,
			},
			"sprint", // context as query param
			undefined, // sprintId as query param
		);
	});

	it("passes sprintId in both body and query param when provided", async () => {
		const client = makeClient();
		await handleViewTool(
			"create_view",
			{
				projectId: "p1",
				name: "V",
				context: "sprint",
				viewType: "board",
				sprintId: "s1",
			},
			client,
		);
		expect(client.createView).toHaveBeenCalledWith(
			"p1",
			expect.objectContaining({ sprint_id: "s1" }),
			"sprint",
			"s1",
		);
	});

	it("includes 'created successfully' in response", async () => {
		const result = await handleViewTool(
			"create_view",
			{ projectId: "p1", name: "V", context: "sprint", viewType: "board" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("created successfully");
	});

	it("passes pluginManifestId/pluginComponent through as config.plugin_manifest_id/plugin_component for viewType 'plugin'", async () => {
		const client = makeClient();
		await handleViewTool(
			"create_view",
			{
				projectId: "p1",
				name: "Dashboard",
				context: "sprint",
				viewType: "plugin",
				pluginManifestId: "com.paca.dashboard",
				pluginComponent: "DashboardIntegrationView",
			},
			client,
		);
		expect(client.createView).toHaveBeenCalledWith(
			"p1",
			expect.objectContaining({
				view_type: "plugin",
				config: {
					plugin_manifest_id: "com.paca.dashboard",
					plugin_component: "DashboardIntegrationView",
				},
			}),
			"sprint",
			undefined,
		);
	});

	it("rejects viewType 'plugin' without pluginManifestId/pluginComponent", async () => {
		await expect(
			handleViewTool(
				"create_view",
				{
					projectId: "p1",
					name: "Dashboard",
					context: "sprint",
					viewType: "plugin",
				},
				makeClient(),
			),
		).rejects.toThrow();
	});

	it("rejects pluginManifestId provided without pluginComponent", async () => {
		await expect(
			handleViewTool(
				"create_view",
				{
					projectId: "p1",
					name: "V",
					context: "sprint",
					viewType: "board",
					pluginManifestId: "com.paca.dashboard",
				},
				makeClient(),
			),
		).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// reorder_views
// ---------------------------------------------------------------------------

describe("handleViewTool – reorder_views", () => {
	it("calls client.reorderViews with projectId, view_ids, context and sprintId as query params", async () => {
		const client = makeClient();
		await handleViewTool(
			"reorder_views",
			{ projectId: "p1", viewIds: ["v2", "v1"], context: "backlog" },
			client,
		);
		expect(client.reorderViews).toHaveBeenCalledWith(
			"p1",
			{ view_ids: ["v2", "v1"] },
			"backlog",
			undefined,
		);
	});

	it("passes sprintId as query param when reordering sprint views", async () => {
		const client = makeClient();
		await handleViewTool(
			"reorder_views",
			{
				projectId: "p1",
				viewIds: ["v2", "v1"],
				context: "sprint",
				sprintId: "s1",
			},
			client,
		);
		expect(client.reorderViews).toHaveBeenCalledWith(
			"p1",
			{ view_ids: ["v2", "v1"] },
			"sprint",
			"s1",
		);
	});

	it("includes 'reordered successfully' in response", async () => {
		const result = await handleViewTool(
			"reorder_views",
			{ projectId: "p1", viewIds: ["v1"] },
			makeClient(),
		);
		expect(result.content[0].text).toContain("reordered successfully");
	});
});

// ---------------------------------------------------------------------------
// get_view
// ---------------------------------------------------------------------------

describe("handleViewTool – get_view", () => {
	it("calls client.getView with projectId and viewId", async () => {
		const client = makeClient();
		await handleViewTool("get_view", { projectId: "p1", viewId: "v1" }, client);
		expect(client.getView).toHaveBeenCalledWith("p1", "v1");
	});

	it("includes view name in response", async () => {
		const result = await handleViewTool(
			"get_view",
			{ projectId: "p1", viewId: "v1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Board View");
	});

	it("includes the plugin binding when the view's config has one", async () => {
		const pluginView = {
			...view,
			view_type: "plugin",
			config: {
				plugin_manifest_id: "com.paca.dashboard",
				plugin_component: "DashboardIntegrationView",
			},
		};
		const client = makeClient({
			getView: vi.fn().mockResolvedValue(pluginView),
		});
		const result = await handleViewTool(
			"get_view",
			{ projectId: "p1", viewId: "v1" },
			client,
		);
		expect(result.content[0].text).toContain(
			"Plugin: com.paca.dashboard (component: DashboardIntegrationView)",
		);
	});

	it("omits the plugin line when the view's config has no plugin binding", async () => {
		const result = await handleViewTool(
			"get_view",
			{ projectId: "p1", viewId: "v1" },
			makeClient(),
		);
		expect(result.content[0].text).not.toContain("Plugin:");
	});
});

// ---------------------------------------------------------------------------
// update_view
// ---------------------------------------------------------------------------

describe("handleViewTool – update_view", () => {
	it("calls client.updateView with projectId, viewId, and mapped fields", async () => {
		const client = makeClient();
		await handleViewTool(
			"update_view",
			{ projectId: "p1", viewId: "v1", name: "New Name" },
			client,
		);
		expect(client.updateView).toHaveBeenCalledWith("p1", "v1", {
			name: "New Name",
			context: undefined,
			view_type: undefined,
			sprint_id: null,
		});
	});

	it("includes 'updated successfully' in response", async () => {
		const result = await handleViewTool(
			"update_view",
			{ projectId: "p1", viewId: "v1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("updated successfully");
	});

	it("passes pluginManifestId/pluginComponent through as config.plugin_manifest_id/plugin_component", async () => {
		const client = makeClient();
		await handleViewTool(
			"update_view",
			{
				projectId: "p1",
				viewId: "v1",
				viewType: "plugin",
				pluginManifestId: "com.paca.dashboard",
				pluginComponent: "DashboardIntegrationView",
			},
			client,
		);
		expect(client.updateView).toHaveBeenCalledWith(
			"p1",
			"v1",
			expect.objectContaining({
				view_type: "plugin",
				config: {
					plugin_manifest_id: "com.paca.dashboard",
					plugin_component: "DashboardIntegrationView",
				},
			}),
		);
	});

	it("passes explicit nulls through to clear an existing plugin binding", async () => {
		const client = makeClient();
		await handleViewTool(
			"update_view",
			{
				projectId: "p1",
				viewId: "v1",
				viewType: "table",
				pluginManifestId: null,
				pluginComponent: null,
			},
			client,
		);
		expect(client.updateView).toHaveBeenCalledWith(
			"p1",
			"v1",
			expect.objectContaining({
				view_type: "table",
				config: { plugin_manifest_id: null, plugin_component: null },
			}),
		);
	});

	it("rejects viewType 'plugin' without pluginManifestId/pluginComponent", async () => {
		await expect(
			handleViewTool(
				"update_view",
				{ projectId: "p1", viewId: "v1", viewType: "plugin" },
				makeClient(),
			),
		).rejects.toThrow();
	});

	it("rejects pluginComponent provided without pluginManifestId", async () => {
		await expect(
			handleViewTool(
				"update_view",
				{
					projectId: "p1",
					viewId: "v1",
					pluginComponent: "DashboardIntegrationView",
				},
				makeClient(),
			),
		).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// delete_view
// ---------------------------------------------------------------------------

describe("handleViewTool – delete_view", () => {
	it("calls client.deleteView with projectId and viewId", async () => {
		const client = makeClient();
		await handleViewTool(
			"delete_view",
			{ projectId: "p1", viewId: "v1" },
			client,
		);
		expect(client.deleteView).toHaveBeenCalledWith("p1", "v1");
	});

	it("includes 'deleted successfully' and viewId in response", async () => {
		const result = await handleViewTool(
			"delete_view",
			{ projectId: "p1", viewId: "v1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("deleted successfully");
		expect(result.content[0].text).toContain("v1");
	});
});

// ---------------------------------------------------------------------------
// list_task_positions
// ---------------------------------------------------------------------------

describe("handleViewTool – list_task_positions", () => {
	it("calls client.listTaskPositions with projectId and viewId", async () => {
		const client = makeClient();
		await handleViewTool(
			"list_task_positions",
			{ projectId: "p1", viewId: "v1" },
			client,
		);
		expect(client.listTaskPositions).toHaveBeenCalledWith("p1", "v1");
	});

	it("returns JSON of positions in response", async () => {
		const result = await handleViewTool(
			"list_task_positions",
			{ projectId: "p1", viewId: "v1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Task Positions:");
		expect(result.content[0].text).toContain("t1");
	});
});

// ---------------------------------------------------------------------------
// bulk_move_tasks
// ---------------------------------------------------------------------------

describe("handleViewTool – bulk_move_tasks", () => {
	it("calls client.bulkMoveTasks with mapped input", async () => {
		const client = makeClient();
		await handleViewTool(
			"bulk_move_tasks",
			{ projectId: "p1", viewId: "v1", taskId: "t1", targetViewId: "v2" },
			client,
		);
		expect(client.bulkMoveTasks).toHaveBeenCalledWith("p1", "v1", {
			task_id: "t1",
			target_view_id: "v2",
			target_status_id: null,
			target_position: undefined,
		});
	});

	it("includes 'moved successfully' in response", async () => {
		const result = await handleViewTool(
			"bulk_move_tasks",
			{ projectId: "p1", viewId: "v1", taskId: "t1", targetViewId: "v2" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("moved successfully");
	});
});

// ---------------------------------------------------------------------------
// move_task
// ---------------------------------------------------------------------------

describe("handleViewTool – move_task", () => {
	it("calls client.bulkMoveTasks (same underlying method as bulk_move_tasks)", async () => {
		const client = makeClient();
		await handleViewTool(
			"move_task",
			{
				projectId: "p1",
				viewId: "v1",
				taskId: "t1",
				targetViewId: "v2",
				targetPosition: 3,
			},
			client,
		);
		expect(client.bulkMoveTasks).toHaveBeenCalledWith("p1", "v1", {
			task_id: "t1",
			target_view_id: "v2",
			target_status_id: null,
			target_position: 3,
		});
	});

	it("includes 'moved successfully' in response", async () => {
		const result = await handleViewTool(
			"move_task",
			{ projectId: "p1", viewId: "v1", taskId: "t1", targetViewId: "v2" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("moved successfully");
	});
});

// ---------------------------------------------------------------------------
// list_custom_fields
// ---------------------------------------------------------------------------

describe("handleViewTool – list_custom_fields", () => {
	it("calls client.listCustomFieldDefinitions with projectId", async () => {
		const client = makeClient();
		await handleViewTool("list_custom_fields", { projectId: "p1" }, client);
		expect(client.listCustomFieldDefinitions).toHaveBeenCalledWith("p1");
	});

	it("includes 'Custom Fields:' header and field display name in response", async () => {
		const result = await handleViewTool(
			"list_custom_fields",
			{ projectId: "p1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Custom Fields:");
		expect(result.content[0].text).toContain("Priority");
	});
});

// ---------------------------------------------------------------------------
// create_custom_field
// ---------------------------------------------------------------------------

describe("handleViewTool – create_custom_field", () => {
	it("calls client.createCustomFieldDefinition with mapped input", async () => {
		const client = makeClient();
		await handleViewTool(
			"create_custom_field",
			{
				projectId: "p1",
				fieldKey: "priority",
				displayName: "Priority",
				fieldType: "select",
				options: ["low", "high"],
				isRequired: true,
			},
			client,
		);
		expect(client.createCustomFieldDefinition).toHaveBeenCalledWith("p1", {
			field_key: "priority",
			display_name: "Priority",
			field_type: "select",
			options: ["low", "high"],
			is_required: true,
		});
	});

	it("includes 'created successfully' in response", async () => {
		const result = await handleViewTool(
			"create_custom_field",
			{ projectId: "p1", fieldKey: "k", displayName: "K", fieldType: "text" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("created successfully");
	});
});

// ---------------------------------------------------------------------------
// get_custom_field
// ---------------------------------------------------------------------------

describe("handleViewTool – get_custom_field", () => {
	it("calls client.getCustomFieldDefinition with projectId and fieldId", async () => {
		const client = makeClient();
		await handleViewTool(
			"get_custom_field",
			{ projectId: "p1", fieldId: "cf1" },
			client,
		);
		expect(client.getCustomFieldDefinition).toHaveBeenCalledWith("p1", "cf1");
	});

	it("includes field display name in response", async () => {
		const result = await handleViewTool(
			"get_custom_field",
			{ projectId: "p1", fieldId: "cf1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Priority");
	});
});

// ---------------------------------------------------------------------------
// update_custom_field
// ---------------------------------------------------------------------------

describe("handleViewTool – update_custom_field", () => {
	it("calls client.updateCustomFieldDefinition with projectId, fieldId, and mapped input", async () => {
		const client = makeClient();
		await handleViewTool(
			"update_custom_field",
			{ projectId: "p1", fieldId: "cf1", displayName: "New Priority" },
			client,
		);
		expect(client.updateCustomFieldDefinition).toHaveBeenCalledWith(
			"p1",
			"cf1",
			{
				display_name: "New Priority",
				field_type: undefined,
				options: undefined,
				is_required: undefined,
			},
		);
	});

	it("includes 'updated successfully' in response", async () => {
		const result = await handleViewTool(
			"update_custom_field",
			{ projectId: "p1", fieldId: "cf1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("updated successfully");
	});
});

// ---------------------------------------------------------------------------
// delete_custom_field
// ---------------------------------------------------------------------------

describe("handleViewTool – delete_custom_field", () => {
	it("calls client.deleteCustomFieldDefinition with projectId and fieldId", async () => {
		const client = makeClient();
		await handleViewTool(
			"delete_custom_field",
			{ projectId: "p1", fieldId: "cf1" },
			client,
		);
		expect(client.deleteCustomFieldDefinition).toHaveBeenCalledWith(
			"p1",
			"cf1",
		);
	});

	it("includes 'deleted successfully' and fieldId in response", async () => {
		const result = await handleViewTool(
			"delete_custom_field",
			{ projectId: "p1", fieldId: "cf1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("deleted successfully");
		expect(result.content[0].text).toContain("cf1");
	});
});

// ---------------------------------------------------------------------------
// unknown tool
// ---------------------------------------------------------------------------

describe("handleViewTool – unknown tool", () => {
	it("throws for an unknown tool name", async () => {
		await expect(handleViewTool("unknown", {}, makeClient())).rejects.toThrow();
	});
});
