import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPost, mockPatch, mockDelete } = vi.hoisted(() => ({
	mockGet: vi.fn(),
	mockPost: vi.fn(),
	mockPatch: vi.fn(),
	mockDelete: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			post: mockPost,
			patch: mockPatch,
			delete: mockDelete,
		},
	},
}));

import {
	addGlobalEnvVar,
	addGlobalMCPServer,
	addGlobalSkill,
	createGlobalAgent,
	deleteGlobalAgent,
	deleteGlobalEnvVar,
	deleteGlobalMCPServer,
	deleteGlobalSkill,
	generateGlobalAcpBridgeToken,
	getGlobalAcpBridgeStatus,
	getGlobalAgent,
	getGlobalConversation,
	heartbeatGlobalConversation,
	listAgents,
	listConversationEventWindow,
	listConversations,
	listGlobalAgents,
	listGlobalChatSessions,
	listGlobalConversationEvents,
	listGlobalConversations,
	listGlobalEnvVars,
	listGlobalMCPServers,
	listGlobalSkills,
	pauseGlobalConversation,
	sendGlobalChatMessage,
	sendGlobalConversationMessage,
	startGlobalChatSession,
	stopGlobalConversation,
	updateGlobalAgent,
	updateGlobalEnvVar,
	updateGlobalMCPServer,
	updateGlobalSkill,
} from "./agent-api";

const PROJECT_ID = "proj-1";

function ok<T>(data: T) {
	return { data: { data, success: true } };
}

function emptyPage() {
	return ok({ items: [], page_size: 20, next_cursor: null });
}

function paramsOf(callIndex = 0) {
	const [, config] = mockGet.mock.calls[callIndex] as [
		string,
		{ params: Record<string, unknown> },
	];
	return config.params ?? {};
}

describe("agent-api", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("listAgents", () => {
		it("sends no params when called without a scope", async () => {
			mockGet.mockResolvedValue(ok({ items: [] }));

			await listAgents(PROJECT_ID);

			expect(mockGet).toHaveBeenCalledWith(`/projects/${PROJECT_ID}/agents`, {
				params: undefined,
			});
		});

		it("forwards scope as a query param when given", async () => {
			mockGet.mockResolvedValue(ok({ items: [] }));

			await listAgents(PROJECT_ID, "project");

			expect(mockGet).toHaveBeenCalledWith(`/projects/${PROJECT_ID}/agents`, {
				params: { scope: "project" },
			});
		});
	});

	describe("listConversations", () => {
		it("defaults to page_size=20 with no other params when called without options", async () => {
			mockGet.mockResolvedValue(emptyPage());

			await listConversations(PROJECT_ID);

			expect(mockGet).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/conversations`,
				{ params: { page_size: 20 } },
			);
		});

		it("joins multi-value filters into comma-separated params", async () => {
			mockGet.mockResolvedValue(emptyPage());

			await listConversations(PROJECT_ID, {
				agentIds: ["agent-a", "agent-b"],
				statuses: ["running", "paused"],
				triggerTypes: ["task_assigned", "chat_message"],
			});

			const params = paramsOf();
			expect(params.agent_id).toBe("agent-a,agent-b");
			expect(params.status).toBe("running,paused");
			expect(params.trigger_type).toBe("task_assigned,chat_message");
		});

		it("omits array params when the array is empty", async () => {
			mockGet.mockResolvedValue(emptyPage());

			await listConversations(PROJECT_ID, {
				agentIds: [],
				statuses: [],
				triggerTypes: [],
			});

			const params = paramsOf();
			expect(params.agent_id).toBeUndefined();
			expect(params.status).toBeUndefined();
			expect(params.trigger_type).toBeUndefined();
		});

		it("converts createdAfter/createdBefore to UTC-instant boundaries of the local day", async () => {
			mockGet.mockResolvedValue(emptyPage());

			await listConversations(PROJECT_ID, {
				createdAfter: "2026-01-01",
				createdBefore: "2026-01-31",
			});

			// Computed the same way the implementation does (local Y/M/D ->
			// Date -> ISO) rather than hardcoding a UTC string, so this test
			// is correct regardless of the runner's local timezone.
			const wantAfter = new Date(2026, 0, 1, 0, 0, 0, 0).toISOString();
			const wantBefore = new Date(2026, 0, 31 + 1, 0, 0, 0, 0).toISOString();

			const params = paramsOf();
			expect(params.created_after).toBe(wantAfter);
			expect(params.created_before).toBe(wantBefore);
		});

		it("trims the search param and omits it when blank", async () => {
			mockGet.mockResolvedValue(emptyPage());

			await listConversations(PROJECT_ID, { search: "  login bug  " });
			expect(paramsOf().search).toBe("login bug");

			mockGet.mockClear();
			mockGet.mockResolvedValue(emptyPage());
			await listConversations(PROJECT_ID, { search: "   " });
			expect(paramsOf().search).toBeUndefined();
		});

		it("forwards cursor and a custom pageSize", async () => {
			mockGet.mockResolvedValue(emptyPage());

			await listConversations(PROJECT_ID, {
				cursor: "opaque-cursor",
				pageSize: 50,
			});

			const params = paramsOf();
			expect(params.cursor).toBe("opaque-cursor");
			expect(params.page_size).toBe(50);
		});
	});

	// ── Global agent CRUD (/admin/agents) ──────────────────────────────────────

	describe("global agent CRUD", () => {
		const AGENT_ID = "agent-1";

		it("listGlobalAgents fetches /admin/agents", async () => {
			mockGet.mockResolvedValue(ok({ items: [] }));
			await listGlobalAgents();
			expect(mockGet).toHaveBeenCalledWith("/admin/agents");
		});

		it("getGlobalAgent fetches /admin/agents/:agentId", async () => {
			mockGet.mockResolvedValue(ok({ id: AGENT_ID }));
			await getGlobalAgent(AGENT_ID);
			expect(mockGet).toHaveBeenCalledWith(`/admin/agents/${AGENT_ID}`);
		});

		it("createGlobalAgent posts the payload to /admin/agents", async () => {
			mockPost.mockResolvedValue(ok({ id: AGENT_ID }));
			const payload = { name: "Bot", handle: "bot" };
			await createGlobalAgent(payload);
			expect(mockPost).toHaveBeenCalledWith("/admin/agents", payload);
		});

		it("updateGlobalAgent patches /admin/agents/:agentId with the payload", async () => {
			mockPatch.mockResolvedValue(ok({ id: AGENT_ID }));
			const payload = { name: "Renamed" };
			await updateGlobalAgent(AGENT_ID, payload);
			expect(mockPatch).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}`,
				payload,
			);
		});

		it("deleteGlobalAgent deletes /admin/agents/:agentId", async () => {
			mockDelete.mockResolvedValue({});
			await deleteGlobalAgent(AGENT_ID);
			expect(mockDelete).toHaveBeenCalledWith(`/admin/agents/${AGENT_ID}`);
		});
	});

	// ── Global chat sessions (/agents/:agentId/chat-sessions) ──────────────────

	describe("global chat sessions", () => {
		const AGENT_ID = "agent-1";
		const SESSION_ID = "sess-1";

		it("listGlobalChatSessions fetches /agents/:agentId/chat-sessions", async () => {
			mockGet.mockResolvedValue(ok({ items: [] }));
			await listGlobalChatSessions(AGENT_ID);
			expect(mockGet).toHaveBeenCalledWith(`/agents/${AGENT_ID}/chat-sessions`);
		});

		it("startGlobalChatSession posts the message payload to /agents/:agentId/chat-sessions", async () => {
			mockPost.mockResolvedValue(
				ok({ session: { id: SESSION_ID }, conversation: { id: "conv-1" } }),
			);
			const payload = { message: "hi" };
			await startGlobalChatSession(AGENT_ID, payload);
			expect(mockPost).toHaveBeenCalledWith(
				`/agents/${AGENT_ID}/chat-sessions`,
				payload,
			);
		});

		it("sendGlobalChatMessage posts to /agents/chat-sessions/:sessionId/messages and unwraps .conversation", async () => {
			const conversation = { id: "conv-1", status: "running" };
			mockPost.mockResolvedValue(ok({ conversation }));
			const result = await sendGlobalChatMessage(SESSION_ID, {
				message: "hi",
			});
			expect(mockPost).toHaveBeenCalledWith(
				`/agents/chat-sessions/${SESSION_ID}/messages`,
				{ message: "hi" },
			);
			expect(result).toBe(conversation);
		});
	});

	// ── Global conversations (/agents/conversations) ────────────────────────────
	//
	// These are the endpoints where a prior authorization bug let a caller
	// read/stop/pause/heartbeat/message another user's global conversation
	// just by knowing its id (server-side ownership check was missing — see
	// agent_service.go's GetGlobalConversation). This client layer has no
	// ownership logic of its own — the server enforces it — but these tests
	// at least pin the request shape: the client sends only the id, never a
	// client-chosen actor/owner field that could paper over a server-side gap.

	describe("global conversations", () => {
		const CONV_ID = "conv-1";

		it("getGlobalConversation fetches /agents/conversations/:conversationId", async () => {
			mockGet.mockResolvedValue(ok({ id: CONV_ID }));
			await getGlobalConversation(CONV_ID);
			expect(mockGet).toHaveBeenCalledWith(`/agents/conversations/${CONV_ID}`);
		});

		it("listGlobalConversationEvents fetches events with a fixed limit param", async () => {
			mockGet.mockResolvedValue(ok({ items: [] }));
			await listGlobalConversationEvents(CONV_ID);
			expect(mockGet).toHaveBeenCalledWith(
				`/agents/conversations/${CONV_ID}/events`,
				{ params: { limit: 200 } },
			);
		});

		it("stopGlobalConversation posts to the stop endpoint with no body", async () => {
			mockPost.mockResolvedValue({});
			await stopGlobalConversation(CONV_ID);
			expect(mockPost).toHaveBeenCalledWith(
				`/agents/conversations/${CONV_ID}/stop`,
			);
		});

		it("pauseGlobalConversation posts to the pause endpoint with no body", async () => {
			mockPost.mockResolvedValue({});
			await pauseGlobalConversation(CONV_ID);
			expect(mockPost).toHaveBeenCalledWith(
				`/agents/conversations/${CONV_ID}/pause`,
			);
		});

		it("heartbeatGlobalConversation posts to the heartbeat endpoint with no body", async () => {
			mockPost.mockResolvedValue({});
			await heartbeatGlobalConversation(CONV_ID);
			expect(mockPost).toHaveBeenCalledWith(
				`/agents/conversations/${CONV_ID}/heartbeat`,
			);
		});

		it("sendGlobalConversationMessage posts only {message} — never a client-supplied actor/owner field", async () => {
			mockPost.mockResolvedValue({});
			await sendGlobalConversationMessage(CONV_ID, "hello");
			expect(mockPost).toHaveBeenCalledWith(
				`/agents/conversations/${CONV_ID}/messages`,
				{ message: "hello" },
			);
		});

		it("listGlobalConversations fetches /agents/conversations (no projectId param) with the shared filter builder", async () => {
			mockGet.mockResolvedValue(emptyPage());
			await listGlobalConversations({ pageSize: 10 });
			expect(mockGet).toHaveBeenCalledWith("/agents/conversations", {
				params: { page_size: 10 },
			});
		});
	});

	// ── Global agent MCP servers / skills / env vars (/admin/agents/:agentId/...) ──
	//
	// One representative CRUD group — the same list/add/update/delete shape
	// repeats for skills and env vars below, all mounted under /admin/agents
	// rather than /projects/:projectId/agents, per agent-api.ts's routing note.

	describe("global agent MCP servers", () => {
		const AGENT_ID = "agent-1";
		const SERVER_ID = "srv-1";

		it("listGlobalMCPServers fetches /admin/agents/:agentId/mcp-servers", async () => {
			mockGet.mockResolvedValue(ok({ items: [] }));
			await listGlobalMCPServers(AGENT_ID);
			expect(mockGet).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/mcp-servers`,
			);
		});

		it("addGlobalMCPServer posts the payload to /admin/agents/:agentId/mcp-servers", async () => {
			mockPost.mockResolvedValue(ok({ id: SERVER_ID }));
			const payload = { server_name: "custom", transport: "stdio" as const };
			await addGlobalMCPServer(AGENT_ID, payload);
			expect(mockPost).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/mcp-servers`,
				payload,
			);
		});

		it("updateGlobalMCPServer patches /admin/agents/:agentId/mcp-servers/:serverId", async () => {
			mockPatch.mockResolvedValue(ok({ id: SERVER_ID }));
			const payload = { is_enabled: false };
			await updateGlobalMCPServer(AGENT_ID, SERVER_ID, payload);
			expect(mockPatch).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/mcp-servers/${SERVER_ID}`,
				payload,
			);
		});

		it("deleteGlobalMCPServer deletes /admin/agents/:agentId/mcp-servers/:serverId", async () => {
			mockDelete.mockResolvedValue({});
			await deleteGlobalMCPServer(AGENT_ID, SERVER_ID);
			expect(mockDelete).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/mcp-servers/${SERVER_ID}`,
			);
		});
	});

	describe("global agent skills", () => {
		const AGENT_ID = "agent-1";
		const SKILL_ID = "skill-1";

		it("listGlobalSkills fetches /admin/agents/:agentId/skills", async () => {
			mockGet.mockResolvedValue(ok({ items: [] }));
			await listGlobalSkills(AGENT_ID);
			expect(mockGet).toHaveBeenCalledWith(`/admin/agents/${AGENT_ID}/skills`);
		});

		it("addGlobalSkill posts the payload to /admin/agents/:agentId/skills", async () => {
			mockPost.mockResolvedValue(ok({ id: SKILL_ID }));
			const payload = {
				skill_name: "custom",
				skill_source: "inline" as const,
			};
			await addGlobalSkill(AGENT_ID, payload);
			expect(mockPost).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/skills`,
				payload,
			);
		});

		it("updateGlobalSkill patches /admin/agents/:agentId/skills/:skillId", async () => {
			mockPatch.mockResolvedValue(ok({ id: SKILL_ID }));
			const payload = { is_enabled: false };
			await updateGlobalSkill(AGENT_ID, SKILL_ID, payload);
			expect(mockPatch).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/skills/${SKILL_ID}`,
				payload,
			);
		});

		it("deleteGlobalSkill deletes /admin/agents/:agentId/skills/:skillId", async () => {
			mockDelete.mockResolvedValue({});
			await deleteGlobalSkill(AGENT_ID, SKILL_ID);
			expect(mockDelete).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/skills/${SKILL_ID}`,
			);
		});
	});

	describe("global agent env vars", () => {
		const AGENT_ID = "agent-1";
		const ENV_VAR_ID = "env-1";

		it("listGlobalEnvVars fetches /admin/agents/:agentId/env-vars", async () => {
			mockGet.mockResolvedValue(ok({ items: [] }));
			await listGlobalEnvVars(AGENT_ID);
			expect(mockGet).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/env-vars`,
			);
		});

		it("addGlobalEnvVar posts the payload to /admin/agents/:agentId/env-vars", async () => {
			mockPost.mockResolvedValue(ok({ id: ENV_VAR_ID }));
			const payload = { key: "FOO", value: "bar" };
			await addGlobalEnvVar(AGENT_ID, payload);
			expect(mockPost).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/env-vars`,
				payload,
			);
		});

		it("updateGlobalEnvVar patches /admin/agents/:agentId/env-vars/:envVarId", async () => {
			mockPatch.mockResolvedValue(ok({ id: ENV_VAR_ID }));
			const payload = { value: "baz" };
			await updateGlobalEnvVar(AGENT_ID, ENV_VAR_ID, payload);
			expect(mockPatch).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/env-vars/${ENV_VAR_ID}`,
				payload,
			);
		});

		it("deleteGlobalEnvVar deletes /admin/agents/:agentId/env-vars/:envVarId", async () => {
			mockDelete.mockResolvedValue({});
			await deleteGlobalEnvVar(AGENT_ID, ENV_VAR_ID);
			expect(mockDelete).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/env-vars/${ENV_VAR_ID}`,
			);
		});
	});

	// ── Global ACP bridge (/admin/agents/:agentId/acp-bridge-*) ─────────────────

	describe("global ACP bridge", () => {
		const AGENT_ID = "agent-1";

		it("generateGlobalAcpBridgeToken posts to /admin/agents/:agentId/acp-bridge-token", async () => {
			mockPost.mockResolvedValue(ok({ token: "tok", run_command: "paca" }));
			await generateGlobalAcpBridgeToken(AGENT_ID);
			expect(mockPost).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/acp-bridge-token`,
			);
		});

		it("getGlobalAcpBridgeStatus fetches the pass-through (non-enveloped) status", async () => {
			mockGet.mockResolvedValue({ data: { connected: true } });
			const result = await getGlobalAcpBridgeStatus(AGENT_ID);
			expect(mockGet).toHaveBeenCalledWith(
				`/admin/agents/${AGENT_ID}/acp-bridge-status`,
			);
			expect(result).toEqual({ connected: true });
		});
	});
	describe("listConversationEventWindow", () => {
		it("requests exactly the window it was asked for", async () => {
			mockGet.mockResolvedValueOnce(ok({ items: [], total: 900 }));

			const page = await listConversationEventWindow(PROJECT_ID, "conv-1", {
				offset: 700,
				limit: 200,
			});

			expect(mockGet).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/conversations/conv-1/events`,
				{ params: { limit: 200, offset: 700 } },
			);
			expect(page.total).toBe(900);
		});

		it("infers a total from the window when the API omits one", async () => {
			mockGet.mockResolvedValueOnce(
				ok({ items: [{ event_index: 10 }, { event_index: 11 }] }),
			);

			const page = await listConversationEventWindow(PROJECT_ID, "conv-1", {
				offset: 10,
				limit: 200,
			});

			expect(page.total).toBe(12);
			expect(page.items).toHaveLength(2);
		});
	});
});
