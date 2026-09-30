import { describe, expect, it, vi } from "vitest";
import {
	buildSnippet,
	extractPlainText,
	getSearchTools,
	handleSearchTool,
} from "../../tools/search-tools.js";

const FOLDERS = [
	{ id: "f1", parent_id: null, name: "Architecture", position: 0 },
	{ id: "f2", parent_id: "f1", name: "API", position: 0 },
];

function block(text: string) {
	return {
		id: "b",
		type: "paragraph",
		content: [{ type: "text", text }],
		children: [],
	};
}

function makeDocClient(hits: any[]) {
	return {
		searchDocuments: vi.fn().mockResolvedValue(hits),
		listFolders: vi.fn().mockResolvedValue(FOLDERS),
	} as any;
}

describe("getSearchTools", () => {
	it("exposes search_docs and search_tasks requiring projectId + query", () => {
		const tools = getSearchTools();
		expect(tools.map((t) => t.name)).toEqual(["search_docs", "search_tasks"]);
		for (const t of tools) {
			expect(t.inputSchema.required).toEqual(["projectId", "query"]);
		}
	});
});

describe("extractPlainText / buildSnippet", () => {
	it("collects text nodes (incl. children) and ignores structural keys", () => {
		const blocks = [
			{ ...block("Hello"), children: [block("nested")] },
			block("world"),
		];
		const text = extractPlainText(blocks);
		expect(text).toContain("Hello");
		expect(text).toContain("nested");
		expect(text).not.toContain("paragraph");
		expect(extractPlainText(null)).toBe("");
	});

	it("builds a trimmed, ellipsised, case-insensitive excerpt", () => {
		const text = `${"a ".repeat(100)}Needle here ${"b ".repeat(100)}`;
		const s = buildSnippet(text, "needle");
		expect(s).toContain("Needle here");
		expect(s.startsWith("…")).toBe(true);
		expect(s.endsWith("…")).toBe(true);
		expect(buildSnippet("nothing", "zzz")).toBe("");
	});
});

describe("search_docs", () => {
	it("renders folder paths, match location and snippets", async () => {
		const docClient = makeDocClient([
			{
				id: "d1",
				folder_id: "f2",
				title: "Endpoints",
				matched_in: "content",
				snippet: "…call the /login endpoint…",
			},
			{ id: "d2", folder_id: null, title: "Login", matched_in: "title" },
		]);
		const result = await handleSearchTool(
			"search_docs",
			{ projectId: "p1", query: "login" },
			{} as any,
			docClient,
		);
		const out = result.content[0].text;
		expect(docClient.searchDocuments).toHaveBeenCalledWith("p1", "login", 20);
		expect(out).toContain(
			"Architecture/API/Endpoints  (content match, id: d1)",
		);
		expect(out).toContain("…call the /login endpoint…");
		expect(out).toContain("📄 Login  (title match, id: d2)");
	});

	it("reports no matches helpfully", async () => {
		const result = await handleSearchTool(
			"search_docs",
			{ projectId: "p1", query: "zzz" },
			{} as any,
			makeDocClient([]),
		);
		expect(result.content[0].text).toContain('No documents matched "zzz"');
	});

	it("rejects a blank query", async () => {
		await expect(
			handleSearchTool(
				"search_docs",
				{ projectId: "p1", query: "  " },
				{} as any,
				makeDocClient([]),
			),
		).rejects.toThrow();
	});
});

describe("search_tasks", () => {
	it("searches content, snippets the description and shows the next cursor", async () => {
		const apiClient = {
			listTasks: vi.fn().mockResolvedValue({
				items: [
					{
						id: "t1",
						task_number: 7,
						title: "Fix auth",
						status_id: "s1",
						description: [block("Users see a login timeout after 5 minutes")],
					},
					{ id: "t2", task_number: 8, title: "Login page", description: null },
				],
				nextCursor: "abc",
			}),
		} as any;
		const result = await handleSearchTool(
			"search_tasks",
			{ projectId: "p1", query: "login", statusId: "s1" },
			apiClient,
			{} as any,
		);
		expect(apiClient.listTasks).toHaveBeenCalledWith("p1", {
			search: "login",
			searchContent: true,
			statusId: "s1",
			sprintId: undefined,
			cursor: undefined,
			pageSize: 20,
		});
		const out = result.content[0].text;
		expect(out).toContain("#7 Fix auth  (id: t1, status: s1)");
		expect(out).toContain("login timeout");
		expect(out).toContain("#8 Login page");
		expect(out).toContain('cursor="abc"');
	});

	it("reports no matches", async () => {
		const apiClient = {
			listTasks: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
		} as any;
		const result = await handleSearchTool(
			"search_tasks",
			{ projectId: "p1", query: "zzz" },
			apiClient,
			{} as any,
		);
		expect(result.content[0].text).toContain('No tasks matched "zzz"');
	});
});
