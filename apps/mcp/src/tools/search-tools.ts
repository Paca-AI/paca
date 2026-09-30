import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIClient, PacaAPIDocClient } from "../api/index.js";
import type { DocumentFolder } from "../types/index.js";

const SearchDocsSchema = z.object({
	projectId: z.string(),
	query: z.string().trim().min(1),
	limit: z.number().int().min(1).max(50).optional().default(20),
});

const SearchTasksSchema = z.object({
	projectId: z.string(),
	query: z.string().trim().min(1),
	statusId: z.string().optional(),
	sprintId: z.string().nullable().optional(),
	cursor: z.string().optional(),
	pageSize: z.number().int().min(1).max(200).optional().default(20),
});

const SNIPPET_RADIUS = 80;

/**
 * Returns the project search MCP tools.
 */
export function getSearchTools(): Tool[] {
	return [
		{
			name: "search_docs",
			description:
				"Search a project's documentation by keyword across BOTH document titles and body text (case-insensitive, literal match — not fuzzy or semantic). " +
				"Returns each match's path, where it matched (title or content) and a short excerpt. " +
				"Use this to find relevant docs quickly instead of walking list_docs; then open a result with read_doc.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					query: {
						type: "string",
						description:
							"Text to look for. Matched literally within a single run of text, so prefer one or two distinctive words over a long phrase.",
					},
					limit: {
						type: "number",
						description:
							"Maximum number of documents to return (1-50, default 20).",
					},
				},
				required: ["projectId", "query"],
			},
		},
		{
			name: "search_tasks",
			description:
				"Search a project's tasks by keyword across the title, the '#<number>' id and the description text (case-insensitive, literal match). " +
				"Returns compact results with an excerpt of where the description matched; use get_task for full details.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					query: {
						type: "string",
						description:
							"Text to look for, e.g. 'login timeout' or '#42'. Matched literally, not fuzzily.",
					},
					statusId: {
						type: "string",
						description: "Only return tasks in this status ID.",
					},
					sprintId: {
						type: "string",
						description:
							"Only return tasks in this sprint ID. Pass null to search only the backlog.",
					},
					cursor: {
						type: "string",
						description:
							"Opaque pagination cursor returned by a previous search_tasks call.",
					},
					pageSize: {
						type: "number",
						description: "Tasks per page (1-200, default 20).",
					},
				},
				required: ["projectId", "query"],
			},
		},
	];
}

/** Concatenates the inline "text" nodes of BlockNote JSON, one line per block. */
export function extractPlainText(blocks: unknown): string {
	const parts: string[] = [];
	const walk = (node: unknown): void => {
		if (Array.isArray(node)) {
			for (const child of node) walk(child);
			return;
		}
		if (!node || typeof node !== "object") return;
		const n = node as Record<string, unknown>;
		if (typeof n.text === "string") parts.push(n.text);
		walk(n.content);
		walk(n.children);
		if ("id" in n) parts.push("\n");
	};
	walk(blocks);
	return parts.join("").trim();
}

/** Excerpt around the first case-insensitive occurrence of query, or "". */
export function buildSnippet(text: string, query: string): string {
	const idx = text.toLowerCase().indexOf(query.toLowerCase());
	if (idx < 0) return "";
	const start = Math.max(0, idx - SNIPPET_RADIUS);
	const end = Math.min(text.length, idx + query.length + SNIPPET_RADIUS);
	const body = text.slice(start, end).split(/\s+/).filter(Boolean).join(" ");
	return `${start > 0 ? "…" : ""}${body}${end < text.length ? "…" : ""}`;
}

function docPath(
	title: string,
	folderId: string | null | undefined,
	folders: Map<string, DocumentFolder>,
): string {
	const names: string[] = [];
	const seen = new Set<string>();
	let cur = folderId ? folders.get(folderId) : undefined;
	while (cur && !seen.has(cur.id)) {
		seen.add(cur.id);
		names.unshift(cur.name);
		cur = cur.parent_id ? folders.get(cur.parent_id) : undefined;
	}
	return [...names, title].join("/");
}

const text = (t: string) => ({ content: [{ type: "text", text: t }] });

/**
 * Handles search_docs / search_tasks tool calls.
 */
export async function handleSearchTool(
	toolName: string,
	args: unknown,
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<any> {
	switch (toolName) {
		case "search_docs": {
			const { projectId, query, limit } = SearchDocsSchema.parse(args);
			const [hits, folders] = await Promise.all([
				docClient.searchDocuments(projectId, query, limit),
				docClient.listFolders(projectId),
			]);
			if (hits.length === 0) {
				return text(
					`No documents matched "${query}" in titles or body text. Try a shorter or different keyword, or browse with list_docs.`,
				);
			}
			const byId = new Map(folders.map((f) => [f.id, f]));
			const lines = hits.map((h) => {
				const path = docPath(h.title, h.folder_id, byId);
				const where =
					h.matched_in === "title" ? "title match" : "content match";
				return `📄 ${path}  (${where}, id: ${h.id})${h.snippet ? `\n    ${h.snippet}` : ""}`;
			});
			return text(
				`Documents matching "${query}" (${hits.length}${hits.length === limit ? ", may be more — raise limit or refine the query" : ""}):\n\n${lines.join("\n\n")}\n\nOpen one with read_doc (path or docId).`,
			);
		}

		case "search_tasks": {
			const { projectId, query, statusId, sprintId, cursor, pageSize } =
				SearchTasksSchema.parse(args);
			const result = await apiClient.listTasks(projectId, {
				search: query,
				searchContent: true,
				statusId,
				sprintId,
				cursor,
				pageSize,
			});
			if (result.items.length === 0) {
				return text(
					`No tasks matched "${query}" in titles, numbers or descriptions.`,
				);
			}
			const lines = result.items.map((t) => {
				const snippet = buildSnippet(extractPlainText(t.description), query);
				return `#${t.task_number} ${t.title}  (id: ${t.id}, status: ${t.status_id || "None"})${snippet ? `\n    ${snippet}` : ""}`;
			});
			const more = result.nextCursor
				? `\n\n_More results available — call search_tasks again with cursor="${result.nextCursor}"._`
				: "";
			return text(
				`Tasks matching "${query}" (${result.items.length} returned):\n\n${lines.join("\n\n")}${more}\n\nUse get_task for full details.`,
			);
		}

		default:
			throw new Error(`Unknown search tool: ${toolName}`);
	}
}
