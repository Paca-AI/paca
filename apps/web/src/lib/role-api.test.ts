import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPost, mockPut, mockDelete } = vi.hoisted(() => ({
	mockGet: vi.fn(),
	mockPost: vi.fn(),
	mockPut: vi.fn(),
	mockDelete: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			post: mockPost,
			put: mockPut,
			delete: mockDelete,
		},
	},
}));

import type { Policy } from "./policy";
import {
	createRole,
	deleteRole,
	getAttributeSchema,
	getKnownActions,
	getRole,
	listRoles,
	platformRolesQueryOptions,
	projectRolesQueryOptions,
	type Role,
	replaceAgentRoles,
	replaceMemberRoles,
	replaceUserRoles,
	setDefaultRole,
	simulatePolicy,
	updateRole,
	validatePolicy,
} from "./role-api";

const ok = <T>(data: T) => ({
	data: { data, error_code: null, message: "ok" },
});

const policy: Policy = {
	version: "2026-10-01",
	statements: [
		{ effect: "Allow", actions: ["tasks:read"], resources: ["project/*"] },
	],
};

const role: Role = {
	id: "r1",
	name: "Reader",
	description: "",
	policy,
	project_id: null,
	is_system: false,
	is_default: false,
	attachment_count: 2,
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

describe("role-api", () => {
	beforeEach(() => vi.clearAllMocks());

	describe("workspace roles", () => {
		it("lists them from /admin/roles", async () => {
			mockGet.mockResolvedValue(ok([role]));
			await expect(listRoles()).resolves.toEqual([role]);
			expect(mockGet).toHaveBeenCalledWith("/admin/roles");
		});

		it("gets one by id", async () => {
			mockGet.mockResolvedValue(ok(role));
			await expect(getRole("r1")).resolves.toEqual(role);
			expect(mockGet).toHaveBeenCalledWith("/admin/roles/r1");
		});

		it("creates with the policy document, not a permission map", async () => {
			mockPost.mockResolvedValue(ok(role));
			const input = { name: "Reader", description: "", policy };
			await expect(createRole(input)).resolves.toEqual(role);
			expect(mockPost).toHaveBeenCalledWith("/admin/roles", input);
		});

		it("replaces with PUT", async () => {
			mockPut.mockResolvedValue(ok(role));
			const input = { name: "Reader", description: "", policy };
			await expect(updateRole("r1", input)).resolves.toEqual(role);
			expect(mockPut).toHaveBeenCalledWith("/admin/roles/r1", input);
		});

		it("deletes by id", async () => {
			mockDelete.mockResolvedValue({});
			await expect(deleteRole("r1")).resolves.toBeUndefined();
			expect(mockDelete).toHaveBeenCalledWith("/admin/roles/r1");
		});

		it("makes a role the default", async () => {
			mockPut.mockResolvedValue(ok({ ...role, is_default: true }));
			await expect(setDefaultRole("r1")).resolves.toMatchObject({
				is_default: true,
			});
			expect(mockPut).toHaveBeenCalledWith("/admin/roles/r1/default");
		});
	});

	describe("project roles", () => {
		it("addresses the project's own routes", async () => {
			mockGet.mockResolvedValue(ok([role]));
			await listRoles("p1");
			expect(mockGet).toHaveBeenCalledWith("/projects/p1/roles");

			mockPost.mockResolvedValue(ok(role));
			await createRole({ name: "x", description: "", policy }, "p1");
			expect(mockPost).toHaveBeenCalledWith(
				"/projects/p1/roles",
				expect.anything(),
			);

			mockPut.mockResolvedValue(ok(role));
			await updateRole("r1", { name: "x", description: "", policy }, "p1");
			expect(mockPut).toHaveBeenCalledWith(
				"/projects/p1/roles/r1",
				expect.anything(),
			);

			mockDelete.mockResolvedValue({});
			await deleteRole("r1", "p1");
			expect(mockDelete).toHaveBeenCalledWith("/projects/p1/roles/r1");
		});
	});

	describe("policy helpers", () => {
		it("validates through the workspace or the project route", async () => {
			mockPost.mockResolvedValue(ok({ valid: true, issues: [] }));
			await expect(validatePolicy(policy)).resolves.toEqual({
				valid: true,
				issues: [],
			});
			expect(mockPost).toHaveBeenCalledWith("/roles/validate", { policy });

			await validatePolicy(policy, "p1");
			expect(mockPost).toHaveBeenLastCalledWith("/projects/p1/roles/validate", {
				policy,
			});
		});

		it("simulates a request", async () => {
			mockPost.mockResolvedValue(ok({ allowed: false, matched: [] }));
			const input = {
				policy,
				action: "tasks:read",
				resource: "project/p/task/t",
			};
			await expect(simulatePolicy(input)).resolves.toEqual({
				allowed: false,
				matched: [],
			});
			expect(mockPost).toHaveBeenCalledWith("/roles/simulate", input);
		});

		it("reads the action catalogue and the attribute schema", async () => {
			mockGet.mockResolvedValue(ok(["tasks:read"]));
			await expect(getKnownActions()).resolves.toEqual(["tasks:read"]);
			expect(mockGet).toHaveBeenCalledWith("/roles/actions");

			mockGet.mockResolvedValue(ok([]));
			await getAttributeSchema("p1");
			expect(mockGet).toHaveBeenLastCalledWith(
				"/projects/p1/roles/attribute-schema",
			);
		});
	});

	describe("assignment replaces the whole set of roles", () => {
		it("for a user", async () => {
			mockPut.mockResolvedValue(ok([role]));
			await expect(replaceUserRoles("u1", ["r1", "r2"])).resolves.toEqual([
				role,
			]);
			expect(mockPut).toHaveBeenCalledWith("/admin/users/u1/roles", {
				role_ids: ["r1", "r2"],
			});
		});

		it("for a global agent, including none", async () => {
			mockPut.mockResolvedValue(ok([]));
			await replaceAgentRoles("a1", []);
			expect(mockPut).toHaveBeenCalledWith("/admin/agents/a1/roles", {
				role_ids: [],
			});
		});

		it("for a project member", async () => {
			mockPut.mockResolvedValue(ok([role]));
			await replaceMemberRoles("p1", "m1", ["r1"]);
			expect(mockPut).toHaveBeenCalledWith("/projects/p1/members/m1/roles", {
				role_ids: ["r1"],
			});
		});
	});

	it("exposes query keys", () => {
		expect(platformRolesQueryOptions.queryKey).toEqual(["admin", "roles"]);
		expect(projectRolesQueryOptions("p1").queryKey).toEqual([
			"projects",
			"p1",
			"roles",
		]);
	});
});
