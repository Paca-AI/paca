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
	addProjectMember,
	createProject,
	createProjectRole,
	deleteProject,
	deleteProjectRole,
	getProject,
	listProjectMembers,
	listProjectRoles,
	listProjects,
	type Project,
	type ProjectListResult,
	type ProjectMember,
	type ProjectRole,
	projectMembersQueryOptions,
	projectQueryOptions,
	projectRolesQueryOptions,
	projectsQueryOptions,
	removeProjectMember,
	updateProject,
	updateProjectMemberRole,
	updateProjectRole,
} from "./project-api";

// ── Fixtures ──────────────────────────────────────────────────────────────────

const mockProject: Project = {
	id: "p1",
	name: "Alpha",
	description: "First project",
	settings: {},
	created_by: "u1",
	created_at: "2026-01-01T00:00:00.000Z",
};

const mockMember: ProjectMember = {
	id: "m1",
	project_id: "p1",
	user_id: "u1",
	project_role_id: "r1",
	username: "alice",
	full_name: "Alice Smith",
	role_name: "Developer",
};

const mockRole: ProjectRole = {
	id: "r1",
	project_id: "p1",
	role_name: "Developer",
	permissions: { "tasks.*": true },
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

/** Wraps data in a success envelope like the real API client returns. */
function ok<T>(data: T) {
	return { data: { data, success: true } };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("project-api", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	// ── Project CRUD ───────────────────────────────────────────────────────────

	describe("listProjects", () => {
		it("fetches with default pagination and unwraps the result", async () => {
			const result: ProjectListResult = {
				items: [mockProject],
				total: 1,
				page: 1,
				page_size: 50,
			};
			mockGet.mockResolvedValue(ok(result));

			await expect(listProjects()).resolves.toEqual(result);
			expect(mockGet).toHaveBeenCalledWith("/projects", {
				params: { page: 1, page_size: 50 },
			});
		});

		it("forwards custom page and page_size params", async () => {
			const result: ProjectListResult = {
				items: [],
				total: 0,
				page: 3,
				page_size: 10,
			};
			mockGet.mockResolvedValue(ok(result));

			await listProjects(3, 10);
			expect(mockGet).toHaveBeenCalledWith("/projects", {
				params: { page: 3, page_size: 10 },
			});
		});
	});

	it("getProject fetches project by id and unwraps", async () => {
		mockGet.mockResolvedValue(ok(mockProject));

		await expect(getProject("p1")).resolves.toEqual(mockProject);
		expect(mockGet).toHaveBeenCalledWith("/projects/p1");
	});

	it("createProject posts payload and unwraps the created project", async () => {
		mockPost.mockResolvedValue(ok(mockProject));
		const payload = { name: "Alpha", description: "First project" };

		await expect(createProject(payload)).resolves.toEqual(mockProject);
		expect(mockPost).toHaveBeenCalledWith("/projects", payload);
	});

	it("createProject omits optional description field when not provided", async () => {
		mockPost.mockResolvedValue(ok(mockProject));

		await createProject({ name: "Minimal" });
		expect(mockPost).toHaveBeenCalledWith("/projects", {
			name: "Minimal",
		});
	});

	it("updateProject patches by id and unwraps the updated project", async () => {
		const updated = { ...mockProject, name: "Alpha v2" };
		mockPatch.mockResolvedValue(ok(updated));

		await expect(updateProject("p1", { name: "Alpha v2" })).resolves.toEqual(
			updated,
		);
		expect(mockPatch).toHaveBeenCalledWith("/projects/p1", {
			name: "Alpha v2",
		});
	});

	it("deleteProject sends DELETE to correct URL and returns undefined", async () => {
		mockDelete.mockResolvedValue({});

		await expect(deleteProject("p1")).resolves.toBeUndefined();
		expect(mockDelete).toHaveBeenCalledWith("/projects/p1");
	});

	// ── Members ────────────────────────────────────────────────────────────────

	describe("members", () => {
		it("listProjectMembers fetches members for a project and unwraps", async () => {
			mockGet.mockResolvedValue(ok([mockMember]));

			await expect(listProjectMembers("p1")).resolves.toEqual([mockMember]);
			expect(mockGet).toHaveBeenCalledWith("/projects/p1/members");
		});

		it("addProjectMember posts payload and unwraps the created member", async () => {
			mockPost.mockResolvedValue(ok(mockMember));
			const payload = { user_id: "u1", project_role_id: "r1" };

			await expect(addProjectMember("p1", payload)).resolves.toEqual(
				mockMember,
			);
			expect(mockPost).toHaveBeenCalledWith("/projects/p1/members", payload);
		});

		it("updateProjectMemberRole patches member role and unwraps the updated member", async () => {
			const updated = {
				...mockMember,
				project_role_id: "r2",
				role_name: "Lead",
			};
			mockPatch.mockResolvedValue(ok(updated));

			await expect(
				updateProjectMemberRole("p1", "u1", { project_role_id: "r2" }),
			).resolves.toEqual(updated);
			expect(mockPatch).toHaveBeenCalledWith("/projects/p1/members/u1", {
				project_role_id: "r2",
			});
		});

		it("removeProjectMember sends DELETE to correct URL and returns undefined", async () => {
			mockDelete.mockResolvedValue({});

			await expect(removeProjectMember("p1", "u1")).resolves.toBeUndefined();
			expect(mockDelete).toHaveBeenCalledWith("/projects/p1/members/u1");
		});
	});

	// ── Roles ──────────────────────────────────────────────────────────────────

	describe("roles", () => {
		it("listProjectRoles fetches roles for a project and unwraps", async () => {
			mockGet.mockResolvedValue(ok([mockRole]));

			await expect(listProjectRoles("p1")).resolves.toEqual([mockRole]);
			expect(mockGet).toHaveBeenCalledWith("/projects/p1/roles");
		});

		it("createProjectRole posts payload and unwraps the created role", async () => {
			mockPost.mockResolvedValue(ok(mockRole));
			const payload = {
				role_name: "Developer",
				permissions: { "tasks.*": true },
			};

			await expect(createProjectRole("p1", payload)).resolves.toEqual(mockRole);
			expect(mockPost).toHaveBeenCalledWith("/projects/p1/roles", payload);
		});

		it("createProjectRole works without optional permissions field", async () => {
			mockPost.mockResolvedValue(ok(mockRole));

			await createProjectRole("p1", { role_name: "Viewer" });
			expect(mockPost).toHaveBeenCalledWith("/projects/p1/roles", {
				role_name: "Viewer",
			});
		});

		it("updateProjectRole patches by role id and unwraps the updated role", async () => {
			const updated = { ...mockRole, role_name: "Senior Developer" };
			mockPatch.mockResolvedValue(ok(updated));

			await expect(
				updateProjectRole("p1", "r1", { role_name: "Senior Developer" }),
			).resolves.toEqual(updated);
			expect(mockPatch).toHaveBeenCalledWith("/projects/p1/roles/r1", {
				role_name: "Senior Developer",
			});
		});

		it("deleteProjectRole sends DELETE to correct URL and returns undefined", async () => {
			mockDelete.mockResolvedValue({});

			await expect(deleteProjectRole("p1", "r1")).resolves.toBeUndefined();
			expect(mockDelete).toHaveBeenCalledWith("/projects/p1/roles/r1");
		});
	});

	// ── Query Options ──────────────────────────────────────────────────────────

	describe("query options", () => {
		it("projectsQueryOptions exposes correct key and fn for default params", () => {
			const opts = projectsQueryOptions();
			expect(opts.queryKey).toEqual(["projects", { page: 1, pageSize: 50 }]);
			expect(typeof opts.queryFn).toBe("function");
		});

		it("projectsQueryOptions uses custom page and pageSize in key", () => {
			const opts = projectsQueryOptions(3, 10);
			expect(opts.queryKey).toEqual(["projects", { page: 3, pageSize: 10 }]);
		});

		it("projectQueryOptions exposes correct key, fn, and staleTime", () => {
			const opts = projectQueryOptions("p1");
			expect(opts.queryKey).toEqual(["projects", "p1"]);
			expect(typeof opts.queryFn).toBe("function");
			expect(opts.staleTime).toBe(2 * 60 * 1000);
		});

		it("projectMembersQueryOptions exposes correct key and fn", () => {
			const opts = projectMembersQueryOptions("p1");
			expect(opts.queryKey).toEqual(["projects", "p1", "members"]);
			expect(typeof opts.queryFn).toBe("function");
		});

		it("projectRolesQueryOptions exposes correct key and fn", () => {
			const opts = projectRolesQueryOptions("p1");
			expect(opts.queryKey).toEqual(["projects", "p1", "roles"]);
			expect(typeof opts.queryFn).toBe("function");
		});
	});
});
