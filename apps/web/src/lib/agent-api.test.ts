import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet } = vi.hoisted(() => ({
	mockGet: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
		},
	},
}));

import { listAgents, listConversations } from "./agent-api";

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
});
