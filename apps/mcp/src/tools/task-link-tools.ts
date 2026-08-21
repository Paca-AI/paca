import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import * as z from "zod";
import type { PacaAPITaskExtendedClient } from "../api/index.js";

const ListTaskLinksSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
});

const CreateTaskLinkSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
	targetTaskId: z.string(),
	linkType: z.enum(["blocks", "relates_to", "duplicates"]),
});

const DeleteTaskLinkSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
	linkId: z.string(),
});

const PROJECT_ID_DESC =
	"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.";

const TASK_ID_DESC =
	"The technical UUID of the task (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_tasks to get the task ID.";

/**
 * Returns all task-link MCP tools.
 */
export function getTaskLinkTools(): Tool[] {
	return [
		{
			name: "list_task_links",
			description:
				"List all links for a task, showing how it relates to other tasks (blocks, is blocked by, relates to, duplicates, is duplicated by)",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					taskId: { type: "string", description: TASK_ID_DESC },
				},
				required: ["projectId", "taskId"],
			},
		},
		{
			name: "create_task_link",
			description:
				"Create a link between two tasks. Use 'blocks' when this task blocks another, 'relates_to' for a general relationship, or 'duplicates' when this task duplicates another.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					taskId: {
						type: "string",
						description:
							"The technical UUID of the source task (the task you are linking from). Use list_tasks to get the task ID.",
					},
					targetTaskId: {
						type: "string",
						description:
							"The technical UUID of the target task (the task you are linking to). Must be different from taskId.",
					},
					linkType: {
						type: "string",
						enum: ["blocks", "relates_to", "duplicates"],
						description:
							"The type of link: 'blocks' (this task blocks the target), 'relates_to' (general relationship, symmetric), 'duplicates' (this task duplicates the target).",
					},
				},
				required: ["projectId", "taskId", "targetTaskId", "linkType"],
			},
		},
		{
			name: "delete_task_link",
			description: "Remove a link between two tasks",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					taskId: { type: "string", description: TASK_ID_DESC },
					linkId: {
						type: "string",
						description:
							"The technical UUID of the link to delete. Use list_task_links to get the link ID.",
					},
				},
				required: ["projectId", "taskId", "linkId"],
			},
		},
	];
}

const DISPLAY_LINK_LABELS: Record<string, string> = {
	blocks: "Blocks",
	is_blocked_by: "Is blocked by",
	relates_to: "Relates to",
	duplicates: "Duplicates",
	is_duplicated_by: "Is duplicated by",
};

function formatTaskLink(link: any): string {
	const label =
		DISPLAY_LINK_LABELS[link.display_link_type] || link.display_link_type;
	const task = link.linked_task;
	const taskRef = task
		? `#${task.task_number} — ${task.title} (ID: ${task.id})`
		: link.target_task_id;
	return `- **${label}:** ${taskRef}\n  Link ID: ${link.id}`;
}

/**
 * Handles task-link tool calls.
 */
export async function handleTaskLinkTool(
	toolName: string,
	args: any,
	taskExtendedClient: PacaAPITaskExtendedClient,
): Promise<any> {
	switch (toolName) {
		case "list_task_links": {
			const { projectId, taskId } = ListTaskLinksSchema.parse(args);
			const links = await taskExtendedClient.listTaskLinks(projectId, taskId);
			if (links.length === 0) {
				return {
					content: [{ type: "text", text: "No links found for this task." }],
				};
			}
			const formatted = links.map(formatTaskLink).join("\n");
			return {
				content: [
					{
						type: "text",
						text: `Task links (${links.length}):\n\n${formatted}`,
					},
				],
			};
		}

		case "create_task_link": {
			const { projectId, taskId, targetTaskId, linkType } =
				CreateTaskLinkSchema.parse(args);
			const link = await taskExtendedClient.createTaskLink(projectId, taskId, {
				target_task_id: targetTaskId,
				link_type: linkType,
			});
			return {
				content: [
					{
						type: "text",
						text: `Task link created successfully:\n\n${formatTaskLink(link)}`,
					},
				],
			};
		}

		case "delete_task_link": {
			const { projectId, taskId, linkId } = DeleteTaskLinkSchema.parse(args);
			await taskExtendedClient.deleteTaskLink(projectId, taskId, linkId);
			return {
				content: [
					{
						type: "text",
						text: `Task link ${linkId} deleted successfully`,
					},
				],
			};
		}

		default:
			throw new Error(`Unknown task link tool: ${toolName}`);
	}
}
