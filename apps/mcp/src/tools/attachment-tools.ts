import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import * as z from "zod";
import type { PacaAPIViewsClient } from "../api/index.js";
import { formatFileSize, formatList } from "../utils/index.js";

const ListTaskAttachmentsSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
});

const GetAttachmentDownloadURLSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
	attachmentId: z.string(),
});

const ReadTaskAttachmentSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
	attachmentId: z.string(),
});

const DeleteTaskAttachmentSchema = z.object({
	projectId: z.string(),
	taskId: z.string(),
	attachmentId: z.string(),
});

// Content types that are safe to decode and hand back as plain text.
const TEXT_MIME_TYPES = new Set([
	"application/json",
	"application/xml",
	"application/yaml",
	"application/x-yaml",
	"application/javascript",
	"application/typescript",
	"application/x-sh",
	"application/sql",
	"application/x-ndjson",
	"application/toml",
]);

// Extension fallback for files uploaded with a generic content type (e.g.
// application/octet-stream), since uploaders don't always set one accurately.
const TEXT_EXTENSIONS = new Set([
	"txt",
	"md",
	"markdown",
	"csv",
	"tsv",
	"log",
	"json",
	"yml",
	"yaml",
	"xml",
	"html",
	"htm",
	"css",
	"scss",
	"less",
	"js",
	"jsx",
	"ts",
	"tsx",
	"mjs",
	"cjs",
	"py",
	"go",
	"rb",
	"java",
	"c",
	"cpp",
	"h",
	"hpp",
	"cs",
	"php",
	"sh",
	"bash",
	"zsh",
	"sql",
	"toml",
	"ini",
	"cfg",
	"conf",
	"env",
	"graphql",
	"proto",
	"rs",
	"kt",
	"swift",
	"vue",
	"svelte",
]);

// Conventionally-named text files that don't carry a recognizable extension,
// matched case-insensitively on the full file name.
const TEXT_FILENAMES = new Set([
	"dockerfile",
	"makefile",
	"rakefile",
	"gemfile",
	"gemfile.lock",
	"procfile",
	"vagrantfile",
	"license",
	"licence",
	"readme",
	"changelog",
	"contributing",
	"authors",
	"notice",
	".gitignore",
	".dockerignore",
	".gitattributes",
	".editorconfig",
	".npmrc",
	".env",
]);

// Image types the MCP "image" content block (and the LLMs consuming it) can
// actually render. Anything else falls back to the binary path below.
const IMAGE_MIME_TYPES = new Set([
	"image/png",
	"image/jpeg",
	"image/gif",
	"image/webp",
]);

const MAX_TEXT_BYTES = 2 * 1024 * 1024; // 2 MB
const MAX_IMAGE_BYTES = 5 * 1024 * 1024; // 5 MB

type AttachmentKind = "text" | "image" | "binary";

function classifyAttachment(
	fileName: string,
	contentType: string | undefined,
): AttachmentKind {
	const normalizedType = (contentType || "").toLowerCase().split(";")[0].trim();
	if (IMAGE_MIME_TYPES.has(normalizedType)) return "image";
	if (normalizedType.startsWith("text/")) return "text";
	if (TEXT_MIME_TYPES.has(normalizedType)) return "text";

	if (TEXT_FILENAMES.has(fileName.toLowerCase().trim())) return "text";

	const ext = fileName.split(".").pop()?.toLowerCase() ?? "";
	if (TEXT_EXTENSIONS.has(ext)) return "text";

	return "binary";
}

/**
 * Returns all attachment-related MCP tools.
 */
export function getAttachmentTools(): Tool[] {
	return [
		{
			name: "list_task_attachments",
			description: "List all attachments for a task",
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
			name: "get_attachment_download_url",
			description: "Get a download URL for an attachment",
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
					attachmentId: {
						type: "string",
						description:
							"The technical UUID of the attachment (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_attachments to get the attachment ID.",
					},
				},
				required: ["projectId", "taskId", "attachmentId"],
			},
		},
		{
			name: "read_task_attachment",
			description:
				"Download and read the contents of a task attachment. Text-based files " +
				"(code, markdown, JSON, YAML, CSV, logs, etc.) are returned as plain text; " +
				"images (PNG, JPEG, GIF, WebP) are returned as a viewable image. Files over " +
				"2 MB (text) or 5 MB (images), and other binary formats (PDF, zip, docx, " +
				"etc.), can't be read this way — use get_attachment_download_url for those.",
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
					attachmentId: {
						type: "string",
						description:
							"The technical UUID of the attachment (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_attachments or get_task to get the attachment ID.",
					},
				},
				required: ["projectId", "taskId", "attachmentId"],
			},
		},
		{
			name: "delete_task_attachment",
			description: "Delete an attachment from a task",
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
					attachmentId: {
						type: "string",
						description:
							"The technical UUID of the attachment (e.g., '550e8400-e29b-41d4-a716-446655440000'). Use list_task_attachments to get the attachment ID.",
					},
				},
				required: ["projectId", "taskId", "attachmentId"],
			},
		},
	];
}

function formatAttachment(attachment: any): string {
	return `Attachment: ${attachment.file?.file_name || "Unknown"}
ID: ${attachment.id}
Size: ${attachment.file?.file_size || 0} bytes
Type: ${attachment.file?.content_type || "Unknown"}
Uploaded by: ${attachment.created_by || "Unknown"}
Uploaded at: ${attachment.created_at}`;
}

/**
 * Handles attachment tool calls.
 */
export async function handleAttachmentTool(
	toolName: string,
	args: any,
	viewsClient: PacaAPIViewsClient,
): Promise<any> {
	switch (toolName) {
		case "list_task_attachments": {
			const { projectId, taskId } = ListTaskAttachmentsSchema.parse(args);
			const attachments = await viewsClient.listTaskAttachments(
				projectId,
				taskId,
			);
			const formatted = formatList(attachments, formatAttachment);
			return {
				content: [
					{
						type: "text",
						text: `Attachments:\n\n${formatted}`,
					},
				],
			};
		}

		case "get_attachment_download_url": {
			const { projectId, taskId, attachmentId } =
				GetAttachmentDownloadURLSchema.parse(args);
			const result = await viewsClient.getAttachmentDownloadURL(
				projectId,
				taskId,
				attachmentId,
			);
			return {
				content: [
					{
						type: "text",
						text: `Download URL: ${result}`,
					},
				],
			};
		}

		case "read_task_attachment": {
			const { projectId, taskId, attachmentId } =
				ReadTaskAttachmentSchema.parse(args);

			const attachments = await viewsClient.listTaskAttachments(
				projectId,
				taskId,
			);
			const attachment = attachments.find((a: any) => a.id === attachmentId);
			if (!attachment) {
				return {
					content: [
						{
							type: "text",
							text: `Attachment ${attachmentId} not found on task ${taskId}.`,
						},
					],
					isError: true,
				};
			}

			const { file } = attachment;
			const kind = classifyAttachment(file.file_name, file.content_type);

			if (kind === "binary") {
				return {
					content: [
						{
							type: "text",
							text: `"${file.file_name}" (${file.content_type || "unknown type"}, ${formatFileSize(file.file_size)}) can't be read as text or an image. Use get_attachment_download_url to download it directly.`,
						},
					],
					isError: true,
				};
			}

			const maxBytes = kind === "image" ? MAX_IMAGE_BYTES : MAX_TEXT_BYTES;
			if (file.file_size > maxBytes) {
				return {
					content: [
						{
							type: "text",
							text: `"${file.file_name}" is ${formatFileSize(file.file_size)}, which exceeds the ${formatFileSize(maxBytes)} limit for reading ${kind === "image" ? "images" : "text files"} inline. Use get_attachment_download_url to download it directly.`,
						},
					],
					isError: true,
				};
			}

			const { buffer, contentType } =
				await viewsClient.downloadAttachmentContent(
					projectId,
					taskId,
					attachmentId,
				);
			const effectiveType =
				contentType?.split(";")[0]?.trim() ||
				file.content_type ||
				"application/octet-stream";

			if (kind === "image") {
				return {
					content: [
						{
							type: "image",
							data: Buffer.from(buffer).toString("base64"),
							mimeType: effectiveType,
						},
					],
				};
			}

			const text = Buffer.from(buffer).toString("utf-8");
			return {
				content: [
					{
						type: "text",
						text: `File: ${file.file_name} (${formatFileSize(file.file_size)})\n\n${text}`,
					},
				],
			};
		}

		case "delete_task_attachment": {
			const { projectId, taskId, attachmentId } =
				DeleteTaskAttachmentSchema.parse(args);
			await viewsClient.deleteTaskAttachment(projectId, taskId, attachmentId);
			return {
				content: [
					{
						type: "text",
						text: `Attachment ${attachmentId} deleted successfully`,
					},
				],
			};
		}

		default:
			throw new Error(`Unknown attachment tool: ${toolName}`);
	}
}
