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
	createGlobalRole,
	deleteGlobalRole,
	type GlobalRole,
	getGlobalRoles,
	getMyGlobalPermissions,
	globalRolesQueryOptions,
	myPermissionsQueryOptions,
	updateGlobalRole,
} from "./admin-api";

describe("admin-api", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("unwraps global roles from response", async () => {
		const roles: GlobalRole[] = [
			{
				id: "r1",
				name: "Admin",
				permissions: { "users.manage": true },
				created_at: "2026-03-29T00:00:00.000Z",
				updated_at: "2026-03-29T00:00:00.000Z",
			},
		];
		mockGet.mockResolvedValue({
			data: { data: roles, error_code: null, message: "ok" },
		});

		await expect(getGlobalRoles()).resolves.toEqual(roles);
		expect(mockGet).toHaveBeenCalledWith("/admin/global-roles");
	});

	it("posts payload to create role and unwraps response", async () => {
		const payload = {
			name: "Editor",
			permissions: { "projects.read": true },
		};
		const created: GlobalRole = {
			id: "r2",
			name: "Editor",
			permissions: payload.permissions,
			created_at: "2026-03-29T00:00:00.000Z",
			updated_at: "2026-03-29T00:00:00.000Z",
		};
		mockPost.mockResolvedValue({
			data: { data: created, error_code: null, message: "ok" },
		});

		await expect(createGlobalRole(payload)).resolves.toEqual(created);
		expect(mockPost).toHaveBeenCalledWith("/admin/global-roles", payload);
	});

	it("patches role by id and unwraps response", async () => {
		const payload = {
			name: "Support",
			permissions: { "tickets.read": true },
		};
		const updated: GlobalRole = {
			id: "r3",
			name: "Support",
			permissions: payload.permissions,
			created_at: "2026-03-29T00:00:00.000Z",
			updated_at: "2026-03-29T00:01:00.000Z",
		};
		mockPatch.mockResolvedValue({
			data: { data: updated, error_code: null, message: "ok" },
		});

		await expect(updateGlobalRole("r3", payload)).resolves.toEqual(updated);
		expect(mockPatch).toHaveBeenCalledWith("/admin/global-roles/r3", payload);
	});

	it("deletes role by id", async () => {
		mockDelete.mockResolvedValue({});

		await expect(deleteGlobalRole("r4")).resolves.toBeUndefined();
		expect(mockDelete).toHaveBeenCalledWith("/admin/global-roles/r4");
	});

	it("unwraps global permissions list from response", async () => {
		mockGet.mockResolvedValue({
			data: {
				data: { permissions: ["users.read", "projects.*"] },
				error_code: null,
				message: "ok",
			},
		});

		await expect(getMyGlobalPermissions()).resolves.toEqual([
			"users.read",
			"projects.*",
		]);
		expect(mockGet).toHaveBeenCalledWith("/users/me/global-permissions");
	});

	it("exposes query option contracts", () => {
		expect(globalRolesQueryOptions.queryKey).toEqual(["admin", "global-roles"]);
		expect(typeof globalRolesQueryOptions.queryFn).toBe("function");

		expect(myPermissionsQueryOptions.queryKey).toEqual([
			"auth",
			"me",
			"permissions",
		]);
		expect(typeof myPermissionsQueryOptions.queryFn).toBe("function");
		expect(myPermissionsQueryOptions.staleTime).toBe(5 * 60 * 1000);
		expect(myPermissionsQueryOptions.retry).toBe(false);
	});
});
