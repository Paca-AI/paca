import { describe, expect, it, vi } from "vitest";

vi.mock("../../utils/index.js", () => ({
	blocknoteToMarkdown: vi.fn(() => "mocked markdown"),
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

/** Create mock API client. Builds tree from folders + docs arrays. */
function makeApiClient(
	opts: { folders?: any[]; documents?: any[]; getDocumentContent?: any } = {},
) {
	const _folders = opts.folders ?? [FOLDER_ROOT, FOLDER_NESTED];
	const documents = opts.documents ?? [DOC_ROOT, DOC_IN_FOLDER, DOC_IN_NESTED];

	return {
		listDocuments: vi.fn().mockResolvedValue(documents),
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

function makeDocClient(opts: { folders?: any[] } = {}) {
	const folders = opts.folders ?? [FOLDER_ROOT, FOLDER_NESTED];

	return {
		listFolders: vi.fn().mockResolvedValue(folders),
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
