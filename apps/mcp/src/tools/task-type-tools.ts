import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import * as z from "zod";
import type { PacaAPIExtendedClient } from "../api/index.js";
import { formatList } from "../utils/index.js";

const ListTaskTypesSchema = z.object({
	projectId: z.string(),
});

const CreateTaskTypeSchema = z.object({
	projectId: z.string(),
	name: z.string(),
	icon: z.string().optional(),
	color: z.string().optional(),
	description: z.string().optional(),
});

const UpdateTaskTypeSchema = z.object({
	projectId: z.string(),
	typeId: z.string(),
	name: z.string().optional(),
	icon: z.string().optional(),
	color: z.string().optional(),
	description: z.string().optional(),
});

const DeleteTaskTypeSchema = z.object({
	projectId: z.string(),
	typeId: z.string(),
});

const SetDefaultTaskTypeSchema = z.object({
	projectId: z.string(),
	typeId: z.string(),
});

const ListTaskStatusesSchema = z.object({
	projectId: z.string(),
});

const CreateTaskStatusSchema = z.object({
	projectId: z.string(),
	name: z.string(),
	color: z.string().optional(),
	category: z.string(),
});

const UpdateTaskStatusSchema = z.object({
	projectId: z.string(),
	statusId: z.string(),
	name: z.string().optional(),
	color: z.string().optional(),
	category: z.string().optional(),
	position: z.number().optional(),
});

const DeleteTaskStatusSchema = z.object({
	projectId: z.string(),
	statusId: z.string(),
});

const SetDefaultTaskStatusSchema = z.object({
	projectId: z.string(),
	statusId: z.string(),
});

/**
 * Returns all task type and status related MCP tools.
 */
export function getTaskTypeTools(): Tool[] {
	return [
		{
			name: "list_task_types",
			description: "List all task types in a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "create_task_type",
			description: "Create a new task type",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					name: {
						type: "string",
						description: "The name of the task type",
					},
					icon: {
						type: "string",
						description: "Icon for the task type",
					},
					color: {
						type: "string",
						description: "Color for the task type",
					},
					description: {
						type: "string",
						description: "Description of the task type",
					},
				},
				required: ["projectId", "name"],
			},
		},
		{
			name: "update_task_type",
			description: "Update an existing task type",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					typeId: {
						type: "string",
						description:
							"The technical UUID of the task type (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_types to get the type ID.",
					},
					name: {
						type: "string",
						description: "The new name of the task type",
					},
					icon: {
						type: "string",
						description: "The new icon",
					},
					color: {
						type: "string",
						description: "The new color",
					},
					description: {
						type: "string",
						description: "The new description",
					},
				},
				required: ["projectId", "typeId"],
			},
		},
		{
			name: "delete_task_type",
			description: "Delete a task type",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					typeId: {
						type: "string",
						description:
							"The technical UUID of the task type (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_types to get the type ID.",
					},
				},
				required: ["projectId", "typeId"],
			},
		},
		{
			name: "set_default_task_type",
			description: "Set a task type as the default for the project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					typeId: {
						type: "string",
						description:
							"The technical UUID of the task type (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_types to get the type ID.",
					},
				},
				required: ["projectId", "typeId"],
			},
		},
	];
}

export function getTaskStatusTools(): Tool[] {
	return [
		{
			name: "list_task_statuses",
			description: "List all task statuses in a project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "create_task_status",
			description: "Create a new task status",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					name: {
						type: "string",
						description: "The name of the status",
					},
					color: {
						type: "string",
						description: "Color for the status",
					},
					category: {
						type: "string",
						description:
							"Category of the status (backlog, refinement, ready, todo, inprogress, done)",
					},
				},
				required: ["projectId", "name", "category"],
			},
		},
		{
			name: "update_task_status",
			description: "Update an existing task status",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					statusId: {
						type: "string",
						description:
							"The technical UUID of the task status (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_statuses to get the status ID.",
					},
					name: {
						type: "string",
						description: "The new name",
					},
					color: {
						type: "string",
						description: "The new color",
					},
					category: {
						type: "string",
						description: "The new category",
					},
					position: {
						type: "number",
						description: "The new position",
					},
				},
				required: ["projectId", "statusId"],
			},
		},
		{
			name: "delete_task_status",
			description: "Delete a task status",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					statusId: {
						type: "string",
						description:
							"The technical UUID of the task status (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_statuses to get the status ID.",
					},
				},
				required: ["projectId", "statusId"],
			},
		},
		{
			name: "set_default_task_status",
			description: "Set a task status as the default for the project",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					statusId: {
						type: "string",
						description:
							"The technical UUID of the task status (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_statuses to get the status ID.",
					},
				},
				required: ["projectId", "statusId"],
			},
		},
	];
}

function formatTaskType(type: any): string {
	return `Task Type: ${type.name}
ID: ${type.id}
Icon: ${type.icon || "None"}
Color: ${type.color || "None"}
Description: ${type.description || "None"}
Default: ${type.is_default}
System: ${type.is_system}
Created: ${type.created_at}`;
}

function formatTaskStatus(status: any): string {
	return `Task Status: ${status.name}
ID: ${status.id}
Color: ${status.color || "None"}
Category: ${status.category}
Position: ${status.position}
Default: ${status.is_default ?? false}
Created: ${status.created_at}`;
}

/**
 * Handles task type and status tool calls.
 */
export async function handleTaskTypeTool(
	toolName: string,
	args: any,
	client: PacaAPIExtendedClient,
): Promise<any> {
	switch (toolName) {
		case "list_task_types": {
			const { projectId } = ListTaskTypesSchema.parse(args);
			const types = await client.listTaskTypes(projectId);
			const formatted = formatList(types, formatTaskType);
			return {
				content: [
					{
						type: "text",
						text: `Task Types:\n\n${formatted}`,
					},
				],
			};
		}

		case "create_task_type": {
			const { projectId, name, icon, color, description } =
				CreateTaskTypeSchema.parse(args);
			const type = await client.createTaskType(projectId, {
				name,
				icon,
				color,
				description,
			});
			return {
				content: [
					{
						type: "text",
						text: `Task type created successfully:\n\n${formatTaskType(type)}`,
					},
				],
			};
		}

		case "update_task_type": {
			const { projectId, typeId, name, icon, color, description } =
				UpdateTaskTypeSchema.parse(args);
			const type = await client.updateTaskType(projectId, typeId, {
				name,
				icon,
				color,
				description,
			});
			return {
				content: [
					{
						type: "text",
						text: `Task type updated successfully:\n\n${formatTaskType(type)}`,
					},
				],
			};
		}

		case "delete_task_type": {
			const { projectId, typeId } = DeleteTaskTypeSchema.parse(args);
			await client.deleteTaskType(projectId, typeId);
			return {
				content: [
					{
						type: "text",
						text: `Task type ${typeId} deleted successfully`,
					},
				],
			};
		}

		case "set_default_task_type": {
			const { projectId, typeId } = SetDefaultTaskTypeSchema.parse(args);
			const type = await client.setDefaultTaskType(projectId, typeId);
			return {
				content: [
					{
						type: "text",
						text: `Default task type set successfully:\n\n${formatTaskType(type)}`,
					},
				],
			};
		}

		case "list_task_statuses": {
			const { projectId } = ListTaskStatusesSchema.parse(args);
			const statuses = await client.listTaskStatuses(projectId);
			const formatted = formatList(statuses, formatTaskStatus);
			return {
				content: [
					{
						type: "text",
						text: `Task Statuses:\n\n${formatted}`,
					},
				],
			};
		}

		case "create_task_status": {
			const { projectId, name, color, category } =
				CreateTaskStatusSchema.parse(args);
			const status = await client.createTaskStatus(projectId, {
				name,
				color,
				category: category as any,
				position: 0,
			});
			return {
				content: [
					{
						type: "text",
						text: `Task status created successfully:\n\n${formatTaskStatus(status)}`,
					},
				],
			};
		}

		case "update_task_status": {
			const { projectId, statusId, name, color, category, position } =
				UpdateTaskStatusSchema.parse(args);
			const status = await client.updateTaskStatus(projectId, statusId, {
				name,
				color,
				category: category as any,
				position,
			});
			return {
				content: [
					{
						type: "text",
						text: `Task status updated successfully:\n\n${formatTaskStatus(status)}`,
					},
				],
			};
		}

		case "delete_task_status": {
			const { projectId, statusId } = DeleteTaskStatusSchema.parse(args);
			await client.deleteTaskStatus(projectId, statusId);
			return {
				content: [
					{
						type: "text",
						text: `Task status ${statusId} deleted successfully`,
					},
				],
			};
		}

		case "set_default_task_status": {
			const { projectId, statusId } = SetDefaultTaskStatusSchema.parse(args);
			const status = await client.setDefaultTaskStatus(projectId, statusId);
			return {
				content: [
					{
						type: "text",
						text: `Default task status set successfully:\n\n${formatTaskStatus(status)}`,
					},
				],
			};
		}

		default:
			throw new Error(`Unknown task type/status tool: ${toolName}`);
	}
}
