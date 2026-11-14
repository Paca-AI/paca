import { describe, expect, it, vi } from "vitest";

vi.mock("../../utils/index.js", () => ({
	blocknoteToMarkdown: vi.fn(() => "mocked markdown"),
	markdownToBlocknote: vi.fn((md: string) => [
		{ type: "paragraph", content: [{ type: "text", text: md }] },
	]),
}));

import {
	getFilesystemDocTools,
	handleFilesystemDocTool,
} from "../../tools/filesystem-doc-tools.js";

// ── Shared test fixtures ───────────────────────────────────────────────────────

const FOLDER_ROOT = {
	id: "f1",
	parent_id: null,
	name: "Architecture",
	position: 0,
};
const FOLDER_NESTED = { id: "f2", parent_id: "f1", name: "API", position: 0 };

const DOC_ROOT = { id: "d1", folder_id: null, title: "README", position: 0 };
const DOC_IN_FOLDER = {
	id: "d2",
	folder_id: "f1",
	title: "Overview",
	position: 0,
};
const DOC_IN_NESTED = {
	id: "d3",
	folder_id: "f2",
	title: "Endpoints",
	position: 0,
};

/** Slices `all` the way the API pages: an offset-style cursor stands in for the opaque one. */
function pageOf<T>(all: T[], opts: { cursor?: string; pageSize?: number }) {
	const start = opts.cursor ? Number(opts.cursor) : 0;
	const size = opts.pageSize ?? 50;
	const items = all.slice(start, start + size);
	const more = start + size < all.length;
	return { items, nextCursor: more ? String(start + size) : null };
}

/** Mock API client serving the documents the way the list endpoint filters them. */
function makeApiClient(
	opts: { folders?: any[]; documents?: any[]; getDocumentContent?: any } = {},
) {
	const documents = opts.documents ?? [DOC_ROOT, DOC_IN_FOLDER, DOC_IN_NESTED];

	return {
		listDocuments: vi.fn().mockImplementation(
			async (
				_pid: string,
				o: {
					folderId?: string;
					title?: string;
					cursor?: string;
					pageSize?: number;
				} = {},
			) => {
				const wanted = o.folderId === "null" ? null : (o.folderId ?? null);
				const matches = documents.filter(
					(d) =>
						(d.folder_id ?? null) === wanted &&
						(!o.title || d.title.toLowerCase() === o.title.toLowerCase()),
				);
				return pageOf(matches, o);
			},
		),
		getDocument: vi.fn().mockResolvedValue({
			id: "d1",
			title: "README",
			updated_at: "2024-01-01T00:00:00Z",
			content: opts.getDocumentContent ?? [{ type: "paragraph" }],
		}),
		createDocument: vi.fn().mockResolvedValue({ id: "d99", title: "New" }),
		updateDocument: vi.fn().mockResolvedValue({ id: "d1", title: "Updated" }),
		deleteDocument: vi.fn().mockResolvedValue(undefined),
	} as any;
}

/** The doc client of a server without the by-path routes (the folder walk is
 *  used), unless `byPath` supplies those methods. */
function makeDocClient(opts: { folders?: any[]; byPath?: any } = {}) {
	const folders = opts.folders ?? [FOLDER_ROOT, FOLDER_NESTED];

	return {
		getByPath: vi.fn().mockResolvedValue({ status: "unsupported" }),
		putByPath: vi.fn().mockResolvedValue({ status: "unsupported" }),
		...opts.byPath,
		listFolders: vi.fn().mockImplementation(
			async (
				_pid: string,
				o: {
					parentId?: string | null;
					name?: string;
					cursor?: string;
					pageSize?: number;
				} = {},
			) => {
				const matches = folders.filter(
					(f) =>
						(f.parent_id ?? null) === (o.parentId ?? null) &&
						(!o.name || f.name.toLowerCase() === o.name.toLowerCase()),
				);
				return pageOf(matches, o);
			},
		),
		createFolder: vi.fn().mockImplementation((_pid: string, input: any) =>
			Promise.resolve({
				id: "fnew",
				name: input.name,
				parent_id: input.parent_id ?? null,
				position: 99,
			}),
		),
		updateFolder: vi.fn().mockResolvedValue({ id: "f1", name: "Renamed" }),
		deleteFolder: vi.fn().mockResolvedValue(undefined),
	} as any;
}

// ── Tool definitions ───────────────────────────────────────────────────────────

describe("getFilesystemDocTools", () => {
	it("returns 5 tools", () => {
		expect(getFilesystemDocTools()).toHaveLength(5);
	});

	it("includes all expected tool names", () => {
		const names = getFilesystemDocTools().map((t) => t.name);
		for (const n of [
			"list_docs",
			"read_doc",
			"write_doc",
			"delete_doc",
			"move_doc",
		]) {
			expect(names).toContain(n);
		}
	});
});

// ── list_docs ──────────────────────────────────────────────────────────────────

describe("handleFilesystemDocTool – list_docs", () => {
	it("renders root tree when no path provided", async () => {
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.content[0].text).toContain("Architecture");
		expect(result.content[0].text).toContain("README");
	});

	it("renders subtree when a valid folder path is provided", async () => {
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", path: "Architecture" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.content[0].text).toContain("API");
		expect(result.content[0].text).toContain("Overview");
	});

	it("returns isError when path does not exist", async () => {
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", path: "Nonexistent" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("not found");
	});

	it("returns isError when path resolves to a document (not a folder)", async () => {
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", path: "README" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("document");
	});

	it("shows (empty) when folder has no children", async () => {
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", path: "Architecture/API" },
			makeApiClient({ documents: [DOC_ROOT, DOC_IN_FOLDER] }),
			makeDocClient(),
		);
		expect(result.content[0].text).toContain("(empty)");
	});

	it("renders root (/) when path is empty string", async () => {
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", path: "" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.content[0].text).toContain("📂 /");
	});
});

// ── read_doc ───────────────────────────────────────────────────────────────────

describe("handleFilesystemDocTool – read_doc", () => {
	it("returns document content at root path", async () => {
		const apiClient = makeApiClient();
		const result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "README" },
			apiClient,
			makeDocClient(),
		);
		expect(apiClient.getDocument).toHaveBeenCalledWith("p1", "d1");
		expect(result.content[0].text).toContain("mocked markdown");
	});

	it("returns document content at nested path", async () => {
		const apiClient = makeApiClient();
		const _result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "Architecture/Overview" },
			apiClient,
			makeDocClient(),
		);
		expect(apiClient.getDocument).toHaveBeenCalledWith("p1", "d2");
	});

	it("returns isError when path not found", async () => {
		const result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "Missing/Doc" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("Not found");
	});

	it("returns isError when path resolves to a folder", async () => {
		const result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "Architecture" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("folder");
	});

	it("returns empty string when document content is null/undefined", async () => {
		const result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "README" },
			makeApiClient({ getDocumentContent: null }),
			makeDocClient(),
		);
		expect(result.content[0].text).toBeDefined();
	});
});

// ── read_doc – docId ───────────────────────────────────────────────────────────
// A doc "injected" as chat context only carries a raw ID, never a virtual
// folder path — docId must skip buildDocTree/resolvePath entirely and call
// apiClient.getDocument directly (see filesystem-doc-tools.ts's read_doc case).

describe("handleFilesystemDocTool – read_doc with docId", () => {
	it("calls apiClient.getDocument directly, bypassing path resolution", async () => {
		const apiClient = makeApiClient();
		const docClient = makeDocClient();
		const result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", docId: "d1" },
			apiClient,
			docClient,
		);
		expect(apiClient.getDocument).toHaveBeenCalledWith("p1", "d1");
		// The tree-walk helpers are never consulted for a docId lookup.
		expect(apiClient.listDocuments).not.toHaveBeenCalled();
		expect(docClient.listFolders).not.toHaveBeenCalled();
		expect(result.content[0].text).toContain("mocked markdown");
		expect(result.content[0].text).toContain("README");
	});

	it("works for a docId that has no corresponding path (e.g. a doc from another folder tree)", async () => {
		const apiClient = makeApiClient({ documents: [] });
		const result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", docId: "not-in-tree" },
			apiClient,
			makeDocClient({ folders: [] }),
		);
		expect(apiClient.getDocument).toHaveBeenCalledWith("p1", "not-in-tree");
		expect(result.isError).toBeUndefined();
	});

	it("prefers docId over path when both are provided", async () => {
		const apiClient = makeApiClient();
		const _result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "Architecture/Overview", docId: "d1" },
			apiClient,
			makeDocClient(),
		);
		expect(apiClient.getDocument).toHaveBeenCalledWith("p1", "d1");
	});

	it("returns isError when neither path nor docId is provided", async () => {
		await expect(
			handleFilesystemDocTool(
				"read_doc",
				{ projectId: "p1" },
				makeApiClient(),
				makeDocClient(),
			),
		).rejects.toThrow();
	});
});

// ── write_doc ──────────────────────────────────────────────────────────────────

describe("handleFilesystemDocTool – write_doc", () => {
	it("creates a new document at root when it does not exist", async () => {
		const apiClient = makeApiClient({ documents: [DOC_IN_FOLDER] });
		const result = await handleFilesystemDocTool(
			"write_doc",
			{ projectId: "p1", path: "NewDoc", content: "# Hello" },
			apiClient,
			makeDocClient(),
		);
		expect(apiClient.createDocument).toHaveBeenCalledWith(
			expect.objectContaining({
				title: "NewDoc",
				content: "# Hello",
				folder_id: null,
			}),
		);
		expect(result.content[0].text).toContain("Created");
	});

	it("updates an existing document", async () => {
		const apiClient = makeApiClient();
		const result = await handleFilesystemDocTool(
			"write_doc",
			{ projectId: "p1", path: "README", content: "# Updated" },
			apiClient,
			makeDocClient(),
		);
		expect(apiClient.updateDocument).toHaveBeenCalledWith("p1", "d1", {
			content: "# Updated",
		});
		expect(result.content[0].text).toContain("Updated");
	});

	it("creates intermediate folders that don't exist", async () => {
		const docClient = makeDocClient({ folders: [] });
		const apiClient = makeApiClient({ documents: [] });
		const result = await handleFilesystemDocTool(
			"write_doc",
			{ projectId: "p1", path: "NewFolder/NewDoc", content: "# Content" },
			apiClient,
			docClient,
		);
		expect(docClient.createFolder).toHaveBeenCalledWith(
			"p1",
			expect.objectContaining({ name: "NewFolder" }),
		);
		expect(result.content[0].text).toContain("Created");
	});

	it("returns isError when path is empty", async () => {
		const result = await handleFilesystemDocTool(
			"write_doc",
			{ projectId: "p1", path: "/", content: "x" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.isError).toBe(true);
	});
});

// ── delete_doc ─────────────────────────────────────────────────────────────────

describe("handleFilesystemDocTool – delete_doc", () => {
	it("deletes a document at root", async () => {
		const apiClient = makeApiClient();
		const result = await handleFilesystemDocTool(
			"delete_doc",
			{ projectId: "p1", path: "README" },
			apiClient,
			makeDocClient(),
		);
		expect(apiClient.deleteDocument).toHaveBeenCalledWith("p1", "d1");
		expect(result.content[0].text).toContain("Deleted document");
	});

	it("deletes a folder", async () => {
		const docClient = makeDocClient();
		const result = await handleFilesystemDocTool(
			"delete_doc",
			{ projectId: "p1", path: "Architecture" },
			makeApiClient(),
			docClient,
		);
		expect(docClient.deleteFolder).toHaveBeenCalledWith("p1", "f1");
		expect(result.content[0].text).toContain("Deleted folder");
	});

	it("returns isError when path not found", async () => {
		const result = await handleFilesystemDocTool(
			"delete_doc",
			{ projectId: "p1", path: "Ghost" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("Not found");
	});
});

// ── move_doc ───────────────────────────────────────────────────────────────────

describe("handleFilesystemDocTool – move_doc", () => {
	it("moves a document to a new path", async () => {
		const apiClient = makeApiClient();
		const result = await handleFilesystemDocTool(
			"move_doc",
			{ projectId: "p1", sourcePath: "README", destPath: "Docs/README" },
			apiClient,
			makeDocClient(),
		);
		expect(apiClient.updateDocument).toHaveBeenCalledWith(
			"p1",
			"d1",
			expect.objectContaining({ title: "README" }),
		);
		expect(result.content[0].text).toContain("Moved document");
	});

	it("renames a document within same folder", async () => {
		const apiClient = makeApiClient();
		const result = await handleFilesystemDocTool(
			"move_doc",
			{ projectId: "p1", sourcePath: "README", destPath: "QUICKSTART" },
			apiClient,
			makeDocClient(),
		);
		expect(apiClient.updateDocument).toHaveBeenCalledWith(
			"p1",
			"d1",
			expect.objectContaining({ title: "QUICKSTART", folder_id: null }),
		);
		expect(result.content[0].text).toContain("Moved document");
	});

	it("moves a folder to a new path", async () => {
		const docClient = makeDocClient();
		const result = await handleFilesystemDocTool(
			"move_doc",
			{
				projectId: "p1",
				sourcePath: "Architecture",
				destPath: "Docs/Architecture",
			},
			makeApiClient(),
			docClient,
		);
		expect(docClient.updateFolder).toHaveBeenCalledWith(
			"p1",
			"f1",
			expect.objectContaining({ name: "Architecture" }),
		);
		expect(result.content[0].text).toContain("Moved folder");
	});

	it("returns isError when source path not found", async () => {
		const result = await handleFilesystemDocTool(
			"move_doc",
			{ projectId: "p1", sourcePath: "Ghost", destPath: "NewName" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("Not found");
	});

	it("returns isError when dest path is empty", async () => {
		const result = await handleFilesystemDocTool(
			"move_doc",
			{ projectId: "p1", sourcePath: "README", destPath: "/" },
			makeApiClient(),
			makeDocClient(),
		);
		expect(result.isError).toBe(true);
	});
});

// ── unknown tool ───────────────────────────────────────────────────────────────

describe("handleFilesystemDocTool – unknown tool", () => {
	it("throws for an unknown tool name", async () => {
		await expect(
			handleFilesystemDocTool(
				"unknown",
				{ projectId: "p1" },
				makeApiClient(),
				makeDocClient(),
			),
		).rejects.toThrow("Unknown filesystem doc tool");
	});
});

// ── Paging: one page per call, and a cursor to continue ───────────────────────

describe("handleFilesystemDocTool – paging", () => {
	const folders = Array.from({ length: 5 }, (_, i) => ({
		id: `pf${i}`,
		parent_id: null,
		name: `Folder ${i}`,
		position: i,
	}));
	const docs = Array.from({ length: 3 }, (_, i) => ({
		id: `pd${i}`,
		folder_id: null,
		title: `Doc ${i}`,
		position: i,
	}));

	it("lists one page of folders and hands back a cursor instead of reading on", async () => {
		const apiClient = makeApiClient({ documents: docs });
		const docClient = makeDocClient({ folders });
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", pageSize: 2 },
			apiClient,
			docClient,
		);
		const text = result.content[0].text;
		expect(text).toContain("Folder 0/");
		expect(text).toContain("Folder 1/");
		expect(text).not.toContain("Folder 2/");
		expect(text).toMatch(/call list_docs again with cursor="[^"]+"/);
		expect(docClient.listFolders).toHaveBeenCalledTimes(1);
		// Documents wait until the folders are done.
		expect(apiClient.listDocuments).not.toHaveBeenCalled();
	});

	it("follows the cursor through the folders, then into the documents", async () => {
		const apiClient = makeApiClient({ documents: docs });
		const docClient = makeDocClient({ folders });
		const seen: string[] = [];
		let cursor: string | undefined;
		for (let i = 0; i < 10; i++) {
			const result = await handleFilesystemDocTool(
				"list_docs",
				{ projectId: "p1", pageSize: 2, cursor },
				apiClient,
				docClient,
			);
			const text: string = result.content[0].text;
			seen.push(...(text.match(/(?:📁|📄) [^\n]+/g) ?? []));
			cursor = text.match(/cursor="([^"]+)"/)?.[1];
			if (!cursor) break;
		}
		expect(seen).toEqual([
			"📁 Folder 0/",
			"📁 Folder 1/",
			"📁 Folder 2/",
			"📁 Folder 3/",
			"📁 Folder 4/",
			"📄 Doc 0",
			"📄 Doc 1",
			"📄 Doc 2",
		]);
	});

	it("rejects a cursor that did not come from list_docs", async () => {
		await expect(
			handleFilesystemDocTool(
				"list_docs",
				{ projectId: "p1", cursor: "not-a-cursor" },
				makeApiClient(),
				makeDocClient(),
			),
		).rejects.toThrow(/cursor/i);
	});

	it("resolves a path with exact-name lookups, never listing a whole level", async () => {
		const apiClient = makeApiClient();
		const docClient = makeDocClient();
		await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "Architecture/API/Endpoints" },
			apiClient,
			docClient,
		);
		for (const [, o] of docClient.listFolders.mock.calls) {
			expect(o.name).toBeDefined();
			expect(o.pageSize).toBe(1);
		}
		for (const [, o] of apiClient.listDocuments.mock.calls) {
			expect(o.title).toBeDefined();
			expect(o.pageSize).toBe(1);
		}
	});
});

// ── by-path (a server with GET/PUT /docs/by-path) ──────────────────────────────

describe("handleFilesystemDocTool – by-path server", () => {
	const DOC = {
		id: "d7",
		title: "Endpoints",
		content: [{ type: "paragraph" }],
		updated_at: "2026-01-01T00:00:00Z",
	};

	it("read_doc resolves a nested path in one request", async () => {
		const apiClient = makeApiClient();
		const docClient = makeDocClient({
			byPath: {
				getByPath: vi
					.fn()
					.mockResolvedValue({ status: "found", doc: DOC, etag: '"h-m"' }),
			},
		});
		const result = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "Architecture/API/Endpoints" },
			apiClient,
			docClient,
		);
		expect(result.isError).toBeUndefined();
		expect(result.content[0].text).toContain("📄 Endpoints");
		expect(docClient.getByPath).toHaveBeenCalledWith(
			"p1",
			"Architecture/API/Endpoints",
		);
		expect(docClient.listFolders).not.toHaveBeenCalled();
		expect(apiClient.listDocuments).not.toHaveBeenCalled();
		expect(apiClient.getDocument).not.toHaveBeenCalled();
	});

	it("read_doc reports a missing document and a folder", async () => {
		const missing = makeDocClient({
			byPath: { getByPath: vi.fn().mockResolvedValue({ status: "not_found" }) },
		});
		const r1 = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "Nope" },
			makeApiClient(),
			missing,
		);
		expect(r1.isError).toBe(true);
		expect(r1.content[0].text).toContain("Not found");

		const folder = makeDocClient({
			byPath: { getByPath: vi.fn().mockResolvedValue({ status: "folder" }) },
		});
		const r2 = await handleFilesystemDocTool(
			"read_doc",
			{ projectId: "p1", path: "Architecture" },
			makeApiClient(),
			folder,
		);
		expect(r2.isError).toBe(true);
		expect(r2.content[0].text).toContain("is a folder");
		expect(folder.listFolders).not.toHaveBeenCalled();
	});

	it("write_doc creates, updates or leaves a document in one request", async () => {
		const putByPath = vi
			.fn()
			.mockResolvedValueOnce({
				status: "written",
				doc: DOC,
				created: true,
				changed: true,
			})
			.mockResolvedValueOnce({
				status: "written",
				doc: DOC,
				created: false,
				changed: true,
			})
			.mockResolvedValueOnce({
				status: "written",
				doc: DOC,
				created: false,
				changed: false,
			});
		const apiClient = makeApiClient();
		const docClient = makeDocClient({ byPath: { putByPath } });
		const args = { projectId: "p1", path: "New/Folder/Doc", content: "# Hi" };

		const texts: string[] = [];
		for (let i = 0; i < 3; i++) {
			const r = await handleFilesystemDocTool(
				"write_doc",
				args,
				apiClient,
				docClient,
			);
			texts.push(r.content[0].text);
		}
		expect(texts).toEqual([
			"Created document: New/Folder/Doc",
			"Updated document: New/Folder/Doc",
			"Unchanged document: New/Folder/Doc",
		]);
		expect(putByPath.mock.calls[0][1]).toEqual({
			path: "New/Folder/Doc",
			content: [
				{ type: "paragraph", content: [{ type: "text", text: "# Hi" }] },
			],
		});
		expect(docClient.createFolder).not.toHaveBeenCalled();
		expect(docClient.listFolders).not.toHaveBeenCalled();
		expect(apiClient.createDocument).not.toHaveBeenCalled();
		expect(apiClient.updateDocument).not.toHaveBeenCalled();
	});

	it("write_doc falls back to the folder walk on an older server", async () => {
		const apiClient = makeApiClient();
		const docClient = makeDocClient(); // putByPath answers unsupported
		const r = await handleFilesystemDocTool(
			"write_doc",
			{ projectId: "p1", path: "Architecture/Overview", content: "x" },
			apiClient,
			docClient,
		);
		expect(r.content[0].text).toBe("Updated document: Architecture/Overview");
		expect(docClient.putByPath).toHaveBeenCalledTimes(1);
		expect(apiClient.updateDocument).toHaveBeenCalledWith("p1", "d2", {
			content: "x",
		});
	});

	it("delete_doc deletes a document found by path, and walks for a folder", async () => {
		const apiClient = makeApiClient();
		const docClient = makeDocClient({
			byPath: {
				getByPath: vi.fn().mockResolvedValue({ status: "found", doc: DOC }),
			},
		});
		const r = await handleFilesystemDocTool(
			"delete_doc",
			{ projectId: "p1", path: "Architecture/API/Endpoints" },
			apiClient,
			docClient,
		);
		expect(r.content[0].text).toBe(
			"Deleted document: Architecture/API/Endpoints",
		);
		expect(apiClient.deleteDocument).toHaveBeenCalledWith("p1", "d7");
		expect(docClient.listFolders).not.toHaveBeenCalled();

		const folderClient = makeDocClient({
			byPath: { getByPath: vi.fn().mockResolvedValue({ status: "folder" }) },
		});
		const r2 = await handleFilesystemDocTool(
			"delete_doc",
			{ projectId: "p1", path: "Architecture" },
			makeApiClient(),
			folderClient,
		);
		expect(r2.content[0].text).toBe("Deleted folder: Architecture");
		expect(folderClient.deleteFolder).toHaveBeenCalledWith("p1", "f1");
	});

	it("move_doc finds the source by path and keeps creating the destination folders", async () => {
		const apiClient = makeApiClient();
		const docClient = makeDocClient({
			byPath: {
				getByPath: vi.fn().mockResolvedValue({ status: "found", doc: DOC }),
			},
		});
		const r = await handleFilesystemDocTool(
			"move_doc",
			{
				projectId: "p1",
				sourcePath: "Architecture/API/Endpoints",
				destPath: "Elsewhere/Renamed",
			},
			apiClient,
			docClient,
		);
		expect(r.content[0].text).toBe(
			"Moved document: Architecture/API/Endpoints → Elsewhere/Renamed",
		);
		expect(docClient.createFolder).toHaveBeenCalledWith("p1", {
			name: "Elsewhere",
			parent_id: undefined,
		});
		expect(apiClient.updateDocument).toHaveBeenCalledWith("p1", "d7", {
			title: "Renamed",
			folder_id: "fnew",
		});
	});
});
