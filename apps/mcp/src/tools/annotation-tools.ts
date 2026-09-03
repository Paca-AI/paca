import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIAnnotationClient } from "../api/index.js";
import type { PageAnnotation } from "../types/index.js";

const ListAnnotationsSchema = z.object({
	projectId: z.string(),
	search: z.string().optional(),
	environmentId: z.string().optional(),
	portForwardId: z.string().optional(),
	status: z.enum(["open", "resolved"]).optional(),
	cursor: z.string().optional(),
	// Defaulted here (not left to the API) so omitting pageSize actually
	// gets the "default 20" the tool description promises — the backend
	// only applies a LIMIT when page_size is present on the query string at
	// all, so leaving this unset would otherwise fetch every annotation in
	// the project unbounded.
	pageSize: z.number().min(1).max(200).optional().default(20),
});

const GetAnnotationSchema = z.object({
	projectId: z.string(),
	annotationId: z.string(),
});

/**
 * Returns all page-annotation ("comment") related MCP tools.
 */
export function getAnnotationTools(): Tool[] {
	return [
		{
			name: "list_annotations",
			description:
				"List page comments (annotations pinned to elements of a page via the browser extension) in a project, across every environment and port forward unless filtered",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					search: {
						type: "string",
						description:
							"Case-insensitive search over the comment body and the commented-on element's text",
					},
					environmentId: {
						type: "string",
						description: "Filter to comments made in one specific environment",
					},
					portForwardId: {
						type: "string",
						description:
							"Filter to comments made through one specific port forward",
					},
					status: {
						type: "string",
						enum: ["open", "resolved"],
						description: "Filter by resolution status",
					},
					cursor: {
						type: "string",
						description:
							"Opaque pagination cursor returned as next_cursor from a previous call to this tool",
					},
					pageSize: {
						type: "number",
						description:
							"Number of comments to return per page (1-200, default 20)",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "get_annotation",
			description:
				"Get full detail for one page comment: the commented-on element, the full reply thread, and any captured console errors / failed network requests",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_projects to get the project ID. Do NOT use the project name.",
					},
					annotationId: {
						type: "string",
						description:
							"The technical UUID of the comment. Use list_annotations to find comment IDs.",
					},
				},
				required: ["projectId", "annotationId"],
			},
		},
	];
}

function formatAnnotationSummary(a: PageAnnotation): string {
	return `Comment: ${a.id}
Page: ${a.page_path}
Element: <${a.element_snapshot.tag_name}> ${a.element_snapshot.accessible_name || a.element_snapshot.text_excerpt}
Status: ${a.status}
Author: ${a.created_by_name} (${a.created_by})
Created: ${a.created_at}
Replies: ${a.comments.length}
Body: ${a.body}`;
}

function formatAnnotationDetail(a: PageAnnotation): string {
	const lines = [
		`Comment: ${a.id}`,
		`Project: ${a.project_id}`,
		`Environment: ${a.environment_id}`,
		`Port forward: ${a.port_forward_id}`,
		`Page: ${a.page_path}`,
		`Element selector: ${a.element_selector}`,
		`Element: <${a.element_snapshot.tag_name}> role=${a.element_snapshot.role} accessible_name=${a.element_snapshot.accessible_name || "(none)"}`,
		`Element text: ${a.element_snapshot.text_excerpt}`,
		`Status: ${a.status}`,
		`Author: ${a.created_by_name} (${a.created_by})`,
		`Created: ${a.created_at}`,
		`Task: ${a.task_id ? a.task_id : "(none created yet)"}`,
		`Screenshot: ${a.screenshot_file_id ? "attached (not shown in this text response)" : "(none)"}`,
		`Console errors captured: ${a.console_errors.length}`,
		...a.console_errors.map((e) => `  [${e.level}] ${e.message}`),
		`Failed requests captured: ${a.failed_requests.length}`,
		...a.failed_requests.map(
			(r) => `  ${r.method} ${r.url} — ${r.status_code || r.error}`,
		),
		"",
		`Comment body:\n${a.body}`,
	];
	if (a.comments.length > 0) {
		lines.push("", `Replies (${a.comments.length}):`);
		for (const c of a.comments) {
			lines.push(`- ${c.created_by_name} (${c.created_at}): ${c.body}`);
		}
	}
	return lines.join("\n");
}

/**
 * Handles page-annotation tool calls.
 */
export async function handleAnnotationTool(
	toolName: string,
	args: any,
	client: PacaAPIAnnotationClient,
): Promise<any> {
	switch (toolName) {
		case "list_annotations": {
			const { projectId, ...opts } = ListAnnotationsSchema.parse(args);
			const result = await client.listAnnotations(projectId, opts);
			const formatted = result.items
				.map(formatAnnotationSummary)
				.join("\n\n---\n\n");
			const nextCursorNote = result.next_cursor
				? `\n\nMore results available — pass cursor: "${result.next_cursor}" to list_annotations to get the next page.`
				: "";
			return {
				content: [
					{
						type: "text",
						text:
							result.items.length === 0
								? "No comments found."
								: `Comments (${result.items.length} returned):\n\n${formatted}${nextCursorNote}`,
					},
				],
			};
		}

		case "get_annotation": {
			const { projectId, annotationId } = GetAnnotationSchema.parse(args);
			const annotation = await client.getAnnotation(projectId, annotationId);
			return {
				content: [
					{
						type: "text",
						text: formatAnnotationDetail(annotation),
					},
				],
			};
		}

		default:
			throw new Error(`Unknown annotation tool: ${toolName}`);
	}
}
