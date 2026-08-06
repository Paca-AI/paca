import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mock markdownToBlocknote so tests don't need a live BlockNote editor, while
// keeping the real formatApiRequestError (used by the error-handling tests
// below). client.ts imports from "../utils/index.js" (relative to src/api/),
// which resolves to the same absolute path as "../../utils/index.js" here.
vi.mock("../../utils/index.js", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../../utils/index.js")>();
	return {
		...actual,
		markdownToBlocknote: vi.fn((md: string) => [
			{ type: "paragraph", text: md },
		]),
		blocknoteToMarkdown: vi.fn(() => ""),
	};
});

import { PacaAPIClient } from "../../api/client.js";
import type { PacaConfig } from "../../types/index.js";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeClient(overrides: Partial<PacaConfig> = {}): PacaAPIClient {
	return new PacaAPIClient({
		apiKey: "test-key",
		baseURL: "http://api.test",
		...overrides,
	});
}

function mockFetchOk(data: unknown) {
	return vi.fn().mockResolvedValueOnce({
		ok: true,
		json: async () => ({ success: true, data }),
		text: async () => JSON.stringify({ success: true, data }),
	} as unknown as Response);
}

function mockFetchError(status = 400, body = "Bad Request") {
	return vi.fn().mockResolvedValueOnce({
		ok: false,
		status,
		statusText: "Error",
		text: async () => body,
	} as unknown as Response);
}

function mockFetchNoContent() {
	return vi.fn().mockResolvedValueOnce({
		ok: true,
		status: 204,
		text: async () => "",
		json: async () => {
			throw new SyntaxError("Unexpected end of JSON input");
		},
	} as unknown as Response);
}

// ---------------------------------------------------------------------------
// Setup / teardown
// ---------------------------------------------------------------------------

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
	fetchMock = mockFetchOk(null);
	vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
	vi.unstubAllGlobals();
});

// ---------------------------------------------------------------------------
// Request construction
// ---------------------------------------------------------------------------

describe("PacaAPIClient – request headers", () => {
	it("sends X-API-Key header on every request", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		const client = makeClient({ apiKey: "my-secret-key" });
		await client.listProjects();
		const [, options] = (fetch as any).mock.calls[0] as [string, RequestInit];
		expect((options.headers as Record<string, string>)["X-API-Key"]).toBe(
			"my-secret-key",
		);
	});

	it("sends X-Agent-ID header when agentId is configured", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		const client = makeClient({ agentId: "agent-abc" });
		await client.listProjects();
		const [, options] = (fetch as any).mock.calls[0] as [string, RequestInit];
		expect((options.headers as Record<string, string>)["X-Agent-ID"]).toBe(
			"agent-abc",
		);
	});

	it("omits X-Agent-ID when agentId is not configured", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		const client = makeClient();
		await client.listProjects();
		const [, options] = (fetch as any).mock.calls[0] as [string, RequestInit];
		expect(
			(options.headers as Record<string, string>)["X-Agent-ID"],
		).toBeUndefined();
	});

	it("sends X-Actor-User-ID header when actorUserId is configured", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		const client = makeClient({ agentId: "agent-abc", actorUserId: "user-7" });
		await client.listProjects();
		const [, options] = (fetch as any).mock.calls[0] as [string, RequestInit];
		expect((options.headers as Record<string, string>)["X-Actor-User-ID"]).toBe(
			"user-7",
		);
	});

	it("omits X-Actor-User-ID when actorUserId is not configured", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		const client = makeClient({ agentId: "agent-abc" });
		await client.listProjects();
		const [, options] = (fetch as any).mock.calls[0] as [string, RequestInit];
		expect(
			(options.headers as Record<string, string>)["X-Actor-User-ID"],
		).toBeUndefined();
	});
});

// ---------------------------------------------------------------------------
// SuccessEnvelope unwrapping
// ---------------------------------------------------------------------------

describe("PacaAPIClient – SuccessEnvelope unwrapping", () => {
	it("extracts .data from a SuccessEnvelope response", async () => {
		const project = { id: "proj-1", name: "Test Project" };
		vi.stubGlobal("fetch", mockFetchOk(project));
		const client = makeClient();
		const result = await client.getProject("proj-1");
		expect(result).toEqual(project);
	});

	it("falls back to the raw response when SuccessEnvelope is absent", async () => {
		const raw = [{ id: "p1" }, { id: "p2" }];
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValueOnce({
				ok: true,
				json: async () => raw,
			} as unknown as Response),
		);
		const client = makeClient();
		const result = await client.listProjects();
		expect(result).toEqual(raw);
	});
});

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

describe("PacaAPIClient – error handling", () => {
	it("throws when the server returns a non-ok status", async () => {
		vi.stubGlobal("fetch", mockFetchError(404, "Not Found"));
		const client = makeClient();
		await expect(client.getProject("missing")).rejects.toThrow("404");
	});

	it("error message includes status text and body", async () => {
		vi.stubGlobal("fetch", mockFetchError(500, "Internal Server Error"));
		const client = makeClient();
		await expect(client.getProject("x")).rejects.toThrow(
			"Internal Server Error",
		);
	});

	it("resolves on 204 No Content without parsing JSON", async () => {
		vi.stubGlobal("fetch", mockFetchNoContent());
		const client = makeClient();
		await expect(client.deleteProject("proj-1")).resolves.toBeUndefined();
	});
});

// ---------------------------------------------------------------------------
// Project methods
// ---------------------------------------------------------------------------

describe("PacaAPIClient – listProjects", () => {
	it("calls the correct endpoint", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		await makeClient().listProjects();
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects?page=1&page_size=50",
		);
	});

	it("accepts custom page and pageSize", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		await makeClient().listProjects(2, 10);
		expect((fetch as any).mock.calls[0][0]).toContain("page=2&page_size=10");
	});

	it("returns an empty array when response.items is missing", async () => {
		vi.stubGlobal("fetch", mockFetchOk({}));
		const result = await makeClient().listProjects();
		expect(result).toEqual([]);
	});

	it("returns items when response has .items", async () => {
		const items = [{ id: "p1" }];
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValueOnce({
				ok: true,
				json: async () => ({ success: true, data: { items } }),
			} as unknown as Response),
		);
		const result = await makeClient().listProjects();
		expect(result).toEqual(items);
	});
});

describe("PacaAPIClient – getProject", () => {
	it("calls GET /api/v1/projects/:id", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "proj-1" }));
		await makeClient().getProject("proj-1");
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects/proj-1",
		);
		expect((fetch as any).mock.calls[0][1].method).toBe("GET");
	});
});

describe("PacaAPIClient – createProject", () => {
	it("calls POST /api/v1/projects with the input body", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "new-proj" }));
		await makeClient().createProject({
			name: "New Project",
			description: "desc",
		});
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects");
		expect(options.method).toBe("POST");
		expect(JSON.parse(options.body as string)).toMatchObject({
			name: "New Project",
		});
	});
});

describe("PacaAPIClient – updateProject", () => {
	it("calls PATCH /api/v1/projects/:id", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "proj-1" }));
		await makeClient().updateProject("proj-1", { name: "Renamed" });
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1");
		expect(options.method).toBe("PATCH");
	});
});

describe("PacaAPIClient – deleteProject", () => {
	it("calls DELETE /api/v1/projects/:id", async () => {
		vi.stubGlobal("fetch", mockFetchOk(null));
		await makeClient().deleteProject("proj-1");
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1");
		expect(options.method).toBe("DELETE");
	});
});

// ---------------------------------------------------------------------------
// Task methods
// ---------------------------------------------------------------------------

describe("PacaAPIClient – listTasks", () => {
	it("calls the correct endpoint without filters", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ items: [] }));
		await makeClient().listTasks("proj-1");
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects/proj-1/tasks",
		);
	});

	it("appends query params for filters", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ items: [] }));
		await makeClient().listTasks("proj-1", {
			sprintId: "sprint-1",
			statusId: "done",
			pageSize: 20,
		});
		const url = (fetch as any).mock.calls[0][0] as string;
		expect(url).toContain("sprint_id=sprint-1");
		expect(url).toContain("status_id=done");
		expect(url).toContain("page_size=20");
	});

	it("returns nextCursor from response", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValueOnce({
				ok: true,
				json: async () => ({
					success: true,
					data: { items: [], next_cursor: "cursor-abc" },
				}),
			} as unknown as Response),
		);
		const result = await makeClient().listTasks("proj-1");
		expect(result.nextCursor).toBe("cursor-abc");
	});
});

describe("PacaAPIClient – getTask", () => {
	it("calls GET /api/v1/projects/:projectId/tasks/:taskId", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "task-1" }));
		await makeClient().getTask("proj-1", "task-1");
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects/proj-1/tasks/task-1",
		);
	});
});

describe("PacaAPIClient – getTaskByNumber", () => {
	it("calls GET /api/v1/projects/:projectId/tasks/by-number/:number", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "task-1" }));
		await makeClient().getTaskByNumber("proj-1", 42);
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects/proj-1/tasks/by-number/42",
		);
	});
});

describe("PacaAPIClient – createTask", () => {
	it("calls POST /api/v1/projects/:id/tasks", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "new-task" }));
		await makeClient().createTask({ project_id: "proj-1", title: "New Task" });
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/tasks");
		expect(options.method).toBe("POST");
		expect(JSON.parse(options.body as string)).toMatchObject({
			title: "New Task",
		});
	});

	it("converts markdown description via markdownToBlocknote", async () => {
		const { markdownToBlocknote } = await import("../../utils/index.js");
		vi.stubGlobal("fetch", mockFetchOk({ id: "t1" }));
		await makeClient().createTask({
			project_id: "proj-1",
			title: "Task",
			description: "**Bold**",
		});
		expect(markdownToBlocknote).toHaveBeenCalledWith("**Bold**");
	});
});

// ---------------------------------------------------------------------------
// Task – update and delete
// ---------------------------------------------------------------------------

describe("PacaAPIClient – updateTask", () => {
	it("calls PATCH /api/v1/projects/:id/tasks/:taskId", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "t1" }));
		await makeClient().updateTask("proj-1", "t1", { title: "Renamed" });
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/tasks/t1");
		expect(options.method).toBe("PATCH");
		expect(JSON.parse(options.body as string)).toMatchObject({
			title: "Renamed",
		});
	});

	it("converts description markdown when provided", async () => {
		const { markdownToBlocknote } = await import("../../utils/index.js");
		vi.stubGlobal("fetch", mockFetchOk({ id: "t1" }));
		await makeClient().updateTask("proj-1", "t1", {
			description: "**Update**",
		});
		expect(markdownToBlocknote).toHaveBeenCalledWith("**Update**");
	});
});

describe("PacaAPIClient – deleteTask", () => {
	it("calls DELETE /api/v1/projects/:id/tasks/:taskId", async () => {
		vi.stubGlobal("fetch", mockFetchOk(null));
		await makeClient().deleteTask("proj-1", "t1");
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/tasks/t1");
		expect(options.method).toBe("DELETE");
	});
});

// ---------------------------------------------------------------------------
// Sprint methods
// ---------------------------------------------------------------------------

describe("PacaAPIClient – listSprints", () => {
	it("calls GET /api/v1/projects/:id/sprints", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		await makeClient().listSprints("proj-1");
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects/proj-1/sprints",
		);
	});
});

describe("PacaAPIClient – getSprint", () => {
	it("calls GET /api/v1/projects/:id/sprints/:sprintId", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "sprint-1" }));
		await makeClient().getSprint("proj-1", "sprint-1");
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects/proj-1/sprints/sprint-1",
		);
	});
});

describe("PacaAPIClient – createSprint", () => {
	it("calls POST /api/v1/projects/:id/sprints with body", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "sprint-1" }));
		await makeClient().createSprint({
			project_id: "proj-1",
			name: "Sprint 1",
			start_date: "2024-01-01",
			end_date: "2024-01-14",
		});
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/sprints");
		expect(options.method).toBe("POST");
		expect(JSON.parse(options.body as string)).toMatchObject({
			name: "Sprint 1",
		});
	});
});

describe("PacaAPIClient – updateSprint", () => {
	it("calls PATCH /api/v1/projects/:id/sprints/:sprintId", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "sprint-1" }));
		await makeClient().updateSprint("proj-1", "sprint-1", {
			name: "Sprint 1 Updated",
		});
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/sprints/sprint-1");
		expect(options.method).toBe("PATCH");
		expect(JSON.parse(options.body as string)).toMatchObject({
			name: "Sprint 1 Updated",
		});
	});
});

describe("PacaAPIClient – deleteSprint", () => {
	it("calls DELETE /api/v1/projects/:id/sprints/:sprintId", async () => {
		vi.stubGlobal("fetch", mockFetchOk(null));
		await makeClient().deleteSprint("proj-1", "sprint-1");
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/sprints/sprint-1");
		expect(options.method).toBe("DELETE");
	});
});

describe("PacaAPIClient – completeSprint", () => {
	it("calls POST /api/v1/projects/:id/sprints/:sprintId/complete", async () => {
		vi.stubGlobal(
			"fetch",
			mockFetchOk({ id: "sprint-1", status: "completed" }),
		);
		await makeClient().completeSprint("proj-1", "sprint-1");
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe(
			"http://api.test/api/v1/projects/proj-1/sprints/sprint-1/complete",
		);
		expect(options.method).toBe("POST");
	});
});

// ---------------------------------------------------------------------------
// Document methods
// ---------------------------------------------------------------------------

describe("PacaAPIClient – listDocuments", () => {
	it("calls GET /api/v1/projects/:id/docs without folder filter", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		await makeClient().listDocuments("proj-1");
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects/proj-1/docs",
		);
	});

	it("appends folder_id query param when provided", async () => {
		vi.stubGlobal("fetch", mockFetchOk([]));
		await makeClient().listDocuments("proj-1", "folder-1");
		expect((fetch as any).mock.calls[0][0]).toContain("folder_id=folder-1");
	});
});

describe("PacaAPIClient – getDocument", () => {
	it("calls GET /api/v1/projects/:id/docs/:docId", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "doc-1" }));
		await makeClient().getDocument("proj-1", "doc-1");
		expect((fetch as any).mock.calls[0][0]).toBe(
			"http://api.test/api/v1/projects/proj-1/docs/doc-1",
		);
	});
});

describe("PacaAPIClient – createDocument", () => {
	it("calls POST /api/v1/projects/:id/docs with mapped body", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "doc-1" }));
		await makeClient().createDocument({
			project_id: "proj-1",
			title: "My Doc",
		});
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/docs");
		expect(options.method).toBe("POST");
		expect(JSON.parse(options.body as string)).toMatchObject({
			title: "My Doc",
		});
	});

	it("converts content markdown to blocknote when provided", async () => {
		const { markdownToBlocknote } = await import("../../utils/index.js");
		vi.stubGlobal("fetch", mockFetchOk({ id: "doc-1" }));
		await makeClient().createDocument({
			project_id: "proj-1",
			title: "D",
			content: "# Heading",
		});
		expect(markdownToBlocknote).toHaveBeenCalledWith("# Heading");
	});
});

describe("PacaAPIClient – updateDocument", () => {
	it("calls PATCH /api/v1/projects/:id/docs/:docId", async () => {
		vi.stubGlobal("fetch", mockFetchOk({ id: "doc-1" }));
		await makeClient().updateDocument("proj-1", "doc-1", { title: "Renamed" });
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/docs/doc-1");
		expect(options.method).toBe("PATCH");
		expect(JSON.parse(options.body as string)).toMatchObject({
			title: "Renamed",
		});
	});
});

describe("PacaAPIClient – deleteDocument", () => {
	it("calls DELETE /api/v1/projects/:id/docs/:docId", async () => {
		vi.stubGlobal("fetch", mockFetchOk(null));
		await makeClient().deleteDocument("proj-1", "doc-1");
		const [url, options] = (fetch as any).mock.calls[0] as [
			string,
			RequestInit,
		];
		expect(url).toBe("http://api.test/api/v1/projects/proj-1/docs/doc-1");
		expect(options.method).toBe("DELETE");
	});
});
