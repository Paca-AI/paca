import { describe, expect, it, vi } from "vitest";

vi.mock("../../utils/index.js", () => ({
	blocknoteToMarkdown: vi.fn(() => "comment markdown"),
	formatToolError: vi.fn((error: unknown) =>
		error instanceof Error ? error.message : String(error),
	),
}));

import {
	getDocActivityTools,
	handleDocActivityTool,
} from "../../tools/doc-activity-tools.js";

const activity = {
	id: "act-1",
	activity_type: "system_event",
	actor_id: "user-1",
	actor_name: "Alice",
	actor_username: "alice",
	content: { field: "status", old: "todo", new: "done" },
	created_at: "2024-01-01T00:00:00Z",
};

const comment = {
	id: "cmt-1",
	activity_type: "comment",
	actor_id: "user-1",
	actor_name: "Bob",
	actor_username: "bob",
	content: null,
	created_at: "2024-01-01T00:00:00Z",
	updated_at: "2024-01-02T00:00:00Z",
};

function makeClient(overrides: Record<string, any> = {}) {
	return {
		listDocumentActivities: vi.fn().mockResolvedValue([activity]),
		addDocumentComment: vi.fn().mockResolvedValue(comment),
		updateDocumentComment: vi
			.fn()
			.mockResolvedValue({ ...comment, content: "updated" }),
		deleteDocumentComment: vi.fn().mockResolvedValue(undefined),
		...overrides,
	} as any;
}

// ---------------------------------------------------------------------------
// getDocActivityTools
// ---------------------------------------------------------------------------

describe("getDocActivityTools", () => {
	it("returns 4 tools", () => {
		expect(getDocActivityTools()).toHaveLength(4);
	});

	it("includes all expected tool names", () => {
		const names = getDocActivityTools().map((t) => t.name);
		expect(names).toContain("list_doc_activities");
		expect(names).toContain("add_doc_comment");
		expect(names).toContain("update_doc_comment");
		expect(names).toContain("delete_doc_comment");
	});

	it("warns in add_doc_comment's description that mentions don't notify", () => {
		const tool = getDocActivityTools().find(
			(t) => t.name === "add_doc_comment",
		);
		expect(tool?.description).toContain("does not send a notification");
	});
});

// ---------------------------------------------------------------------------
// list_doc_activities
// ---------------------------------------------------------------------------

describe("handleDocActivityTool – list_doc_activities", () => {
	it("calls client.listDocumentActivities with projectId and docId", async () => {
		const client = makeClient();
		await handleDocActivityTool(
			"list_doc_activities",
			{ projectId: "p1", docId: "d1" },
			client,
		);
		expect(client.listDocumentActivities).toHaveBeenCalledWith("p1", "d1");
	});

	it("includes 'Document Activities:' header, activity type, and username", async () => {
		const result = await handleDocActivityTool(
			"list_doc_activities",
			{ projectId: "p1", docId: "d1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Document Activities:");
		expect(result.content[0].text).toContain("system_event");
		expect(result.content[0].text).toContain("@alice");
	});

	it("returns an isError result when required args are missing, instead of rejecting", async () => {
		const result = await handleDocActivityTool(
			"list_doc_activities",
			{ projectId: "p1" },
			makeClient(),
		);
		expect(result.isError).toBe(true);
	});
});

// ---------------------------------------------------------------------------
// add_doc_comment
// ---------------------------------------------------------------------------

describe("handleDocActivityTool – add_doc_comment", () => {
	it("calls client.addDocumentComment with projectId, docId, and content", async () => {
		const client = makeClient();
		await handleDocActivityTool(
			"add_doc_comment",
			{ projectId: "p1", docId: "d1", content: "LGTM" },
			client,
		);
		expect(client.addDocumentComment).toHaveBeenCalledWith("p1", "d1", "LGTM");
	});

	it("includes 'added successfully' and the commenter's username in the response", async () => {
		const result = await handleDocActivityTool(
			"add_doc_comment",
			{ projectId: "p1", docId: "d1", content: "LGTM" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("added successfully");
		expect(result.content[0].text).toContain("@bob");
	});

	it("returns an isError result when content is missing, instead of rejecting", async () => {
		const result = await handleDocActivityTool(
			"add_doc_comment",
			{ projectId: "p1", docId: "d1" },
			makeClient(),
		);
		expect(result.isError).toBe(true);
	});
});

// ---------------------------------------------------------------------------
// update_doc_comment
// ---------------------------------------------------------------------------

describe("handleDocActivityTool – update_doc_comment", () => {
	it("calls client.updateDocumentComment with all IDs and content", async () => {
		const client = makeClient();
		await handleDocActivityTool(
			"update_doc_comment",
			{ projectId: "p1", docId: "d1", commentId: "cmt-1", content: "edited" },
			client,
		);
		expect(client.updateDocumentComment).toHaveBeenCalledWith(
			"p1",
			"d1",
			"cmt-1",
			"edited",
		);
	});

	it("includes 'updated successfully' in the response", async () => {
		const result = await handleDocActivityTool(
			"update_doc_comment",
			{ projectId: "p1", docId: "d1", commentId: "cmt-1", content: "edited" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("updated successfully");
	});
});

// ---------------------------------------------------------------------------
// delete_doc_comment
// ---------------------------------------------------------------------------

describe("handleDocActivityTool – delete_doc_comment", () => {
	it("calls client.deleteDocumentComment with projectId, docId, and commentId", async () => {
		const client = makeClient();
		await handleDocActivityTool(
			"delete_doc_comment",
			{ projectId: "p1", docId: "d1", commentId: "cmt-1" },
			client,
		);
		expect(client.deleteDocumentComment).toHaveBeenCalledWith(
			"p1",
			"d1",
			"cmt-1",
		);
	});

	it("includes 'deleted successfully' and commentId in the response", async () => {
		const result = await handleDocActivityTool(
			"delete_doc_comment",
			{ projectId: "p1", docId: "d1", commentId: "cmt-1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("cmt-1");
		expect(result.content[0].text).toContain("deleted successfully");
	});
});

// ---------------------------------------------------------------------------
// unknown tool / client errors
// ---------------------------------------------------------------------------

describe("handleDocActivityTool – error handling", () => {
	it("returns an isError result for an unknown tool name instead of rejecting", async () => {
		// handleDocActivityTool catches internally rather than relying on
		// handleToolCall's outer try/catch, which only sees synchronous
		// throws — a bare `return handleDocActivityTool(...)` there does not
		// catch this function's own async rejections. Every error path here
		// must resolve to a normal tool result, never reject.
		const result = await handleDocActivityTool(
			"not_a_real_tool",
			{},
			makeClient(),
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("Unknown document activity tool");
	});

	it("returns an isError result when the client rejects", async () => {
		const client = makeClient({
			addDocumentComment: vi.fn().mockRejectedValue(new Error("API down")),
		});
		const result = await handleDocActivityTool(
			"add_doc_comment",
			{ projectId: "p1", docId: "d1", content: "hi" },
			client,
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("API down");
	});
});
