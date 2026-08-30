import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIClient, PacaAPIDocClient } from "../api/index.js";
import type { Document, DocumentFolder } from "../types/index.js";
import { blocknoteToMarkdown } from "../utils/index.js";

// ── Zod schemas ───────────────────────────────────────────────────────────────

const ListDocsSchema = z.object({
	projectId: z.string(),
	path: z.string().optional(),
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
});

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

type FoldersByParent = Map<string | null, DocumentFolder[]>;
type DocsByFolder = Map<string | null, Document[]>;

interface DocTree {
	folders: DocumentFolder[];
	documents: Document[];
	foldersByParent: FoldersByParent;
	docsByFolder: DocsByFolder;
	foldersById: Map<string, DocumentFolder>;
}

type ResolvedPath =
	| { type: "root" }
	| { type: "folder"; folder: DocumentFolder }
	| { type: "doc"; doc: Document };

// ── Tree building ─────────────────────────────────────────────────────────────

async function buildDocTree(
	projectId: string,
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<DocTree> {
	const [folders, documents] = await Promise.all([
		docClient.listFolders(projectId),
		apiClient.listDocuments(projectId),
	]);

	const foldersByParent: FoldersByParent = new Map();
	const foldersById = new Map<string, DocumentFolder>();

	for (const f of folders) {
		const pid = f.parent_id || null;
		if (!foldersByParent.has(pid)) foldersByParent.set(pid, []);
		foldersByParent.get(pid)?.push(f);
		foldersById.set(f.id, f);
	}

	const docsByFolder: DocsByFolder = new Map();
	for (const d of documents) {
		const fid = d.folder_id || null;
		if (!docsByFolder.has(fid)) docsByFolder.set(fid, []);
		docsByFolder.get(fid)?.push(d);
	}

	return { folders, documents, foldersByParent, docsByFolder, foldersById };
}

// ── Tree rendering ────────────────────────────────────────────────────────────

function renderLines(parentId: string | null, tree: DocTree): string[] {
	const childFolders = (tree.foldersByParent.get(parentId) || [])
		.slice()
		.sort((a, b) => a.position - b.position || a.name.localeCompare(b.name));
	const childDocs = (tree.docsByFolder.get(parentId) || [])
		.slice()
		.sort((a, b) => a.position - b.position || a.title.localeCompare(b.title));

	type Entry = { label: string; folderId?: string };
	const entries: Entry[] = [
		...childFolders.map((f) => ({ label: `📁 ${f.name}/`, folderId: f.id })),
		...childDocs.map((d) => ({ label: `📄 ${d.title}` })),
	];

	const lines: string[] = [];
	entries.forEach((entry, i) => {
		const last = i === entries.length - 1;
		const connector = last ? "└── " : "├── ";
		const childIndent = last ? "    " : "│   ";
		lines.push(connector + entry.label);
		if (entry.folderId) {
			for (const child of renderLines(entry.folderId, tree)) {
				lines.push(childIndent + child);
			}
		}
	});
	return lines;
}

// ── Path helpers ──────────────────────────────────────────────────────────────

function parsePath(path: string): string[] {
	return path
		.split("/")
		.map((p) => p.trim())
		.filter((p) => p.length > 0);
}

function resolvePath(path: string, tree: DocTree): ResolvedPath | null {
	const parts = parsePath(path);
	if (parts.length === 0) return { type: "root" };

	let currentFolderId: string | null = null;

	for (let i = 0; i < parts.length - 1; i++) {
		const name = parts[i];
		const siblings: DocumentFolder[] =
			tree.foldersByParent.get(currentFolderId) || [];
		const found: DocumentFolder | undefined = siblings.find(
			(f: DocumentFolder) => f.name === name,
		);
		if (!found) return null;
		currentFolderId = found.id;
	}

	const lastName = parts[parts.length - 1];

	const siblingFolders: DocumentFolder[] =
		tree.foldersByParent.get(currentFolderId) || [];
	const folder: DocumentFolder | undefined = siblingFolders.find(
		(f: DocumentFolder) => f.name === lastName,
	);
	if (folder) return { type: "folder", folder };

	const siblingDocs: Document[] = tree.docsByFolder.get(currentFolderId) || [];
	const doc: Document | undefined = siblingDocs.find(
		(d: Document) => d.title === lastName,
	);
	if (doc) return { type: "doc", doc };

	return null;
}

async function ensureFolderPath(
	projectId: string,
	folderNames: string[],
	docClient: PacaAPIDocClient,
	tree: DocTree,
): Promise<string | null> {
	let currentFolderId: string | null = null;

	for (const name of folderNames) {
		const siblings: DocumentFolder[] =
			tree.foldersByParent.get(currentFolderId) || [];
		let folder: DocumentFolder | undefined = siblings.find(
			(f: DocumentFolder) => f.name === name,
		);

		if (!folder) {
			folder = await docClient.createFolder(projectId, {
				name,
				parent_id: currentFolderId || undefined,
			});
			if (!tree.foldersByParent.has(currentFolderId)) {
				tree.foldersByParent.set(currentFolderId, []);
			}
			tree.foldersByParent.get(currentFolderId)?.push(folder);
			tree.foldersById.set(folder.id, folder);
		}

		currentFolderId = folder.id;
	}

	return currentFolderId;
}

// ── Tool definitions ──────────────────────────────────────────────────────────

export function getFilesystemDocTools(): Tool[] {
	return [
		{
			name: "list_docs",
			description:
				"List the documentation tree for a project, showing folders and documents in a filesystem-like hierarchy. " +
				"Optionally start from a specific folder path. Use this to explore the project documentation structure.",
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
			const { projectId, path } = ListDocsSchema.parse(args);
			const tree = await buildDocTree(projectId, apiClient, docClient);

			let rootId: string | null = null;
			let header = "/";

			if (path && path.trim() !== "" && path.trim() !== "/") {
				const resolved = resolvePath(path, tree);
				if (!resolved) {
					return {
						content: [{ type: "text", text: `Path not found: ${path}` }],
						isError: true,
					};
				} else if (resolved.type === "doc") {
					return {
						content: [
							{
								type: "text",
								text: `'${path}' is a document, not a folder. Use read_doc to read it.`,
							},
						],
						isError: true,
					};
				} else if (resolved.type === "folder") {
					rootId = resolved.folder.id;
					header = path;
				}
			}

			const lines = renderLines(rootId, tree);
			const treeStr = lines.length > 0 ? lines.join("\n") : "(empty)";
			return {
				content: [{ type: "text", text: `📂 ${header}\n${treeStr}` }],
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

			const tree = await buildDocTree(projectId, apiClient, docClient);
			const resolved = resolvePath(path, tree);

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

		case "write_doc": {
			const { projectId, path, content } = WriteDocSchema.parse(args);
			const tree = await buildDocTree(projectId, apiClient, docClient);
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

			const folderNames = parts.slice(0, -1);
			const docTitle = parts[parts.length - 1];
			const folderId = await ensureFolderPath(
				projectId,
				folderNames,
				docClient,
				tree,
			);

			const siblingDocs = tree.docsByFolder.get(folderId) || [];
			const existing = siblingDocs.find((d) => d.title === docTitle);

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
			const tree = await buildDocTree(projectId, apiClient, docClient);
			const resolved = resolvePath(path, tree);

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
			const tree = await buildDocTree(projectId, apiClient, docClient);
			const source = resolvePath(sourcePath, tree);

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
				tree,
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
