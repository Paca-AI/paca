import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import * as z from "zod";
import type { PacaAPITaskExtendedClient } from "../api/index.js";
import { blocknoteToMarkdown } from "../utils/index.js";

const ListTaskActivitiesSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
});

const AddTaskCommentSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
	content: z.string(),
});

const UpdateTaskCommentSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
	commentId: z.string(),
	content: z.string(),
});

const DeleteTaskCommentSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
	commentId: z.string(),
});

/**
 * Returns all task comment and activity related MCP tools.
 */
export function getTaskActivityTools(): Tool[] {
	return [
		{
			name: "list_task_activities",
			description: "List all activities for a task",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					taskId: {
						type: "string",
						description:
							"The technical UUID of the task (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_tasks to get the task ID.",
					},
				},
				required: ["projectId", "taskId"],
			},
		},
		{
			name: "add_task_comment",
			description: "Add a comment to a task",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					taskId: {
						type: "string",
						description:
							"The technical UUID of the task (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_tasks to get the task ID.",
					},
					content: {
						type: "string",
						description: "The comment content",
					},
				},
				required: ["projectId", "taskId", "content"],
			},
		},
		{
			name: "update_task_comment",
			description: "Update a task comment",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					taskId: {
						type: "string",
						description:
							"The technical UUID of the task (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_tasks to get the task ID.",
					},
					commentId: {
						type: "string",
						description:
							"The technical UUID of the comment (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_activities to find comment IDs in the activity list.",
					},
					content: {
						type: "string",
						description: "The new comment content",
					},
				},
				required: ["projectId", "taskId", "commentId", "content"],
			},
		},
		{
			name: "delete_task_comment",
			description: "Delete a task comment",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					taskId: {
						type: "string",
						description:
							"The technical UUID of the task (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_tasks to get the task ID.",
					},
					commentId: {
						type: "string",
						description:
							"The technical UUID of the comment (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_activities to find comment IDs in the activity list.",
					},
				},
				required: ["projectId", "taskId", "commentId"],
			},
		},
	];
}

function formatTaskActivity(activity: any): string {
	const content =
		activity.activity_type === "comment" && activity.content
			? blocknoteToMarkdown(activity.content)
			: JSON.stringify(activity.content, null, 2);

	return `Activity: ${activity.activity_type}
ID: ${activity.id}
User: ${activity.actor_name} (${activity.actor_id})
Description: ${content}
Created: ${activity.created_at}`;
}

function formatTaskComment(comment: any): string {
	const content = comment.content ? blocknoteToMarkdown(comment.content) : "";

	return `Comment:
ID: ${comment.id}
User: ${comment.actor_name} (${comment.actor_id})
Content: ${content}
Created: ${comment.created_at}
Updated: ${comment.updated_at}`;
}

/**
 * Handles task activity and comment tool calls.
 */
export async function handleTaskActivityTool(
	toolName: string,
	args: any,
	client: PacaAPITaskExtendedClient,
): Promise<any> {
	switch (toolName) {
		case "list_task_activities": {
			const { projectId, taskId } = ListTaskActivitiesSchema.parse(args);
			const activities = await client.listTaskActivities(projectId, taskId);
			const formatted = activities.map(formatTaskActivity).join("\n\n---\n\n");
			return {
				content: [
					{
						type: "text",
						text: `Task Activities:\n\n${formatted}`,
					},
				],
			};
		}

		case "add_task_comment": {
			const { projectId, taskId, content } = AddTaskCommentSchema.parse(args);
			const comment = await client.addTaskComment(projectId, taskId, {
				content,
			});
			return {
				content: [
					{
						type: "text",
						text: `Comment added successfully:\n\n${formatTaskComment(comment)}`,
					},
				],
			};
		}

		case "update_task_comment": {
			const { projectId, taskId, commentId, content } =
				UpdateTaskCommentSchema.parse(args);
			const comment = await client.updateTaskComment(
				projectId,
				taskId,
				commentId,
				{
					content,
				},
			);
			return {
				content: [
					{
						type: "text",
						text: `Comment updated successfully:\n\n${formatTaskComment(comment)}`,
					},
				],
			};
		}

		case "delete_task_comment": {
			const { projectId, taskId, commentId } =
				DeleteTaskCommentSchema.parse(args);
			await client.deleteTaskComment(projectId, taskId, commentId);
			return {
				content: [
					{
						type: "text",
						text: `Comment ${commentId} deleted successfully`,
					},
				],
			};
		}

		default:
			throw new Error(`Unknown task activity tool: ${toolName}`);
	}
}
