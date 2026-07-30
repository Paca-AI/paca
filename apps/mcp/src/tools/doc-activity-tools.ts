import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIDocClient } from "../api/index.js";
import { blocknoteToMarkdown, formatToolError } from "../utils/index.js";

const ListDocActivitiesSchema = z.object({
	projectId: z.string(),
	docId: z.string(),
});

const AddDocCommentSchema = z.object({
	projectId: z.string(),
	docId: z.string(),
	content: z.string(),
});

const UpdateDocCommentSchema = z.object({
	projectId: z.string(),
	docId: z.string(),
	commentId: z.string(),
	content: z.string(),
});

const DeleteDocCommentSchema = z.object({
	projectId: z.string(),
	docId: z.string(),
	commentId: z.string(),
});

const PROJECT_ID_DESC =
	"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.";

const DOC_ID_DESC =
	"The technical UUID of the document (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_docs to get the document ID.";

const COMMENT_ID_DESC =
	"The technical UUID of the comment (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_doc_activities to find comment IDs.";

const MENTION_NOTE =
	"NOTE: writing '@username' in the content does not send a notification for document comments (unlike add_task_comment) — the recipient will only see it if they happen to open this document. If this document is linked to a task, prefer add_task_comment there when you need to reliably reach someone.";

/**
 * Returns all document comment and activity related MCP tools.
 */
export function getDocActivityTools(): Tool[] {
	return [
		{
			name: "list_doc_activities",
			description:
				"List all activity for a document (comments and system events), oldest first. Each entry includes actor_name and actor_username.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					docId: { type: "string", description: DOC_ID_DESC },
				},
				required: ["projectId", "docId"],
			},
		},
		{
			name: "add_doc_comment",
			description: `Add a comment to a document. ${MENTION_NOTE}`,
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					docId: { type: "string", description: DOC_ID_DESC },
					content: { type: "string", description: "The comment content." },
				},
				required: ["projectId", "docId", "content"],
			},
		},
		{
			name: "update_doc_comment",
			description: "Update a document comment.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					docId: { type: "string", description: DOC_ID_DESC },
					commentId: { type: "string", description: COMMENT_ID_DESC },
					content: { type: "string", description: "The new comment content." },
				},
				required: ["projectId", "docId", "commentId", "content"],
			},
		},
		{
			name: "delete_doc_comment",
			description: "Delete a document comment.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: { type: "string", description: PROJECT_ID_DESC },
					docId: { type: "string", description: DOC_ID_DESC },
					commentId: { type: "string", description: COMMENT_ID_DESC },
				},
				required: ["projectId", "docId", "commentId"],
			},
		},
	];
}

function formatDocActivity(activity: any): string {
	const content =
		activity.activity_type === "comment" && activity.content
			? blocknoteToMarkdown(activity.content)
			: JSON.stringify(activity.content, null, 2);

	return `Activity: ${activity.activity_type}
ID: ${activity.id}
User: ${activity.actor_name} (@${activity.actor_username}, ${activity.actor_id})
Description: ${content}
Created: ${activity.created_at}`;
}

function formatDocComment(comment: any): string {
	const content = comment.content ? blocknoteToMarkdown(comment.content) : "";

	return `Comment:
ID: ${comment.id}
User: ${comment.actor_name} (@${comment.actor_username}, ${comment.actor_id})
Content: ${content}
Created: ${comment.created_at}
Updated: ${comment.updated_at}`;
}

/**
 * Handles document activity and comment tool calls.
 *
 * Wrapped in its own try/catch (rather than relying on handleToolCall's outer
 * try/catch, which only sees synchronous throws) — a bare
 * `return handleDocActivityTool(...)` in tools/index.ts does not catch this
 * function's own async rejections, so every error path here must resolve to
 * a normal tool result instead of rejecting.
 */
export async function handleDocActivityTool(
	toolName: string,
	args: any,
	client: PacaAPIDocClient,
): Promise<any> {
	try {
		switch (toolName) {
			case "list_doc_activities": {
				const { projectId, docId } = ListDocActivitiesSchema.parse(args);
				const activities = await client.listDocumentActivities(
					projectId,
					docId,
				);
				const formatted = activities.map(formatDocActivity).join("\n\n---\n\n");
				return {
					content: [
						{ type: "text", text: `Document Activities:\n\n${formatted}` },
					],
				};
			}

			case "add_doc_comment": {
				const { projectId, docId, content } = AddDocCommentSchema.parse(args);
				const comment = await client.addDocumentComment(
					projectId,
					docId,
					content,
				);
				return {
					content: [
						{
							type: "text",
							text: `Comment added successfully:\n\n${formatDocComment(comment)}`,
						},
					],
				};
			}

			case "update_doc_comment": {
				const { projectId, docId, commentId, content } =
					UpdateDocCommentSchema.parse(args);
				const comment = await client.updateDocumentComment(
					projectId,
					docId,
					commentId,
					content,
				);
				return {
					content: [
						{
							type: "text",
							text: `Comment updated successfully:\n\n${formatDocComment(comment)}`,
						},
					],
				};
			}

			case "delete_doc_comment": {
				const { projectId, docId, commentId } =
					DeleteDocCommentSchema.parse(args);
				await client.deleteDocumentComment(projectId, docId, commentId);
				return {
					content: [
						{ type: "text", text: `Comment ${commentId} deleted successfully` },
					],
				};
			}

			default:
				throw new Error(`Unknown document activity tool: ${toolName}`);
		}
	} catch (error) {
		return {
			content: [{ type: "text", text: `Error: ${formatToolError(error)}` }],
			isError: true,
		};
	}
}
