import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PacaAPIConversationClient } from "../../api/conversation-client.js";

const CONFIG = { baseURL: "https://api.example.com", apiKey: "key123" };

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

describe("PacaAPIConversationClient", () => {
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		fetchMock = vi.fn().mockResolvedValue(okEnvelope({}));
		vi.stubGlobal("fetch", fetchMock);
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	// ---------------------------------------------------------------------------
	// Basic request behaviour
	// ---------------------------------------------------------------------------

	it("includes X-API-Key header", async () => {
		const client = new PacaAPIConversationClient(CONFIG);
		await client.getConversation("c1");
		expect(fetchMock.mock.calls[0][1].headers["X-API-Key"]).toBe("key123");
	});

	it("sends X-Agent-ID header when agentId is configured", async () => {
		const client = new PacaAPIConversationClient({
			...CONFIG,
			agentId: "agent-1",
		});
		await client.getConversation("c1");
		expect(fetchMock.mock.calls[0][1].headers["X-Agent-ID"]).toBe("agent-1");
	});

	it("omits X-Agent-ID when agentId is not configured", async () => {
		const client = new PacaAPIConversationClient(CONFIG);
		await client.getConversation("c1");
		expect(fetchMock.mock.calls[0][1].headers["X-Agent-ID"]).toBeUndefined();
	});

	it("sends X-Actor-User-ID header only alongside agentId", async () => {
		const client = new PacaAPIConversationClient({
			...CONFIG,
			agentId: "agent-1",
			actorUserId: "user-7",
		});
		await client.getConversation("c1");
		expect(fetchMock.mock.calls[0][1].headers["X-Actor-User-ID"]).toBe(
			"user-7",
		);
	});

	it("omits X-Actor-User-ID when actorUserId is configured without agentId", async () => {
		const client = new PacaAPIConversationClient({
			...CONFIG,
			actorUserId: "user-7",
		});
		await client.getConversation("c1");
		expect(
			fetchMock.mock.calls[0][1].headers["X-Actor-User-ID"],
		).toBeUndefined();
	});

	it("sends X-Conversation-ID header only alongside agentId", async () => {
		const client = new PacaAPIConversationClient({
			...CONFIG,
			agentId: "agent-1",
			conversationId: "conv-current",
		});
		await client.getConversation("c1");
		expect(fetchMock.mock.calls[0][1].headers["X-Conversation-ID"]).toBe(
			"conv-current",
		);
	});

	it("omits X-Conversation-ID when conversationId is configured without agentId", async () => {
		const client = new PacaAPIConversationClient({
			...CONFIG,
			conversationId: "conv-current",
		});
		await client.getConversation("c1");
		expect(
			fetchMock.mock.calls[0][1].headers["X-Conversation-ID"],
		).toBeUndefined();
	});

	it("omits X-Conversation-ID when conversationId is not configured", async () => {
		const client = new PacaAPIConversationClient({
			...CONFIG,
			agentId: "agent-1",
		});
		await client.getConversation("c1");
		expect(
			fetchMock.mock.calls[0][1].headers["X-Conversation-ID"],
		).toBeUndefined();
	});

	it("throws on non-OK response", async () => {
		fetchMock.mockResolvedValue(errorResponse(503, "Service Unavailable"));
		const client = new PacaAPIConversationClient(CONFIG);
		await expect(client.getConversation("c1")).rejects.toThrow("503");
	});

	it("returns raw JSON when not a SuccessEnvelope", async () => {
		fetchMock.mockResolvedValue(rawOk({ id: "c1", status: "running" }));
		const client = new PacaAPIConversationClient(CONFIG);
		const result = await client.getConversation("c1");
		expect(result).toEqual({ id: "c1", status: "running" });
	});

	it("sends only GET requests (no body)", async () => {
		const client = new PacaAPIConversationClient(CONFIG);
		await client.getConversation("c1");
		expect(fetchMock.mock.calls[0][1].method).toBe("GET");
		expect(fetchMock.mock.calls[0][1].body).toBeUndefined();
	});

	// ---------------------------------------------------------------------------
	// Agent self-service conversation reads — GET /agents/me/conversations/:id.
	// No projectId: this path authorizes by the conversation's own agent_id
	// matching the caller (see agentdom.Service.GetConversationForAgent), not
	// by project or human membership, so it works identically for a
	// project-scoped or global conversation.
	// ---------------------------------------------------------------------------

	describe("getConversation", () => {
		it("calls GET /api/v1/agents/me/conversations/:conversationId", async () => {
			const client = new PacaAPIConversationClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "c1", status: "running" }));
			const result = await client.getConversation("c1");
			expect(fetchMock.mock.calls[0][0]).toBe(
				"https://api.example.com/api/v1/agents/me/conversations/c1",
			);
			expect(result).toEqual({ id: "c1", status: "running" });
		});
	});

	describe("listConversationEvents", () => {
		it("calls GET /api/v1/agents/me/conversations/:conversationId/events with no query string by default", async () => {
			const client = new PacaAPIConversationClient(CONFIG);
			fetchMock.mockResolvedValue(
				okEnvelope({
					items: [],
					total: 0,
					next_cursor: null,
					prev_cursor: null,
				}),
			);
			await client.listConversationEvents("c1");
			expect(fetchMock.mock.calls[0][0]).toBe(
				"https://api.example.com/api/v1/agents/me/conversations/c1/events",
			);
		});

		it("builds after/before/limit query params", async () => {
			const client = new PacaAPIConversationClient(CONFIG);
			fetchMock.mockResolvedValue(
				okEnvelope({
					items: [],
					total: 0,
					next_cursor: null,
					prev_cursor: null,
				}),
			);
			await client.listConversationEvents("c1", { after: "cur1", limit: 25 });
			const url = fetchMock.mock.calls[0][0] as string;
			expect(url).toContain("after=cur1");
			expect(url).toContain("limit=25");
			expect(url).not.toContain("before=");
		});

		it("URL-encodes cursor values", async () => {
			const client = new PacaAPIConversationClient(CONFIG);
			fetchMock.mockResolvedValue(
				okEnvelope({
					items: [],
					total: 0,
					next_cursor: null,
					prev_cursor: null,
				}),
			);
			await client.listConversationEvents("c1", { before: "a b&c" });
			const url = fetchMock.mock.calls[0][0] as string;
			expect(url).toContain(`before=${encodeURIComponent("a b&c")}`);
		});

		it("normalizes the items/total/cursor response shape", async () => {
			const client = new PacaAPIConversationClient(CONFIG);
			const events = [
				{
					id: "e1",
					conversation_id: "c1",
					event_index: 0,
					event_type: "user_message",
					event_source: "user",
					payload: {},
					created_at: "2024-01-01T00:00:00Z",
				},
			];
			fetchMock.mockResolvedValue(
				okEnvelope({
					items: events,
					total: 5,
					next_cursor: "n1",
					prev_cursor: null,
				}),
			);
			const result = await client.listConversationEvents("c1");
			expect(result).toEqual({
				items: events,
				total: 5,
				next_cursor: "n1",
				prev_cursor: null,
			});
		});

		it("defaults missing fields when the response omits them", async () => {
			const client = new PacaAPIConversationClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({}));
			const result = await client.listConversationEvents("c1");
			expect(result).toEqual({
				items: [],
				total: 0,
				next_cursor: null,
				prev_cursor: null,
			});
		});
	});
});
