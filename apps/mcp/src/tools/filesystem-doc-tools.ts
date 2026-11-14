import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIClient, PacaAPIDocClient } from "../api/index.js";
import { pagingFields, pagingProperties } from "../api/paginate.js";
import type { Document, DocumentFolder } from "../types/index.js";
import { blocknoteToMarkdown, markdownToBlocknote } from "../utils/index.js";

// ── Zod schemas ───────────────────────────────────────────────────────────────

const ListDocsSchema = z.object({
	projectId: z.string(),
	path: z.string().optional(),
	...pagingFields,
});

const ReadDocSchema = z
	.object({
		projectId: z.string(),
		path: z.string().optional(),
		// Lets a caller that already has a raw document ID (e.g. one attached
		// as chat context, which only carries an ID, never a virtual folder
		// path) skip path resolution entirely and go straight to
		// apiClient.getDocument. See the read_doc handler below.
		docId: z.string().optional(),
	})
	.refine((data) => !!data.path || !!data.docId, {
		message: "Either path or docId must be provided",
		path: ["path"],
	});

const WriteDocSchema = z.object({
	projectId: z.string(),
	path: z.string(),
	content: z.string(),
	note: z.string().max(500).optional(),
	task: z.string().max(50).optional(),
});

/** The note, task and client label a write records with its activity entry:
 *  only the non-empty ones. The client label comes from PACA_CLIENT. */
function writeNote(note?: string, task?: string) {
	const out: { note?: string; task?: string; client?: string } = {};
	if (note) out.note = note;
	if (task) out.task = task;
	const client = process.env.PACA_CLIENT?.slice(0, 200);
	if (client) out.client = client;
	return out;
}

const DeleteDocSchema = z.object({
	projectId: z.string(),
	path: z.string(),
});

const MoveDocSchema = z.object({
	projectId: z.string(),
	sourcePath: z.string(),
	destPath: z.string(),
});

// ── Internal types ────────────────────────────────────────────────────────────

type ResolvedPath =
	| { type: "root" }
	| { type: "folder"; folder: DocumentFolder }
	| { type: "doc"; doc: Document };

// ── Path helpers ──────────────────────────────────────────────────────────────
//
// A document path is resolved by the server in one request (GET/PUT
// /docs/by-path). A server from before those routes answers "unsupported", and
// so does a path that names a folder for the tools that also act on folders:
// then the path is resolved one component at a time with exact-name lookups
// against the folder / document lists, so no call ever reads the whole tree.

function parsePath(path: string): string[] {
	return path
		.split("/")
		.map((p) => p.trim())
		.filter((p) => p.length > 0);
}

async function findFolder(
	projectId: string,
	parentId: string | null,
	name: string,
	docClient: PacaAPIDocClient,
): Promise<DocumentFolder | undefined> {
	const page = await docClient.listFolders(projectId, {
		parentId,
		name,
		pageSize: 1,
	});
	return page.items[0];
}

async function findDoc(
	projectId: string,
	folderId: string | null,
	title: string,
	apiClient: PacaAPIClient,
): Promise<Document | undefined> {
	const page = await apiClient.listDocuments(projectId, {
		folderId: folderId ?? "null",
		title,
		pageSize: 1,
	});
	return page.items[0];
}

async function resolvePath(
	projectId: string,
	path: string,
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<ResolvedPath | null> {
	const parts = parsePath(path);
	if (parts.length === 0) return { type: "root" };

	let currentFolderId: string | null = null;
	for (let i = 0; i < parts.length - 1; i++) {
		const found = await findFolder(
			projectId,
			currentFolderId,
			parts[i],
			docClient,
		);
		if (!found) return null;
		currentFolderId = found.id;
	}

	const lastName = parts[parts.length - 1];
	const folder = await findFolder(
		projectId,
		currentFolderId,
		lastName,
		docClient,
	);
	if (folder) return { type: "folder", folder };

	const doc = await findDoc(projectId, currentFolderId, lastName, apiClient);
	if (doc) return { type: "doc", doc };

	return null;
}

async function ensureFolderPath(
	projectId: string,
	folderNames: string[],
	docClient: PacaAPIDocClient,
): Promise<string | null> {
	let currentFolderId: string | null = null;

	for (const name of folderNames) {
		let folder = await findFolder(projectId, currentFolderId, name, docClient);
		if (!folder) {
			folder = await docClient.createFolder(projectId, {
				name,
				parent_id: currentFolderId || undefined,
			});
		}
		currentFolderId = folder.id;
	}

	return currentFolderId;
}

/**
 * Resolves a path that may name a document or a folder (delete_doc,
 * move_doc): a document in one request where the server supports it, else
 * (a folder, or an older server) the folder walk. When a folder and a
 * document share the path's name, the server picks the document.
 */
async function resolveDocOrFolder(
	projectId: string,
	path: string,
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<ResolvedPath | null> {
	if (parsePath(path).length === 0) return { type: "root" };
	const byPath = await docClient.getByPath(projectId, path);
	if (byPath.status === "found") return { type: "doc", doc: byPath.doc };
	if (byPath.status === "not_found") return null;
	return resolvePath(projectId, path, apiClient, docClient);
}

function renderDoc(doc: Document, path: string) {
	const content = doc.content ? blocknoteToMarkdown(doc.content) : "";
	return {
		content: [
			{
				type: "text",
				text: `📄 ${doc.title}\nPath: ${path}\nUpdated: ${doc.updated_at}\n\n---\n\n${content}`,
			},
		],
	};
}

// ── Directory listing cursor ──────────────────────────────────────────────────
//
// A directory shows its folders first, then its documents; the two lists page
// separately, so the cursor says which one it resumes.

interface DirCursor {
	phase: "folders" | "docs";
	cursor?: string;
}

function encodeDirCursor(c: DirCursor): string {
	return Buffer.from(JSON.stringify(c)).toString("base64url");
}

function decodeDirCursor(raw: string | undefined): DirCursor {
	if (!raw) return { phase: "folders" };
	try {
		const c = JSON.parse(Buffer.from(raw, "base64url").toString("utf8"));
		if (c && (c.phase === "folders" || c.phase === "docs")) {
			return { phase: c.phase, cursor: c.cursor };
		}
	} catch {
		/* fall through */
	}
	throw new Error(
		"Invalid cursor: pass the cursor from the previous list_docs reply.",
	);
}

// ── Tool definitions ──────────────────────────────────────────────────────────

export function getFilesystemDocTools(): Tool[] {
	return [
		{
			name: "list_docs",
			description:
				"List one folder of a project's documentation: its subfolders first, then its documents, one page at a time. " +
				"Optionally name the folder by path (the top level when omitted) and open subfolders with further calls. " +
				"When more remain, the reply says so and gives a cursor to pass to the next call.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					path: {
						type: "string",
						description:
							"Optional folder path to list from (e.g. 'Architecture' or 'Guides/Setup'). " +
							"Omit or leave empty to list from the root.",
					},
					...pagingProperties,
				},
				required: ["projectId"],
			},
		},
		{
			name: "read_doc",
			description:
				"Read the Markdown content of a document, either by its path (e.g. 'Architecture/API Design') or directly by its technical UUID via docId. " +
				"Use list_docs first to discover available paths, or pass docId directly when you already have a document's ID (e.g. one attached as chat context) rather than its folder path — docId skips path resolution entirely. " +
				"Provide exactly one of path or docId.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					path: {
						type: "string",
						description:
							"Path to the document (e.g. 'Architecture/API Design' or just 'README'). " +
							"Folder and document names are separated by '/'. Omit if docId is provided.",
					},
					docId: {
						type: "string",
						description:
							"The technical UUID of the document to read directly, bypassing path resolution. Use this when you already have a document's ID (e.g. from context attached to this conversation) rather than its folder path. Omit if path is provided.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "write_doc",
			description:
				"Create or update a document at the given path. " +
				"If the document already exists it will be updated; otherwise it will be created. " +
				"Missing intermediate folders are created automatically. " +
				"Always prefer this over creating local files when writing project documentation.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					path: {
						type: "string",
						description:
							"Full path for the document (e.g. 'Architecture/API Design' or 'README'). " +
							"The last segment is the document title; preceding segments are folder names.",
					},
					content: {
						type: "string",
						description: "Document content in Markdown format.",
					},
					note: {
						type: "string",
						description:
							"Optional: why you made this change, in a sentence (at most 500 characters). " +
							"It is shown with the change in the document's activity. Plain text: no secrets.",
					},
					task: {
						type: "string",
						description:
							"Optional: the task this change belongs to, such as 'PACA-88' (at most 50 characters).",
					},
				},
				required: ["projectId", "path", "content"],
			},
		},
		{
			name: "delete_doc",
			description:
				"Delete a document or folder by path. " +
				"Deleting a folder also removes all documents and subfolders inside it.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					path: {
						type: "string",
						description: "Path to the document or folder to delete.",
					},
				},
				required: ["projectId", "path"],
			},
		},
		{
			name: "move_doc",
			description:
				"Move or rename a document or folder by specifying a source path and destination path. " +
				"Missing intermediate folders in the destination are created automatically.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					sourcePath: {
						type: "string",
						description: "Current path of the document or folder.",
					},
					destPath: {
						type: "string",
						description:
							"New path for the document or folder. The last segment becomes the new name.",
					},
				},
				required: ["projectId", "sourcePath", "destPath"],
			},
		},
	];
}

// ── Tool handler ──────────────────────────────────────────────────────────────

export async function handleFilesystemDocTool(
	toolName: string,
	args: unknown,
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<any> {
	switch (toolName) {
		case "list_docs": {
			const { projectId, path, cursor, pageSize } = ListDocsSchema.parse(args);

			let folderId: string | null = null;
			let header = "/";
			if (path && path.trim() !== "" && path.trim() !== "/") {
				const resolved = await resolvePath(
					projectId,
					path,
					apiClient,
					docClient,
				);
				if (!resolved) {
					return {
						content: [{ type: "text", text: `Path not found: ${path}` }],
						isError: true,
					};
				}
				if (resolved.type === "doc") {
					return {
						content: [
							{
								type: "text",
								text: `'${path}' is a document, not a folder. Use read_doc to read it.`,
							},
						],
						isError: true,
					};
				}
				if (resolved.type === "folder") {
					folderId = resolved.folder.id;
					header = path;
				}
			}

			const position = decodeDirCursor(cursor);
			const lines: string[] = [];
			let next: DirCursor | null = null;

			if (position.phase === "folders") {
				const folders = await docClient.listFolders(projectId, {
					parentId: folderId,
					cursor: position.cursor,
					pageSize,
				});
				lines.push(...folders.items.map((f) => `📁 ${f.name}/`));
				if (folders.nextCursor) {
					next = { phase: "folders", cursor: folders.nextCursor };
				}
			}
			if (!next) {
				// Folders are exhausted (or this call resumes at the documents).
				const docs = await apiClient.listDocuments(projectId, {
					folderId: folderId ?? "null",
					cursor: position.phase === "docs" ? position.cursor : undefined,
					pageSize,
				});
				lines.push(...docs.items.map((d) => `📄 ${d.title}`));
				if (docs.nextCursor) next = { phase: "docs", cursor: docs.nextCursor };
			}

			const listing = lines.length > 0 ? lines.join("\n") : "(empty)";
			const notice = next
				? `\n\n_More entries available — call list_docs again with cursor="${encodeDirCursor(next)}" for the next page._`
				: "";
			return {
				content: [{ type: "text", text: `📂 ${header}\n${listing}${notice}` }],
			};
		}

		case "read_doc": {
			const { projectId, path, docId } = ReadDocSchema.parse(args);

			// docId short-circuits straight to the API, skipping the
			// buildDocTree/resolvePath walk below entirely — needed for a
			// caller that only has a raw document ID (e.g. a doc injected as
			// chat context), which has no virtual folder path to resolve.
			if (docId) {
				const doc = await apiClient.getDocument(projectId, docId);
				const content = doc.content ? blocknoteToMarkdown(doc.content) : "";
				return {
					content: [
						{
							type: "text",
							text: `📄 ${doc.title}\nID: ${doc.id}\nUpdated: ${doc.updated_at}\n\n---\n\n${content}`,
						},
					],
				};
			}

			if (!path) {
				return {
					content: [
						{ type: "text", text: "Either path or docId must be provided." },
					],
					isError: true,
				};
			}

			const byPath =
				parsePath(path).length > 0
					? await docClient.getByPath(projectId, path)
					: ({ status: "folder" } as const);
			if (byPath.status === "found") {
				return renderDoc(byPath.doc, path);
			}
			if (byPath.status === "not_found") {
				return {
					content: [{ type: "text", text: `Not found: ${path}` }],
					isError: true,
				};
			}
			const resolved =
				byPath.status === "folder"
					? ({ type: "folder" } as const)
					: await resolvePath(projectId, path, apiClient, docClient);

			if (!resolved) {
				return {
					content: [{ type: "text", text: `Not found: ${path}` }],
					isError: true,
				};
			}
			if (resolved.type !== "doc") {
				return {
					content: [
						{
							type: "text",
							text: `'${path}' is a folder, not a document. Use list_docs to see its contents.`,
						},
					],
					isError: true,
				};
			}

			const doc = await apiClient.getDocument(projectId, resolved.doc.id);
			return renderDoc(doc, path);
		}

		case "write_doc": {
			const { projectId, path, content, note, task } =
				WriteDocSchema.parse(args);
			const parts = parsePath(path);

			if (parts.length === 0) {
				return {
					content: [
						{
							type: "text",
							text: "Path must include at least a document name.",
						},
					],
					isError: true,
				};
			}

			const written = await docClient.putByPath(projectId, {
				path,
				content: content ? markdownToBlocknote(content) : [],
				...writeNote(note, task),
			});
			if (written.status === "written") {
				const verb = written.created
					? "Created"
					: written.changed
						? "Updated"
						: "Unchanged";
				return {
					content: [{ type: "text", text: `${verb} document: ${path}` }],
				};
			}

			// An older server: create the folders and the document one call at a
			// time. It has nowhere to store a note, so none is sent.
			const folderNames = parts.slice(0, -1);
			const docTitle = parts[parts.length - 1];
			const folderId = await ensureFolderPath(
				projectId,
				folderNames,
				docClient,
			);

			const existing = await findDoc(projectId, folderId, docTitle, apiClient);

			if (existing) {
				await apiClient.updateDocument(projectId, existing.id, { content });
				return {
					content: [{ type: "text", text: `Updated document: ${path}` }],
				};
			}

			await apiClient.createDocument({
				project_id: projectId,
				title: docTitle,
				content,
				folder_id: folderId,
			});
			return {
				content: [{ type: "text", text: `Created document: ${path}` }],
			};
		}

		case "delete_doc": {
			const { projectId, path } = DeleteDocSchema.parse(args);
			const resolved = await resolveDocOrFolder(
				projectId,
				path,
				apiClient,
				docClient,
			);

			if (!resolved || resolved.type === "root") {
				return {
					content: [{ type: "text", text: `Not found: ${path}` }],
					isError: true,
				};
			}

			if (resolved.type === "doc") {
				await apiClient.deleteDocument(projectId, resolved.doc.id);
				return {
					content: [{ type: "text", text: `Deleted document: ${path}` }],
				};
			}

			await docClient.deleteFolder(projectId, resolved.folder.id);
			return {
				content: [{ type: "text", text: `Deleted folder: ${path}` }],
			};
		}

		case "move_doc": {
			const { projectId, sourcePath, destPath } = MoveDocSchema.parse(args);
			const source = await resolveDocOrFolder(
				projectId,
				sourcePath,
				apiClient,
				docClient,
			);

			if (!source || source.type === "root") {
				return {
					content: [{ type: "text", text: `Not found: ${sourcePath}` }],
					isError: true,
				};
			}

			const destParts = parsePath(destPath);
			if (destParts.length === 0) {
				return {
					content: [
						{
							type: "text",
							text: "Destination path must include at least a name.",
						},
					],
					isError: true,
				};
			}

			const destFolderNames = destParts.slice(0, -1);
			const destName = destParts[destParts.length - 1];
			const destFolderId = await ensureFolderPath(
				projectId,
				destFolderNames,
				docClient,
			);

			if (source.type === "doc") {
				await apiClient.updateDocument(projectId, source.doc.id, {
					title: destName,
					folder_id: destFolderId,
				});
				return {
					content: [
						{
							type: "text",
							text: `Moved document: ${sourcePath} → ${destPath}`,
						},
					],
				};
			}

			await docClient.updateFolder(projectId, source.folder.id, {
				name: destName,
				parent_id: destFolderId,
			});
			return {
				content: [
					{
						type: "text",
						text: `Moved folder: ${sourcePath} → ${destPath}`,
					},
				],
			};
		}

		default:
			throw new Error(`Unknown filesystem doc tool: ${toolName}`);
	}
}
