import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type {
	PacaAPIClient,
	PacaAPIDocClient,
	PacaAPIExtendedClient,
} from "../api/index.js";
import { pagingFields, pagingProperties } from "../api/paginate.js";
import type { Document, DocumentFolder } from "../types/index.js";
import { blocknoteToMarkdown, markdownToBlocknote } from "../utils/index.js";
import {
	findSection,
	type GrepLine,
	grepLines,
	type LineMatcher,
	literalMatcher,
	markdownLines,
	numberLines,
	outlineOf,
	parseLineRange,
	regexScan,
} from "../utils/markdown-sections.js";
import { editDocument, type LoadedDoc } from "./doc-edit.js";

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
		outline: z.boolean().optional(),
		section: z.string().min(1).optional(),
		lines: z.string().optional(),
	})
	.refine((data) => !!data.path || !!data.docId, {
		message: "Either path or docId must be provided",
		path: ["path"],
	});

const GrepDocsSchema = z.object({
	projectId: z.string(),
	pattern: z.string().min(1),
	regex: z.boolean().optional().default(false),
	ignoreCase: z.boolean().optional().default(false),
	path: z.string().optional(),
	context: z.number().int().min(0).max(5).optional().default(0),
	limit: z.number().int().min(1).max(200).optional().default(50),
});

const EditDocSchema = z
	.object({
		projectId: z.string(),
		path: z.string().optional(),
		docId: z.string().optional(),
		old_string: z.string().min(1),
		new_string: z.string(),
		replaceAll: z.boolean().optional().default(false),
		note: z.string().max(500).optional(),
		task: z.string().max(50).optional(),
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

function textResult(text: string) {
	return { content: [{ type: "text", text }] };
}

function errorResult(text: string) {
	return { content: [{ type: "text", text }], isError: true };
}

/** What part of a document read_doc returns; at most one is set. */
interface ReadView {
	outline?: boolean;
	section?: string;
	lines?: string;
}

/** The part of `markdown` the view asks for, numbered by line when a view is
 *  set (the whole text, unnumbered, otherwise). */
function renderView(
	markdown: string,
	view: ReadView,
): { text: string } | { error: string } {
	const set = [view.outline, view.section, view.lines].filter(
		(v) => v !== undefined && v !== false,
	).length;
	if (set > 1) {
		return { error: "Pass only one of outline, section or lines." };
	}
	if (view.outline) return { text: `Outline:\n${outlineOf(markdown)}` };

	const all = markdownLines(markdown);
	if (view.section !== undefined) {
		const found = findSection(markdown, view.section);
		if (found.status === "not_found") {
			return {
				error: `No heading "${view.section}" in this document. Read it with outline: true to list its headings.`,
			};
		}
		if (found.status === "ambiguous") {
			const list = found.matches
				.map((h) => `${h.line}: ${"#".repeat(h.level)} ${h.text}`)
				.join("\n");
			return {
				error: `"${view.section}" matches ${found.matches.length} headings:\n${list}\n\nRead the one you want with lines (for example lines: "${found.matches[0].line}-").`,
			};
		}
		return {
			text: `Section "${found.heading.text}": lines ${found.start}-${found.end} of ${all.length}\n\n${numberLines(all.slice(found.start - 1, found.end), found.start)}`,
		};
	}
	if (view.lines !== undefined) {
		const range = parseLineRange(view.lines, all.length);
		if ("error" in range) return range;
		return {
			text: `Lines ${range.start}-${range.end} of ${all.length}\n\n${numberLines(all.slice(range.start - 1, range.end), range.start)}`,
		};
	}
	return { text: markdown };
}

/** read_doc's reply: a header (title, where, last update, ETag) and the
 *  part of the document the view asks for. `where` is "Path: …" or "ID: …";
 *  without an ETag the content hash stands in (If-Match accepts either). */
function renderDoc(
	doc: Document,
	where: string,
	etag: string | undefined,
	view: ReadView = {},
) {
	const markdown = doc.content ? blocknoteToMarkdown(doc.content) : "";
	const body = renderView(markdown, view);
	if ("error" in body) return errorResult(body.error);
	const tag = etag ?? doc.content_hash;
	return textResult(
		`📄 ${doc.title}\n${where}\nUpdated: ${doc.updated_at}${tag ? `\nETag: ${tag}` : ""}\n\n---\n\n${body.text}`,
	);
}

// ── Reading a document for an edit ────────────────────────────────────────────

/** The document at `path` (or `docId`) and the If-Match value of that read:
 *  the by-path ETag, else the content hash. */
async function loadDoc(
	projectId: string,
	path: string | undefined,
	docId: string | undefined,
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<LoadedDoc | { error: string }> {
	if (docId) {
		const doc = await apiClient.getDocument(projectId, docId);
		return { doc, tag: doc.content_hash, label: doc.title };
	}
	const p = path ?? "";
	if (parsePath(p).length === 0) {
		return { error: "Path must include at least a document name." };
	}
	const byPath = await docClient.getByPath(projectId, p);
	if (byPath.status === "found") {
		return {
			doc: byPath.doc,
			tag: byPath.etag ?? byPath.doc.content_hash,
			label: p,
		};
	}
	if (byPath.status === "not_found") return { error: `Not found: ${p}` };
	const resolved =
		byPath.status === "folder"
			? ({ type: "folder" } as const)
			: await resolvePath(projectId, p, apiClient, docClient);
	if (!resolved) return { error: `Not found: ${p}` };
	if (resolved.type !== "doc") {
		return { error: `'${p}' is a folder, not a document.` };
	}
	const doc = await apiClient.getDocument(projectId, resolved.doc.id);
	return { doc, tag: doc.content_hash, label: p };
}

/** "Full Name (@username)" for a user id, from the project's members. */
async function memberName(
	extendedClient: PacaAPIExtendedClient | undefined,
	projectId: string,
	userId: string,
): Promise<string | undefined> {
	if (!extendedClient) return undefined;
	let cursor: string | undefined;
	for (let page = 0; page < 3; page++) {
		const res = await extendedClient.listProjectMembers(projectId, {
			cursor,
			pageSize: 100,
		});
		const m = res.items.find((x) => x.user_id === userId || x.id === userId);
		if (m) return m.username ? `${m.full_name} (@${m.username})` : m.full_name;
		if (!res.nextCursor) return undefined;
		cursor = res.nextCursor;
	}
	return undefined;
}

// ── grep_docs ─────────────────────────────────────────────────────────────────

/** The most hits GET /docs/search returns. */
const SEARCH_MAX = 50;
/** The most documents one grep_docs call reads. */
const GREP_MAX_DOCS = 200;
/** How long a grep_docs regex may run on one document, and in all. */
const REGEX_DOC_MS = 500;
const REGEX_TOTAL_MS = 10_000;
/** After this many documents where the regex ran too long, it stops. */
const REGEX_MAX_SLOW = 3;
/** The most ids one POST /docs/batch-get takes. */
const BATCH_MAX = 100;

interface GrepCandidate {
	id: string;
	path: string;
}

/** The documents in `folderId` and every folder below it, breadth first,
 *  stopping once more than `cap` are found. */
async function scanFolder(
	projectId: string,
	folderId: string | null,
	prefix: string,
	cap: number,
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<{ items: GrepCandidate[]; truncated: boolean }> {
	const items: GrepCandidate[] = [];
	const queue: Array<{ id: string | null; prefix: string }> = [
		{ id: folderId, prefix },
	];
	while (queue.length > 0 && items.length <= cap) {
		const folder = queue.shift() as { id: string | null; prefix: string };
		let cursor: string | undefined;
		do {
			const page = await apiClient.listDocuments(projectId, {
				folderId: folder.id ?? "null",
				cursor,
				pageSize: 100,
			});
			for (const d of page.items) {
				items.push({ id: d.id, path: folder.prefix + d.title });
			}
			cursor = page.nextCursor ?? undefined;
		} while (cursor && items.length <= cap);
		cursor = undefined;
		do {
			const page = await docClient.listFolders(projectId, {
				parentId: folder.id,
				cursor,
				pageSize: 100,
			});
			for (const f of page.items) {
				queue.push({ id: f.id, prefix: `${folder.prefix}${f.name}/` });
			}
			cursor = page.nextCursor ?? undefined;
		} while (cursor);
	}
	return { items: items.slice(0, cap), truncated: items.length > cap };
}

/** The documents grep_docs reads when `path` is given: that document, or
 *  the documents under that folder. */
async function grepScope(
	projectId: string,
	path: string,
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<{ items: GrepCandidate[]; truncated: boolean } | { error: string }> {
	const parts = parsePath(path);
	if (parts.length === 0) {
		return scanFolder(projectId, null, "", GREP_MAX_DOCS, apiClient, docClient);
	}
	const byPath = await docClient.getByPath(projectId, path);
	if (byPath.status === "found") {
		return {
			items: [{ id: byPath.doc.id, path: parts.join("/") }],
			truncated: false,
		};
	}
	if (byPath.status === "not_found") return { error: `Not found: ${path}` };
	const resolved = await resolvePath(projectId, path, apiClient, docClient);
	if (!resolved) return { error: `Not found: ${path}` };
	if (resolved.type === "doc") {
		return {
			items: [{ id: resolved.doc.id, path: parts.join("/") }],
			truncated: false,
		};
	}
	return scanFolder(
		projectId,
		resolved.type === "folder" ? resolved.folder.id : null,
		resolved.type === "folder" ? `${parts.join("/")}/` : "",
		GREP_MAX_DOCS,
		apiClient,
		docClient,
	);
}

/** The documents with these ids, with content: batch-get 100 at a time, or
 *  one read each on a server without it. Unreadable ones are left out. */
async function fetchDocs(
	projectId: string,
	ids: string[],
	apiClient: PacaAPIClient,
	docClient: PacaAPIDocClient,
): Promise<{ docs: Document[]; failed: number }> {
	const out: Document[] = [];
	let failed = 0;
	for (let i = 0; i < ids.length; i += BATCH_MAX) {
		const chunk = ids.slice(i, i + BATCH_MAX);
		const res = await docClient.batchGet(projectId, chunk);
		if (res.status === "ok") {
			out.push(...res.items);
			continue;
		}
		const rest = ids.slice(i);
		for (let j = 0; j < rest.length; j += 10) {
			const got = await Promise.all(
				rest.slice(j, j + 10).map((id) =>
					apiClient.getDocument(projectId, id).then(
						(d) => d,
						() => null,
					),
				),
			);
			for (const d of got) {
				if (d) out.push(d);
				else failed++;
			}
		}
		break;
	}
	return { docs: out, failed };
}

/**
 * Whether the server search can stand in for scanning: a single word with
 * no Markdown syntax. Search matches one formatting run of the stored text,
 * so a phrase may be split by bold or a link, and Markdown that read_doc
 * shows (`##`, `- [ ]`, backticks, link brackets, table pipes) is not in the
 * stored text at all.
 */
function searchable(pattern: string): boolean {
	return (
		/^[\p{L}\p{N}](?:[\p{L}\p{N}.,:;'"!?@/+=-]|_(?=[\p{L}\p{N}]))*$/u.test(
			pattern,
		) && !/^\d+[.)]$/.test(pattern)
	);
}

/**
 * The word the server search looks for to find candidate documents: the
 * pattern itself when it is one plain word, otherwise its longest run of
 * letters and digits of three or more (null when it has none).
 */
function searchWord(pattern: string): string | null {
	if (searchable(pattern)) return pattern;
	let best: string | null = null;
	for (const w of pattern.match(/[\p{L}\p{N}]+/gu) ?? []) {
		if (w.length >= 3 && (!best || w.length > best.length)) best = w;
	}
	return best;
}

function grepOutput(path: string, groups: GrepLine[][], context: number) {
	return groups
		.map((g) =>
			g
				.map((l) =>
					l.match
						? `${path}:${l.line}: ${l.text}`
						: `${path}-${l.line}- ${l.text}`,
				)
				.join("\n"),
		)
		.join(context > 0 ? "\n--\n" : "\n");
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
				"Provide exactly one of path or docId. " +
				"For a long document, read part of it: outline: true lists the headings with their line numbers, " +
				"section returns one heading's part, lines returns a line range. Those replies number each line; " +
				"the numbers are lines of the full Markdown, the same ones grep_docs reports. " +
				"The header carries the document's ETag.",
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
					outline: {
						type: "boolean",
						description:
							"Return only the headings, as '<line>: ## Heading', and the document's line count.",
					},
					section: {
						type: "string",
						description:
							"Return one section: the heading with this text (case-insensitive) down to the next heading of the same or a higher level.",
					},
					lines: {
						type: "string",
						description:
							"Return a line range such as '40-80' (also '40' or '40-'); it is clamped to the document.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "grep_docs",
			description:
				"Find lines in a project's documents, like grep: prints 'Folder/Title:12: text' for each matching line " +
				"(context lines as 'Folder/Title-11- text', groups separated by '--'). Line numbers are lines of the Markdown read_doc returns. " +
				"Patterns match the Markdown text, so Markdown syntax (## Heading, * [ ] task, `code`) can be part of a pattern; a literal also matches with ** and _ removed, so 'foo bar' finds 'foo **bar**'. " +
				"Without path, a literal is first narrowed to candidate documents across the project through the server's search for its longest word (which may miss a word split by formatting); " +
				"a regex, a path, a pattern with no word of three letters, or no match among the candidates scans the documents under path (the whole project when absent), at most 200 per call. " +
				"A regex gets 0.5 s per document and 10 s in all: a document where it runs longer is skipped and named in a notice, and the search stops after 3 such documents. " +
				"For many repeated searches, a local mirror and rg are cheaper.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					pattern: {
						type: "string",
						description:
							"The text to find, or a JavaScript regular expression when regex is true.",
					},
					regex: {
						type: "boolean",
						description:
							"Treat pattern as a regular expression (default false).",
					},
					ignoreCase: {
						type: "boolean",
						description: "Match without regard to case (default false).",
					},
					path: {
						type: "string",
						description:
							"Optional folder (or document) path to limit the search to, such as 'Guides'.",
					},
					context: {
						type: "number",
						description:
							"Lines of context to show around each match, 0 to 5 (default 0).",
					},
					limit: {
						type: "number",
						description:
							"The most matching lines to return, 1 to 200 (default 50).",
					},
				},
				required: ["projectId", "pattern"],
			},
		},
		{
			name: "edit_doc",
			description:
				"Replace an exact string in a document, like a file edit: only the blocks the text touches are rewritten; " +
				"every other block is sent back exactly as stored. Prefer this to write_doc for changing part of a document. " +
				"old_string must match the Markdown read_doc returns, including line breaks, and be unique unless replaceAll is set. " +
				"If someone else changed the document since it was read, the edit is re-applied to their version once; " +
				"if that fails, the reply says who changed it and why. A locked managed copy of a repository file cannot be edited here.",
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
							"Path to the document (e.g. 'Architecture/API Design'). Omit if docId is provided.",
					},
					docId: {
						type: "string",
						description:
							"The technical UUID of the document. Omit if path is provided.",
					},
					old_string: {
						type: "string",
						description: "The exact Markdown text to replace.",
					},
					new_string: {
						type: "string",
						description:
							"The Markdown text to put in its place (empty to delete it).",
					},
					replaceAll: {
						type: "boolean",
						description:
							"Replace every occurrence of old_string (default false).",
					},
					note: {
						type: "string",
						description:
							"Optional: why you made this change, in a sentence (at most 500 characters). " +
							"It is shown with the change in the document's activity and to anyone whose edit conflicts with it. Plain text: no secrets.",
					},
					task: {
						type: "string",
						description:
							"Optional: the task this change belongs to, such as 'PACA-88' (at most 50 characters).",
					},
				},
				required: ["projectId", "old_string", "new_string"],
			},
		},
		{
			name: "write_doc",
			description:
				"Create or update a document at the given path. " +
				"If the document already exists it will be updated; otherwise it will be created. " +
				"Missing intermediate folders are created automatically. " +
				"Always prefer this over creating local files when writing project documentation. " +
				"To change part of an existing document, use edit_doc instead: it rewrites only what changed.",
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
	extendedClient?: PacaAPIExtendedClient,
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
			const { projectId, path, docId, outline, section, lines } =
				ReadDocSchema.parse(args);
			const view: ReadView = { outline, section, lines };

			// docId short-circuits straight to the API, skipping the
			// buildDocTree/resolvePath walk below entirely — needed for a
			// caller that only has a raw document ID (e.g. a doc injected as
			// chat context), which has no virtual folder path to resolve.
			if (docId) {
				const doc = await apiClient.getDocument(projectId, docId);
				return renderDoc(doc, `ID: ${doc.id}`, undefined, view);
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
				return renderDoc(byPath.doc, `Path: ${path}`, byPath.etag, view);
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
			return renderDoc(doc, `Path: ${path}`, undefined, view);
		}

		case "grep_docs": {
			const { projectId, pattern, regex, ignoreCase, path, context, limit } =
				GrepDocsSchema.parse(args);
			if (regex) {
				try {
					new RegExp(pattern, ignoreCase ? "i" : "");
				} catch (err) {
					return errorResult(
						`Invalid regular expression: ${(err as Error).message}`,
					);
				}
			}

			// Reads the candidates and matches them; null when a literal found
			// nothing among search candidates (the caller then scans).
			const run = async (
				candidates: GrepCandidate[],
				notices: string[],
				fromSearch: boolean,
			) => {
				const fetched = await fetchDocs(
					projectId,
					candidates.map((c) => c.id),
					apiClient,
					docClient,
				);
				const docs = new Map(fetched.docs.map((d) => [d.id, d]));
				const found = candidates.flatMap((c) => {
					const doc = docs.get(c.id);
					if (!doc) return [];
					const markdown = doc.content ? blocknoteToMarkdown(doc.content) : "";
					return [{ path: c.path, markdown }];
				});

				// Literal matching is linear; a regex runs in a worker thread
				// with a time limit per document, so a catastrophic pattern
				// cannot block the server.
				let matchers: LineMatcher[];
				let notSearched = 0;
				const extra: string[] = [];
				if (regex) {
					const scan = await regexScan(
						pattern,
						ignoreCase,
						found.map((f) => f.markdown),
						{
							perDocMs: REGEX_DOC_MS,
							totalMs: REGEX_TOTAL_MS,
							maxTimeouts: REGEX_MAX_SLOW,
						},
					);
					matchers = scan.hits.map((h) => {
						const set = new Set(h ?? []);
						return (_line: string, index: number) => set.has(index);
					});
					notSearched = scan.timedOut.length + scan.unsearched.length;
					if (scan.timedOut.length > 0) {
						extra.push(
							`The regular expression ran longer than ${REGEX_DOC_MS} ms and was stopped in: ${scan.timedOut.map((i) => found[i].path).join(", ")}.`,
						);
					}
					if (scan.unsearched.length > 0) {
						const n = scan.unsearched.length;
						extra.push(
							scan.timedOut.length >= REGEX_MAX_SLOW
								? `Stopped after ${REGEX_MAX_SLOW} slow documents: ${n} more ${n === 1 ? "was" : "were"} not searched. Simplify the regular expression (avoid nested quantifiers such as (a+)+) or narrow with path.`
								: `Stopped at the time limit of ${REGEX_TOTAL_MS / 1000} s: ${n} more document${n === 1 ? " was" : "s were"} not searched. Narrow with path or simplify the regular expression.`,
						);
					}
				} else {
					const m = literalMatcher(pattern, ignoreCase);
					matchers = found.map(() => m);
				}

				const blocks: string[] = [];
				let total = 0;
				let withMatches = 0;
				for (let i = 0; i < found.length; i++) {
					const res = grepLines(
						found[i].markdown,
						matchers[i],
						context,
						limit - total,
					);
					if (res.matches > 0) {
						withMatches++;
						total += res.matches;
						blocks.push(grepOutput(found[i].path, res.groups, context));
					}
					if (total >= limit) {
						if (res.truncated || i < found.length - 1) {
							extra.push(
								`Stopped at the limit of ${limit} matching lines; there may be more. Raise limit (at most 200) or narrow with path.`,
							);
						}
						break;
					}
				}
				if (fromSearch && total === 0) return null;
				if (fetched.failed > 0) {
					notices.push(
						`${fetched.failed} document${fetched.failed === 1 ? "" : "s"} could not be read (server error or no access) and ${fetched.failed === 1 ? "was" : "were"} not searched.`,
					);
				}
				notices.push(...extra);
				const searched = found.length - notSearched;
				const tail = notices.length > 0 ? `\n\n${notices.join("\n")}` : "";
				if (total === 0) {
					return textResult(
						`No matches for "${pattern}" in ${searched} document${searched === 1 ? "" : "s"} searched.${tail}`,
					);
				}
				return textResult(
					`${total} matching line${total === 1 ? "" : "s"} in ${withMatches} document${withMatches === 1 ? "" : "s"} (${searched} searched):\n\n${blocks.join(context > 0 ? "\n--\n" : "\n")}${tail}`,
				);
			};

			// A literal without path: the server's search for its longest
			// word gives candidate documents across the whole project. With
			// no candidate or no match among them, scan instead.
			const word = regex || path?.trim() ? null : searchWord(pattern);
			if (word) {
				const hits = await docClient.searchDocuments(
					projectId,
					word,
					SEARCH_MAX,
				);
				if (hits.length > 0) {
					const chains = new Map<string, string>();
					for (const id of new Set(hits.map((h) => h.folder_id))) {
						if (!id) continue;
						const chain = await docClient
							.getFolderPath(projectId, id)
							.catch(() => [] as DocumentFolder[]);
						chains.set(id, chain.map((f) => `${f.name}/`).join(""));
					}
					const notices: string[] = [];
					if (hits.length === SEARCH_MAX) {
						notices.push(
							`The server search returned ${SEARCH_MAX} documents, the most it returns, so some documents may be missing: pass path to scan a folder instead.`,
						);
					}
					const result = await run(
						hits.map((h) => ({
							id: h.id,
							path:
								(h.folder_id ? (chains.get(h.folder_id) ?? "") : "") + h.title,
						})),
						notices,
						true,
					);
					if (result) return result;
				}
			}

			const scope = await grepScope(
				projectId,
				path ?? "",
				apiClient,
				docClient,
			);
			if ("error" in scope) return errorResult(scope.error);
			const notices: string[] = [];
			if (scope.truncated) {
				notices.push(
					`Searched only the first ${GREP_MAX_DOCS} documents${path ? ` under ${path}` : ""}: pass a narrower path to search the rest.`,
				);
			}
			return (await run(scope.items, notices, false)) as ReturnType<
				typeof textResult
			>;
		}

		case "edit_doc": {
			const {
				projectId,
				path,
				docId,
				old_string,
				new_string,
				replaceAll,
				note,
				task,
			} = EditDocSchema.parse(args);
			return editDocument(
				{
					load: () => loadDoc(projectId, path, docId, apiClient, docClient),
					write: (doc, blocks, tag) =>
						docClient.patchDocument(
							projectId,
							doc.id,
							{ content: blocks, ...writeNote(note, task) },
							tag ? { ifMatch: tag } : {},
						),
					writerName: (userId) => memberName(extendedClient, projectId, userId),
				},
				{ oldString: old_string, newString: new_string, replaceAll },
			);
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
