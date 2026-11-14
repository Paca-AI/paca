import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIViewsClient } from "../api/index.js";
import type { ViewConfig } from "../types/index.js";
import { formatList } from "../utils/index.js";

const ListViewsSchema = z.object({
	projectId: z.string(),
	context: z.string().optional(),
	sprintId: z.string().optional(),
});

const CreateViewSchema = z
	.object({
		projectId: z.string(),
		name: z.string(),
		context: z.string(),
		viewType: z.string(),
		sprintId: z.string().optional(),
		pluginManifestId: z.string().optional(),
		pluginComponent: z.string().optional(),
	})
	.refine(
		(data) =>
			(data.pluginManifestId === undefined) ===
			(data.pluginComponent === undefined),
		{
			message: "pluginManifestId and pluginComponent must be provided together",
			path: ["pluginComponent"],
		},
	)
	.refine(
		(data) =>
			data.viewType !== "plugin" ||
			(!!data.pluginManifestId && !!data.pluginComponent),
		{
			message:
				"pluginManifestId and pluginComponent are required when viewType is 'plugin'",
			path: ["viewType"],
		},
	);

const ReorderViewsSchema = z.object({
	projectId: z.string(),
	viewIds: z.array(z.string()),
	context: z.string().optional(),
	sprintId: z.string().optional(),
});

const GetViewSchema = z.object({
	projectId: z.string(),
	viewId: z.string(),
});

const UpdateViewSchema = z
	.object({
		projectId: z.string(),
		viewId: z.string(),
		name: z.string().optional(),
		context: z.string().optional(),
		viewType: z.string().optional(),
		sprintId: z.string().optional(),
		// null explicitly clears an existing plugin binding (e.g. when
		// changing viewType away from "plugin"); undefined leaves it untouched.
		pluginManifestId: z.string().nullable().optional(),
		pluginComponent: z.string().nullable().optional(),
	})
	.refine(
		(data) =>
			(data.pluginManifestId === undefined) ===
			(data.pluginComponent === undefined),
		{
			message: "pluginManifestId and pluginComponent must be provided together",
			path: ["pluginComponent"],
		},
	)
	.refine(
		(data) =>
			data.viewType !== "plugin" ||
			(!!data.pluginManifestId && !!data.pluginComponent),
		{
			message:
				"pluginManifestId and pluginComponent are required when viewType is 'plugin'",
			path: ["viewType"],
		},
	);

const DeleteViewSchema = z.object({
	projectId: z.string(),
	viewId: z.string(),
});

const ListTaskPositionsSchema = z.object({
	projectId: z.string(),
	viewId: z.string(),
});

const MoveTaskSchema = z.object({
	projectId: z.string(),
	viewId: z.string(),
	taskId: z.string(),
	position: z.number().min(0),
	groupKey: z.string().nullable().optional(),
});

const BulkMoveTasksSchema = z.object({
	projectId: z.string(),
	viewId: z.string(),
	items: z
		.array(
			z.object({
				taskId: z.string(),
				position: z.number().min(0),
				groupKey: z.string().nullable().optional(),
			}),
		)
		.min(1),
});

const ListCustomFieldsSchema = z.object({
	projectId: z.string(),
});

// A custom field option can be given as a plain string (no color) or as an
// object carrying an optional color, e.g. { value: "High", color: "#ef4444" }.
const CustomFieldOptionInputSchema = z.union([
	z.string(),
	z.object({ value: z.string(), color: z.string().optional() }),
]);

const CreateCustomFieldSchema = z.object({
	projectId: z.string(),
	fieldKey: z.string(),
	displayName: z.string(),
	fieldType: z.string(),
	options: z.array(CustomFieldOptionInputSchema).optional(),
	isRequired: z.boolean().optional(),
	useInSearch: z.boolean().optional(),
	rows: z.number().int().min(2).max(20).optional(),
});

const GetCustomFieldSchema = z.object({
	projectId: z.string(),
	fieldId: z.string(),
});

const UpdateCustomFieldSchema = z.object({
	projectId: z.string(),
	fieldId: z.string(),
	displayName: z.string().optional(),
	fieldType: z.string().optional(),
	options: z.array(CustomFieldOptionInputSchema).optional(),
	isRequired: z.boolean().optional(),
	useInSearch: z.boolean().optional(),
	rows: z.number().int().min(2).max(20).optional(),
});

// Normalizes a parsed options array (each element either a plain string or
// an already-{value,color} object) into CustomFieldOption[] for the API.
function normalizeCustomFieldOptions(
	options: z.infer<typeof CustomFieldOptionInputSchema>[] | undefined,
): { value: string; color?: string }[] | undefined {
	return options?.map((opt) =>
		typeof opt === "string" ? { value: opt } : opt,
	);
}

const DeleteCustomFieldSchema = z.object({
	projectId: z.string(),
	fieldId: z.string(),
});

/**
 * Returns all view-related MCP tools.
 */
export function getViewTools(): Tool[] {
	return [
		{
			name: "list_views",
			description:
				"List all views in a project. Use context='backlog' or context='timeline' to list non-sprint views. Use context='sprint' with sprintId to list views for a specific sprint.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					context: {
						type: "string",
						description:
							"The view context: 'sprint', 'backlog', or 'timeline'. Defaults to 'backlog'. Use 'sprint' together with sprintId to list sprint views.",
					},
					sprintId: {
						type: "string",
						description: "The sprint ID (required when context is 'sprint').",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "create_view",
			description:
				"Create a new view. For a plugin-provided view (e.g. a plugin's custom dashboard/board surfaced via its 'view' extension point), set viewType to 'plugin' and provide pluginManifestId + pluginComponent — check the target plugin's own docs/skill for its exact manifest ID and component name (e.g. the com.paca.dashboard plugin's Dashboard view uses pluginManifestId 'com.paca.dashboard' and pluginComponent 'DashboardIntegrationView'). The returned view's id is what that plugin then expects as its own 'host view id'.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					name: {
						type: "string",
						description: "The name of the view",
					},
					context: {
						type: "string",
						description: "The context (sprint, backlog, timeline)",
					},
					viewType: {
						type: "string",
						enum: ["table", "board", "gantt", "plugin"],
						description:
							"The type of view. Use 'table', 'board', or 'gantt' for the built-in layouts, or 'plugin' to host a plugin-provided view (requires pluginManifestId and pluginComponent).",
					},
					sprintId: {
						type: "string",
						description: "The sprint ID (required for sprint context)",
					},
					pluginManifestId: {
						type: "string",
						description:
							"Required (together with pluginComponent) when viewType is 'plugin'. The reverse-DNS manifest ID of the plugin that provides this view (e.g. 'com.paca.dashboard').",
					},
					pluginComponent: {
						type: "string",
						description:
							"Required (together with pluginManifestId) when viewType is 'plugin'. The component name from that plugin's 'view' extension point (e.g. 'DashboardIntegrationView').",
					},
				},
				required: ["projectId", "name", "context", "viewType"],
			},
		},
		{
			name: "reorder_views",
			description: "Reorder views in a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					viewIds: {
						type: "array",
						items: { type: "string" },
						description: "Array of view IDs in new order",
					},
					context: {
						type: "string",
						description:
							"The view context: 'sprint', 'backlog', or 'timeline'.",
					},
					sprintId: {
						type: "string",
						description: "The sprint ID (required when context is 'sprint').",
					},
				},
				required: ["projectId", "viewIds"],
			},
		},
		{
			name: "get_view",
			description: "Get details of a specific view",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					viewId: {
						type: "string",
						description: "The ID of the view",
					},
				},
				required: ["projectId", "viewId"],
			},
		},
		{
			name: "update_view",
			description: "Update an existing view",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					viewId: {
						type: "string",
						description: "The ID of the view",
					},
					name: {
						type: "string",
						description: "The new name",
					},
					context: {
						type: "string",
						description: "The new context",
					},
					viewType: {
						type: "string",
						enum: ["table", "board", "gantt", "plugin"],
						description:
							"The type of view (table, board, gantt, or plugin — plugin requires pluginManifestId and pluginComponent).",
					},
					sprintId: {
						type: "string",
						description: "The new sprint ID",
					},
					pluginManifestId: {
						type: ["string", "null"],
						description:
							"Required (together with pluginComponent) when viewType is 'plugin'. The reverse-DNS manifest ID of the plugin that provides this view (e.g. 'com.paca.dashboard'). Pass null (together with pluginComponent: null) to clear an existing plugin binding, e.g. when changing viewType away from 'plugin'.",
					},
					pluginComponent: {
						type: ["string", "null"],
						description:
							"Required (together with pluginManifestId) when viewType is 'plugin'. The component name from that plugin's 'view' extension point (e.g. 'DashboardIntegrationView'). Pass null (together with pluginManifestId: null) to clear an existing plugin binding.",
					},
				},
				required: ["projectId", "viewId"],
			},
		},
		{
			name: "delete_view",
			description: "Delete a view",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					viewId: {
						type: "string",
						description: "The ID of the view",
					},
				},
				required: ["projectId", "viewId"],
			},
		},
		{
			name: "list_task_positions",
			description: "List task positions in a view",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					viewId: {
						type: "string",
						description: "The ID of the view",
					},
				},
				required: ["projectId", "viewId"],
			},
		},
		{
			name: "bulk_move_tasks",
			description:
				"Set the position of several tasks within one view (no sprint or status change)",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					viewId: {
						type: "string",
						description: "The ID of the view",
					},
					items: {
						type: "array",
						description: "The tasks and their new positions in the view",
						items: {
							type: "object",
							properties: {
								taskId: { type: "string", description: "The ID of the task" },
								position: {
									type: "number",
									description: "The zero-based position in the view",
								},
								groupKey: {
									type: ["string", "null"],
									description: "The group key when the view is grouped",
								},
							},
							required: ["taskId", "position"],
						},
					},
				},
				required: ["projectId", "viewId", "items"],
			},
		},
		{
			name: "move_task",
			description:
				"Set a task's position within one view (no sprint or status change)",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					viewId: {
						type: "string",
						description: "The ID of the view",
					},
					taskId: {
						type: "string",
						description: "The ID of the task",
					},
					position: {
						type: "number",
						description: "The zero-based position in the view",
					},
					groupKey: {
						type: ["string", "null"],
						description: "The group key when the view is grouped",
					},
				},
				required: ["projectId", "viewId", "taskId", "position"],
			},
		},
	];
}

/**
 * Returns all custom field related MCP tools.
 */
export function getCustomFieldTools(): Tool[] {
	return [
		{
			name: "list_custom_fields",
			description: "List all custom field definitions in a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "create_custom_field",
			description: "Create a new custom field definition",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					fieldKey: {
						type: "string",
						description: "The field key",
					},
					displayName: {
						type: "string",
						description: "The display name",
					},
					fieldType: {
						type: "string",
						description:
							"The field type (text, number, date, select, multi_select, boolean, url, long_text)",
					},
					options: {
						type: "array",
						items: {
							oneOf: [
								{ type: "string" },
								{
									type: "object",
									properties: {
										value: { type: "string" },
										color: { type: "string" },
									},
									required: ["value"],
								},
							],
						},
						description:
							'Options for select/multi_select types. Each item is either a plain string, or an object { value, color? } to give that value a display color (e.g. "#ef4444").',
					},
					isRequired: {
						type: "boolean",
						description: "Whether the field is required",
					},
					useInSearch: {
						type: "boolean",
						description: "Whether task search also matches this field's value",
					},
					rows: {
						type: "number",
						description:
							"Visible lines of a long_text field's editor (2-20, default 3)",
					},
				},
				required: ["projectId", "fieldKey", "displayName", "fieldType"],
			},
		},
		{
			name: "get_custom_field",
			description: "Get details of a custom field definition",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					fieldId: {
						type: "string",
						description: "The ID of the field",
					},
				},
				required: ["projectId", "fieldId"],
			},
		},
		{
			name: "update_custom_field",
			description: "Update a custom field definition",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					fieldId: {
						type: "string",
						description: "The ID of the field",
					},
					displayName: {
						type: "string",
						description: "The new display name",
					},
					fieldType: {
						type: "string",
						description: "The new field type",
					},
					options: {
						type: "array",
						items: {
							oneOf: [
								{ type: "string" },
								{
									type: "object",
									properties: {
										value: { type: "string" },
										color: { type: "string" },
									},
									required: ["value"],
								},
							],
						},
						description:
							'New options. Each item is either a plain string, or an object { value, color? } to give that value a display color (e.g. "#ef4444").',
					},
					isRequired: {
						type: "boolean",
						description: "Whether the field is required",
					},
					useInSearch: {
						type: "boolean",
						description: "Whether task search also matches this field's value",
					},
					rows: {
						type: "number",
						description:
							"Visible lines of a long_text field's editor (2-20, default 3)",
					},
				},
				required: ["projectId", "fieldId"],
			},
		},
		{
			name: "delete_custom_field",
			description: "Delete a custom field definition",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The ID of the project",
					},
					fieldId: {
						type: "string",
						description: "The ID of the field",
					},
				},
				required: ["projectId", "fieldId"],
			},
		},
	];
}

function formatView(view: any): string {
	const lines = [
		`View: ${view.name}`,
		`ID: ${view.id}`,
		`Type: ${view.view_type}`,
		`Context: ${view.context}`,
		`Sprint ID: ${view.sprint_id || "None"}`,
	];
	if (view.config?.plugin_manifest_id) {
		lines.push(
			`Plugin: ${view.config.plugin_manifest_id} (component: ${view.config.plugin_component})`,
		);
	}
	lines.push(`Position: ${view.position}`, `Created: ${view.created_at}`);
	return lines.join("\n");
}

/**
 * Builds the ViewConfig body sent for a plugin-backed view. Returns
 * undefined when neither field is set, so non-plugin create/update calls
 * don't send an empty config object. A field set to null (only possible on
 * update_view) is passed through as null to explicitly clear an existing
 * plugin binding.
 */
function buildViewConfig(
	pluginManifestId?: string | null,
	pluginComponent?: string | null,
): ViewConfig | undefined {
	if (pluginManifestId === undefined && pluginComponent === undefined) {
		return undefined;
	}
	return {
		plugin_manifest_id: pluginManifestId,
		plugin_component: pluginComponent,
	};
}

function formatCustomField(field: any): string {
	return `Custom Field: ${field.display_name}
ID: ${field.id}
Key: ${field.field_key}
Type: ${field.field_type}
Options: ${field.options.length > 0 ? field.options.map((o: { value: string }) => o.value).join(", ") : "None"}
Required: ${field.is_required}
Use in search: ${field.use_in_search}
Rows: ${field.rows}
Created: ${field.created_at}`;
}

/**
 * Handles view and custom field tool calls.
 */
export async function handleViewTool(
	toolName: string,
	args: any,
	client: PacaAPIViewsClient,
): Promise<any> {
	switch (toolName) {
		case "list_views": {
			const { projectId, context, sprintId } = ListViewsSchema.parse(args);
			// Default to 'backlog' when no context is provided to avoid requiring sprint_id
			const resolvedContext = context ?? "backlog";
			const views = await client.listViews(
				projectId,
				resolvedContext,
				sprintId,
			);
			const formatted = formatList(views, formatView);
			return {
				content: [
					{
						type: "text",
						text: `Views:\n\n${formatted}`,
					},
				],
			};
		}

		case "create_view": {
			const {
				projectId,
				name,
				context,
				viewType,
				sprintId,
				pluginManifestId,
				pluginComponent,
			} = CreateViewSchema.parse(args);
			const view = await client.createView(
				projectId,
				{
					name,
					context,
					view_type: viewType as any,
					sprint_id: sprintId ?? null,
					config: buildViewConfig(pluginManifestId, pluginComponent),
				},
				context,
				sprintId,
			);
			return {
				content: [
					{
						type: "text",
						text: `View created successfully:\n\n${formatView(view)}`,
					},
				],
			};
		}

		case "reorder_views": {
			const { projectId, viewIds, context, sprintId } =
				ReorderViewsSchema.parse(args);
			await client.reorderViews(
				projectId,
				{ view_ids: viewIds },
				context,
				sprintId,
			);
			return {
				content: [
					{
						type: "text",
						text: `Views reordered successfully`,
					},
				],
			};
		}

		case "get_view": {
			const { projectId, viewId } = GetViewSchema.parse(args);
			const view = await client.getView(projectId, viewId);
			return {
				content: [
					{
						type: "text",
						text: formatView(view),
					},
				],
			};
		}

		case "update_view": {
			const {
				projectId,
				viewId,
				name,
				context,
				viewType,
				sprintId,
				pluginManifestId,
				pluginComponent,
			} = UpdateViewSchema.parse(args);
			const view = await client.updateView(projectId, viewId, {
				name,
				context,
				view_type: viewType as any,
				sprint_id: sprintId ?? null,
				config: buildViewConfig(pluginManifestId, pluginComponent),
			});
			return {
				content: [
					{
						type: "text",
						text: `View updated successfully:\n\n${formatView(view)}`,
					},
				],
			};
		}

		case "delete_view": {
			const { projectId, viewId } = DeleteViewSchema.parse(args);
			await client.deleteView(projectId, viewId);
			return {
				content: [
					{
						type: "text",
						text: `View ${viewId} deleted successfully`,
					},
				],
			};
		}

		case "list_task_positions": {
			const { projectId, viewId } = ListTaskPositionsSchema.parse(args);
			const positions = await client.listTaskPositions(projectId, viewId);
			return {
				content: [
					{
						type: "text",
						text: `Task Positions:\n\n${JSON.stringify(positions, null, 2)}`,
					},
				],
			};
		}

		case "bulk_move_tasks": {
			const { projectId, viewId, items } = BulkMoveTasksSchema.parse(args);
			await client.bulkMoveTasks(projectId, viewId, {
				items: items.map((item) => ({
					task_id: item.taskId,
					position: item.position,
					group_key: item.groupKey ?? null,
				})),
			});
			return {
				content: [
					{
						type: "text",
						text: `Moved ${items.length} task position${items.length === 1 ? "" : "s"} in view ${viewId}`,
					},
				],
			};
		}

		case "move_task": {
			const { projectId, viewId, taskId, position, groupKey } =
				MoveTaskSchema.parse(args);
			await client.moveTaskPosition(projectId, viewId, taskId, {
				position,
				group_key: groupKey ?? null,
			});
			return {
				content: [
					{
						type: "text",
						text: `Moved task ${taskId} to position ${position} in view ${viewId}`,
					},
				],
			};
		}

		case "list_custom_fields": {
			const { projectId } = ListCustomFieldsSchema.parse(args);
			const fields = await client.listCustomFieldDefinitions(projectId);
			const formatted = formatList(fields, formatCustomField);
			return {
				content: [
					{
						type: "text",
						text: `Custom Fields:\n\n${formatted}`,
					},
				],
			};
		}

		case "create_custom_field": {
			const {
				projectId,
				fieldKey,
				displayName,
				fieldType,
				options,
				isRequired,
				useInSearch,
				rows,
			} = CreateCustomFieldSchema.parse(args);
			const field = await client.createCustomFieldDefinition(projectId, {
				field_key: fieldKey,
				display_name: displayName,
				field_type: fieldType as any,
				options: normalizeCustomFieldOptions(options),
				is_required: isRequired,
				use_in_search: useInSearch,
				rows,
			});
			return {
				content: [
					{
						type: "text",
						text: `Custom field created successfully:\n\n${formatCustomField(field)}`,
					},
				],
			};
		}

		case "get_custom_field": {
			const { projectId, fieldId } = GetCustomFieldSchema.parse(args);
			const field = await client.getCustomFieldDefinition(projectId, fieldId);
			return {
				content: [
					{
						type: "text",
						text: formatCustomField(field),
					},
				],
			};
		}

		case "update_custom_field": {
			const {
				projectId,
				fieldId,
				displayName,
				fieldType,
				options,
				isRequired,
				useInSearch,
				rows,
			} = UpdateCustomFieldSchema.parse(args);
			const field = await client.updateCustomFieldDefinition(
				projectId,
				fieldId,
				{
					display_name: displayName,
					field_type: fieldType as any,
					options: normalizeCustomFieldOptions(options),
					is_required: isRequired,
					use_in_search: useInSearch,
					rows,
				},
			);
			return {
				content: [
					{
						type: "text",
						text: `Custom field updated successfully:\n\n${formatCustomField(field)}`,
					},
				],
			};
		}

		case "delete_custom_field": {
			const { projectId, fieldId } = DeleteCustomFieldSchema.parse(args);
			await client.deleteCustomFieldDefinition(projectId, fieldId);
			return {
				content: [
					{
						type: "text",
						text: `Custom field ${fieldId} deleted successfully`,
					},
				],
			};
		}

		default:
			throw new Error(`Unknown view/custom field tool: ${toolName}`);
	}
}
