import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../utils/index.js", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../../utils/index.js")>();
	return {
		...actual,
		markdownToBlocknote: vi.fn((md: string) => [
			{ type: "paragraph", content: [{ type: "text", text: md }] },
		]),
	};
});

import { PacaAPITaskExtendedClient } from "../../api/task-extended-client.js";

const CONFIG = { baseURL: "https://api.example.com", apiKey: "key123" };
const CONFIG_WITH_AGENT = { ...CONFIG, agentId: "agent-1" };

function okEnvelope(data: any) {
	return {
		ok: true,
		json: async () => ({ success: true, data }),
		text: async () => "",
	};
}

function rawOk(data: any) {
	return { ok: true, json: async () => data, text: async () => "" };
}

function errorResponse(status = 400, body = "Bad Request") {
	return {
		ok: false,
		status,
		statusText: body,
		text: async () => body,
		json: async () => ({}),
	};
}

function noContentResponse() {
	return {
		ok: true,
		status: 204,
		text: async () => "",
		json: async () => {
			throw new SyntaxError("Unexpected end of JSON input");
		},
	};
}

describe("PacaAPITaskExtendedClient", () => {
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		fetchMock = vi.fn().mockResolvedValue(okEnvelope([]));
		vi.stubGlobal("fetch", fetchMock);
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	// ---------------------------------------------------------------------------
	// Request behaviour
	// ---------------------------------------------------------------------------

	it("includes X-API-Key header", async () => {
		const client = new PacaAPITaskExtendedClient(CONFIG);
		await client.listTaskActivities("p1", "t1");
		expect(fetchMock.mock.calls[0][1].headers["X-API-Key"]).toBe("key123");
	});

	it("includes X-Agent-ID when agentId is set", async () => {
		const client = new PacaAPITaskExtendedClient(CONFIG_WITH_AGENT);
		await client.listTaskActivities("p1", "t1");
		expect(fetchMock.mock.calls[0][1].headers["X-Agent-ID"]).toBe("agent-1");
	});

	it("throws on non-OK response", async () => {
		fetchMock.mockResolvedValue(errorResponse(403, "Forbidden"));
		const client = new PacaAPITaskExtendedClient(CONFIG);
		await expect(client.listTaskActivities("p1", "t1")).rejects.toThrow("403");
	});

	it("returns raw JSON when not a SuccessEnvelope", async () => {
		fetchMock.mockResolvedValue(rawOk([{ id: "act1" }]));
		const client = new PacaAPITaskExtendedClient(CONFIG);
		const result = await client.listTaskActivities("p1", "t1");
		expect(result).toEqual([{ id: "act1" }]);
	});

	it("resolves on 204 No Content without parsing JSON", async () => {
		fetchMock.mockResolvedValue(noContentResponse());
		const client = new PacaAPITaskExtendedClient(CONFIG);
		await expect(
			client.deleteTaskComment("p1", "t1", "c1"),
		).resolves.toBeUndefined();
	});

	// ---------------------------------------------------------------------------
	// Task Activities
	// ---------------------------------------------------------------------------

	describe("listTaskActivities", () => {
		it("calls GET /api/v1/projects/:id/tasks/:taskId/activities", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "act1" }]));
			const result = await client.listTaskActivities("p1", "t1");
			expect(fetchMock.mock.calls[0][0]).toContain("/tasks/t1/activities");
			expect(result).toEqual([{ id: "act1" }]);
		});

		it("extracts .items from object response", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ items: [{ id: "act1" }] }));
			const result = await client.listTaskActivities("p1", "t1");
			expect(result).toEqual([{ id: "act1" }]);
		});

		it("extracts .activities from object response", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ activities: [{ id: "act1" }] }));
			const result = await client.listTaskActivities("p1", "t1");
			expect(result).toEqual([{ id: "act1" }]);
		});
	});

	// ---------------------------------------------------------------------------
	// Task Comments
	// ---------------------------------------------------------------------------

	describe("addTaskComment", () => {
		it("calls POST /api/v1/projects/:id/tasks/:taskId/activities/comments", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "c1" }));
			await client.addTaskComment("p1", "t1", { content: "Hello" });
			expect(fetchMock.mock.calls[0][0]).toContain("/activities/comments");
			expect(fetchMock.mock.calls[0][1].method).toBe("POST");
		});

		it("converts markdown content to blocknote blocks", async () => {
			const { markdownToBlocknote } = await import("../../utils/index.js");
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "c1" }));
			await client.addTaskComment("p1", "t1", { content: "# Heading" });
			expect(markdownToBlocknote).toHaveBeenCalledWith("# Heading");
		});

		it("sends null content when no content provided", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "c1" }));
			await client.addTaskComment("p1", "t1", {});
			const body = JSON.parse(fetchMock.mock.calls[0][1].body);
			expect(body.content).toBeNull();
		});
	});

	describe("updateTaskComment", () => {
		it("calls PATCH /api/v1/projects/:id/tasks/:taskId/activities/comments/:commentId", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "c1" }));
			await client.updateTaskComment("p1", "t1", "c1", { content: "Updated" });
			expect(fetchMock.mock.calls[0][0]).toContain("/activities/comments/c1");
			expect(fetchMock.mock.calls[0][1].method).toBe("PATCH");
		});

		it("converts markdown content to blocknote blocks on update", async () => {
			const { markdownToBlocknote } = await import("../../utils/index.js");
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "c1" }));
			await client.updateTaskComment("p1", "t1", "c1", {
				content: "Updated body",
			});
			expect(markdownToBlocknote).toHaveBeenCalledWith("Updated body");
		});
	});

	describe("deleteTaskComment", () => {
		it("calls DELETE /api/v1/projects/:id/tasks/:taskId/activities/comments/:commentId", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.deleteTaskComment("p1", "t1", "c1");
			expect(fetchMock.mock.calls[0][0]).toContain("/activities/comments/c1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});

	// ---------------------------------------------------------------------------
	// Subtasks
	// ---------------------------------------------------------------------------

	describe("listSubtasks", () => {
		it("calls GET /api/v1/projects/:id/tasks?parent_task_id=:parentId", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "sub1" }]));
			const result = await client.listSubtasks("p1", "parent-1");
			expect(fetchMock.mock.calls[0][0]).toContain("parent_task_id=parent-1");
			expect(result).toEqual([{ id: "sub1" }]);
		});

		it("extracts .items from object response", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ items: [{ id: "sub1" }] }));
			const result = await client.listSubtasks("p1", "parent-1");
			expect(result).toEqual([{ id: "sub1" }]);
		});
	});

	// ---------------------------------------------------------------------------
	// Task Statuses (via task-extended-client)
	// ---------------------------------------------------------------------------

	describe("listTaskStatuses", () => {
		it("calls GET /api/v1/projects/:id/task-statuses", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "st1" }]));
			const result = await client.listTaskStatuses("p1");
			expect(fetchMock.mock.calls[0][0]).toContain("/task-statuses");
			expect(result).toEqual([{ id: "st1" }]);
		});

		it("extracts .statuses from object response", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ statuses: [{ id: "st1" }] }));
			const result = await client.listTaskStatuses("p1");
			expect(result).toEqual([{ id: "st1" }]);
		});
	});

	// ---------------------------------------------------------------------------
	// Task Types (via task-extended-client)
	// ---------------------------------------------------------------------------

	describe("listTaskTypes", () => {
		it("calls GET /api/v1/projects/:id/task-types", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "ty1" }]));
			const result = await client.listTaskTypes("p1");
			expect(fetchMock.mock.calls[0][0]).toContain("/task-types");
			expect(result).toEqual([{ id: "ty1" }]);
		});

		it("extracts .types from object response", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ types: [{ id: "ty1" }] }));
			const result = await client.listTaskTypes("p1");
			expect(result).toEqual([{ id: "ty1" }]);
		});
	});

	// ---------------------------------------------------------------------------
	// Project Members (via task-extended-client)
	// ---------------------------------------------------------------------------

	describe("listProjectMembers", () => {
		it("calls GET /api/v1/projects/:id/members", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "m1" }]));
			const result = await client.listProjectMembers("p1");
			expect(fetchMock.mock.calls[0][0]).toContain("/members");
			expect(result).toEqual([{ id: "m1" }]);
		});

		it("extracts .members from object response", async () => {
			const client = new PacaAPITaskExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ members: [{ id: "m1" }] }));
			const result = await client.listProjectMembers("p1");
			expect(result).toEqual([{ id: "m1" }]);
		});
	});
});
