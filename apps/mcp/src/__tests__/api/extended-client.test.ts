import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PacaAPIExtendedClient } from "../../api/extended-client.js";

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

describe("PacaAPIExtendedClient", () => {
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		fetchMock = vi.fn().mockResolvedValue(okEnvelope([]));
		vi.stubGlobal("fetch", fetchMock);
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	// ---------------------------------------------------------------------------
	// request helpers
	// ---------------------------------------------------------------------------

	it("includes X-API-Key header", async () => {
		fetchMock.mockResolvedValue(okEnvelope([]));
		const client = new PacaAPIExtendedClient(CONFIG);
		await client.listProjectMembers("p1");
		expect(fetchMock.mock.calls[0][1].headers["X-API-Key"]).toBe("key123");
	});

	it("includes X-Agent-ID header when agentId is set", async () => {
		fetchMock.mockResolvedValue(okEnvelope([]));
		const client = new PacaAPIExtendedClient(CONFIG_WITH_AGENT);
		await client.listProjectMembers("p1");
		expect(fetchMock.mock.calls[0][1].headers["X-Agent-ID"]).toBe("agent-1");
	});

	it("omits X-Agent-ID header when agentId is absent", async () => {
		fetchMock.mockResolvedValue(okEnvelope([]));
		const client = new PacaAPIExtendedClient(CONFIG);
		await client.listProjectMembers("p1");
		expect(fetchMock.mock.calls[0][1].headers["X-Agent-ID"]).toBeUndefined();
	});

	it("throws on non-OK response", async () => {
		fetchMock.mockResolvedValue(errorResponse(500, "Server Error"));
		const client = new PacaAPIExtendedClient(CONFIG);
		await expect(client.listProjectMembers("p1")).rejects.toThrow("500");
	});

	it("resolves on 204 No Content without parsing JSON", async () => {
		fetchMock.mockResolvedValue(noContentResponse());
		const client = new PacaAPIExtendedClient(CONFIG);
		await expect(client.deleteProjectRole("p1", "r1")).resolves.toBeUndefined();
	});

	it("returns raw JSON when response is not a SuccessEnvelope", async () => {
		const raw = [{ id: "m1" }];
		fetchMock.mockResolvedValue(rawOk(raw));
		const client = new PacaAPIExtendedClient(CONFIG);
		const result = await client.listProjectMembers("p1");
		expect(result).toEqual(raw);
	});

	// ---------------------------------------------------------------------------
	// Project Members
	// ---------------------------------------------------------------------------

	describe("listProjectMembers", () => {
		it("calls GET /api/v1/projects/:id/members", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "m1" }]));
			const result = await client.listProjectMembers("p1");
			expect(fetchMock).toHaveBeenCalledWith(
				"https://api.example.com/api/v1/projects/p1/members",
				expect.objectContaining({ method: "GET" }),
			);
			expect(result).toEqual([{ id: "m1" }]);
		});

		it("extracts .items when response is an object with items", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ items: [{ id: "m1" }] }));
			const result = await client.listProjectMembers("p1");
			expect(result).toEqual([{ id: "m1" }]);
		});
	});

	describe("addProjectMember", () => {
		it("calls POST /api/v1/projects/:id/members", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "m2" }));
			const result = await client.addProjectMember("p1", {
				user_id: "u1",
				role_ids: ["r1"],
			});
			expect(fetchMock.mock.calls[0][0]).toContain("/members");
			expect(fetchMock.mock.calls[0][1].method).toBe("POST");
			expect(result).toEqual({ id: "m2" });
		});
	});

	describe("getMyProjectPermissions", () => {
		it("returns actions from response", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ actions: ["tasks:write"] }));
			const result = await client.getMyProjectPermissions("p1");
			expect(result).toEqual(["tasks:write"]);
		});

		it("returns an empty list when actions is missing", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({}));
			const result = await client.getMyProjectPermissions("p1");
			expect(result).toEqual([]);
		});
	});

	describe("updateProjectMemberRole", () => {
		it("calls PUT /api/v1/projects/:id/members/:memberId/roles", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "r2" }]));
			await client.updateProjectMemberRole("p1", "m1", { role_ids: ["r2"] });
			expect(fetchMock.mock.calls[0][0]).toContain("/members/m1/roles");
			expect(fetchMock.mock.calls[0][1].method).toBe("PUT");
		});
	});

	describe("removeProjectMember", () => {
		it("calls DELETE /api/v1/projects/:id/members/:userId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.removeProjectMember("p1", "u1");
			expect(fetchMock.mock.calls[0][0]).toContain("/members/u1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});

	// ---------------------------------------------------------------------------
	// Project Roles
	// ---------------------------------------------------------------------------

	describe("listProjectRoles", () => {
		it("calls GET /api/v1/projects/:id/roles", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "r1" }]));
			const result = await client.listProjectRoles("p1");
			expect(fetchMock.mock.calls[0][0]).toContain("/roles");
			expect(result).toEqual([{ id: "r1" }]);
		});

		it("extracts .items from object response", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ items: [{ id: "r1" }] }));
			const result = await client.listProjectRoles("p1");
			expect(result).toEqual([{ id: "r1" }]);
		});
	});

	describe("createProjectRole", () => {
		it("calls POST /api/v1/projects/:id/roles", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "r2" }));
			await client.createProjectRole("p1", {
				name: "Dev",
				policy: { statements: [] },
			});
			expect(fetchMock.mock.calls[0][1].method).toBe("POST");
		});
	});

	describe("updateProjectRole", () => {
		it("calls PUT /api/v1/projects/:id/roles/:roleId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "r1" }));
			await client.updateProjectRole("p1", "r1", {
				name: "Lead",
				description: "",
				policy: { statements: [] },
			});
			expect(fetchMock.mock.calls[0][0]).toContain("/roles/r1");
			expect(fetchMock.mock.calls[0][1].method).toBe("PUT");
		});
	});

	describe("deleteProjectRole", () => {
		it("calls DELETE /api/v1/projects/:id/roles/:roleId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.deleteProjectRole("p1", "r1");
			expect(fetchMock.mock.calls[0][0]).toContain("/roles/r1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});

	// ---------------------------------------------------------------------------
	// Task Types
	// ---------------------------------------------------------------------------

	describe("listTaskTypes", () => {
		it("calls GET /api/v1/projects/:id/task-types", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "ty1" }]));
			const result = await client.listTaskTypes("p1");
			expect(fetchMock.mock.calls[0][0]).toContain("/task-types");
			expect(result).toEqual([{ id: "ty1" }]);
		});
	});

	describe("createTaskType", () => {
		it("calls POST /api/v1/projects/:id/task-types", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "ty2" }));
			await client.createTaskType("p1", { name: "Bug" });
			expect(fetchMock.mock.calls[0][1].method).toBe("POST");
		});
	});

	describe("updateTaskType", () => {
		it("calls PATCH /api/v1/projects/:id/task-types/:typeId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "ty1" }));
			await client.updateTaskType("p1", "ty1", { name: "Epic" });
			expect(fetchMock.mock.calls[0][0]).toContain("/task-types/ty1");
			expect(fetchMock.mock.calls[0][1].method).toBe("PATCH");
		});
	});

	describe("deleteTaskType", () => {
		it("calls DELETE /api/v1/projects/:id/task-types/:typeId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.deleteTaskType("p1", "ty1");
			expect(fetchMock.mock.calls[0][0]).toContain("/task-types/ty1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});

	describe("setDefaultTaskType", () => {
		it("calls PUT /api/v1/projects/:id/task-types/:typeId/set-default", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "ty1", is_default: true }));
			await client.setDefaultTaskType("p1", "ty1");
			expect(fetchMock.mock.calls[0][0]).toContain("/set-default");
			expect(fetchMock.mock.calls[0][1].method).toBe("PUT");
		});
	});

	// ---------------------------------------------------------------------------
	// Task Statuses
	// ---------------------------------------------------------------------------

	describe("listTaskStatuses", () => {
		it("calls GET /api/v1/projects/:id/task-statuses", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "st1" }]));
			const result = await client.listTaskStatuses("p1");
			expect(fetchMock.mock.calls[0][0]).toContain("/task-statuses");
			expect(result).toEqual([{ id: "st1" }]);
		});
	});

	describe("createTaskStatus", () => {
		it("calls POST /api/v1/projects/:id/task-statuses", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "st2" }));
			await client.createTaskStatus("p1", {
				name: "Done",
				category: "done",
				position: 0,
			});
			expect(fetchMock.mock.calls[0][1].method).toBe("POST");
		});
	});

	describe("updateTaskStatus", () => {
		it("calls PATCH /api/v1/projects/:id/task-statuses/:statusId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "st1" }));
			await client.updateTaskStatus("p1", "st1", { name: "Closed" });
			expect(fetchMock.mock.calls[0][0]).toContain("/task-statuses/st1");
			expect(fetchMock.mock.calls[0][1].method).toBe("PATCH");
		});
	});

	describe("deleteTaskStatus", () => {
		it("calls DELETE /api/v1/projects/:id/task-statuses/:statusId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.deleteTaskStatus("p1", "st1");
			expect(fetchMock.mock.calls[0][0]).toContain("/task-statuses/st1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});

	describe("setDefaultTaskStatus", () => {
		it("calls PUT .../set-default for task status", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "st1", is_default: true }));
			await client.setDefaultTaskStatus("p1", "st1");
			expect(fetchMock.mock.calls[0][0]).toContain(
				"/task-statuses/st1/set-default",
			);
			expect(fetchMock.mock.calls[0][1].method).toBe("PUT");
		});
	});

	// ---------------------------------------------------------------------------
	// Custom Field Definitions
	// ---------------------------------------------------------------------------

	describe("listCustomFieldDefinitions", () => {
		it("calls GET /api/v1/projects/:id/custom-fields", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "cf1" }]));
			const result = await client.listCustomFieldDefinitions("p1");
			expect(fetchMock.mock.calls[0][0]).toContain("/custom-fields");
			expect(result).toEqual([{ id: "cf1" }]);
		});
	});

	describe("getCustomFieldDefinition", () => {
		it("calls GET /api/v1/projects/:id/custom-fields/:fieldId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "cf1" }));
			const result = await client.getCustomFieldDefinition("p1", "cf1");
			expect(fetchMock.mock.calls[0][0]).toContain("/custom-fields/cf1");
			expect(result).toEqual({ id: "cf1" });
		});
	});

	describe("createCustomFieldDefinition", () => {
		it("calls POST /api/v1/projects/:id/custom-fields", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "cf2" }));
			await client.createCustomFieldDefinition("p1", {
				field_key: "priority",
				display_name: "Priority",
				field_type: "select",
			});
			expect(fetchMock.mock.calls[0][1].method).toBe("POST");
		});
	});

	describe("updateCustomFieldDefinition", () => {
		it("calls PATCH /api/v1/projects/:id/custom-fields/:fieldId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "cf1" }));
			await client.updateCustomFieldDefinition("p1", "cf1", {
				display_name: "Priority V2",
			});
			expect(fetchMock.mock.calls[0][0]).toContain("/custom-fields/cf1");
			expect(fetchMock.mock.calls[0][1].method).toBe("PATCH");
		});
	});

	describe("deleteCustomFieldDefinition", () => {
		it("calls DELETE /api/v1/projects/:id/custom-fields/:fieldId", async () => {
			const client = new PacaAPIExtendedClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.deleteCustomFieldDefinition("p1", "cf1");
			expect(fetchMock.mock.calls[0][0]).toContain("/custom-fields/cf1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});
});
