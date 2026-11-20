// Notes of a note view through the document tools: list_docs and write_doc
// take a viewId, and delete_doc a docId (notes have no path).
import { describe, expect, it, vi } from "vitest";
import { handleFilesystemDocTool } from "../../tools/filesystem-doc-tools.js";

const NOTE = { id: "n1", view_id: "v1", title: "Sprint goals", position: 0 };

function clients(notes: any[] = [NOTE]) {
	const apiClient = {
		listDocuments: vi.fn(async (_p: string, o: any = {}) => {
			const items = notes.filter(
				(n) =>
					n.view_id === o.viewId &&
					(!o.title || n.title.toLowerCase() === o.title.toLowerCase()),
			);
			return { items, nextCursor: null };
		}),
		createDocument: vi.fn(async (input: any) => ({
			id: "n9",
			title: input.title,
			view_id: input.view_id,
		})),
		deleteDocument: vi.fn(async () => undefined),
	} as any;
	const docClient = {
		patchDocument: vi.fn(async () => ({ doc: NOTE })),
		putByPath: vi.fn(),
	} as any;
	return { apiClient, docClient };
}

function text(result: any): string {
	return result.content[0].text;
}

describe("list_docs with viewId", () => {
	it("lists the notes of the view with their docId", async () => {
		const { apiClient, docClient } = clients();
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", viewId: "v1" },
			apiClient,
			docClient,
		);
		expect(apiClient.listDocuments).toHaveBeenCalledWith("p1", {
			viewId: "v1",
			cursor: undefined,
			pageSize: undefined,
		});
		expect(text(result)).toBe(
			"📝 Notes of view v1\n📄 Sprint goals (docId: n1)",
		);
	});

	it("hands back a cursor when more notes remain", async () => {
		const { apiClient, docClient } = clients();
		apiClient.listDocuments.mockResolvedValueOnce({
			items: [NOTE],
			nextCursor: "c2",
		});
		const first = text(
			await handleFilesystemDocTool(
				"list_docs",
				{ projectId: "p1", viewId: "v1", pageSize: 1 },
				apiClient,
				docClient,
			),
		);
		const cursor = first.match(/cursor="([^"]+)"/)?.[1];
		expect(cursor).toBeTruthy();

		await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", viewId: "v1", cursor },
			apiClient,
			docClient,
		);
		expect(apiClient.listDocuments).toHaveBeenLastCalledWith("p1", {
			viewId: "v1",
			cursor: "c2",
			pageSize: undefined,
		});
	});

	it("refuses a path together with viewId", async () => {
		const { apiClient, docClient } = clients();
		const result = await handleFilesystemDocTool(
			"list_docs",
			{ projectId: "p1", viewId: "v1", path: "Guides" },
			apiClient,
			docClient,
		);
		expect(result.isError).toBe(true);
		expect(apiClient.listDocuments).not.toHaveBeenCalled();
	});
});

describe("write_doc with viewId", () => {
	it("creates the document when the view has none", async () => {
		const { apiClient, docClient } = clients([]);
		const result = await handleFilesystemDocTool(
			"write_doc",
			{
				projectId: "p1",
				viewId: "v1",
				path: " Retro ",
				content: "# Went well",
				note: "after the demo",
			},
			apiClient,
			docClient,
		);
		expect(apiClient.createDocument).toHaveBeenCalledWith({
			project_id: "p1",
			title: "Retro",
			content: "# Went well",
			view_id: "v1",
			note: "after the demo",
		});
		expect(docClient.putByPath).not.toHaveBeenCalled();
		expect(text(result)).toBe("Created note: Retro (docId: n9)");
	});

	it("updates the view's document", async () => {
		const { apiClient, docClient } = clients();
		const result = await handleFilesystemDocTool(
			"write_doc",
			{
				projectId: "p1",
				viewId: "v1",
				path: "sprint goals",
				content: "Ship notes",
			},
			apiClient,
			docClient,
		);
		expect(docClient.patchDocument).toHaveBeenCalledWith("p1", "n1", {
			content: expect.any(Array),
		});
		expect(apiClient.createDocument).not.toHaveBeenCalled();
		expect(text(result)).toBe("Updated note: Sprint goals (docId: n1)");
	});

	it("updates the view's document whatever title is given", async () => {
		const { apiClient, docClient } = clients();
		const result = await handleFilesystemDocTool(
			"write_doc",
			{ projectId: "p1", viewId: "v1", path: "Another title", content: "x" },
			apiClient,
			docClient,
		);
		expect(docClient.patchDocument).toHaveBeenCalledWith("p1", "n1", {
			content: expect.any(Array),
		});
		expect(apiClient.createDocument).not.toHaveBeenCalled();
		expect(text(result)).toBe("Updated note: Sprint goals (docId: n1)");
	});

	it("refuses a title with a slash", async () => {
		const { apiClient, docClient } = clients();
		const result = await handleFilesystemDocTool(
			"write_doc",
			{ projectId: "p1", viewId: "v1", path: "Q1/Q2", content: "x" },
			apiClient,
			docClient,
		);
		expect(result.isError).toBe(true);
		expect(apiClient.listDocuments).not.toHaveBeenCalled();
	});
});

describe("delete_doc with docId", () => {
	it("deletes the document by id", async () => {
		const { apiClient, docClient } = clients();
		const result = await handleFilesystemDocTool(
			"delete_doc",
			{ projectId: "p1", docId: "n1" },
			apiClient,
			docClient,
		);
		expect(apiClient.deleteDocument).toHaveBeenCalledWith("p1", "n1");
		expect(text(result)).toBe("Deleted document: n1");
	});

	it("needs exactly one of path and docId", async () => {
		const { apiClient, docClient } = clients();
		for (const args of [
			{ projectId: "p1" },
			{ projectId: "p1", path: "README", docId: "n1" },
		]) {
			await expect(
				handleFilesystemDocTool("delete_doc", args, apiClient, docClient),
			).rejects.toThrow("Provide exactly one of path or docId");
		}
		expect(apiClient.deleteDocument).not.toHaveBeenCalled();
	});
});
