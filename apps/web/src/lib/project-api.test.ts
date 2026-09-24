import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPost, mockPatch, mockDelete, mockPut } = vi.hoisted(
	() => ({
		mockGet: vi.fn(),
		mockPost: vi.fn(),
		mockPatch: vi.fn(),
		mockDelete: vi.fn(),
		mockPut: vi.fn(),
	}),
);

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			post: mockPost,
			patch: mockPatch,
			delete: mockDelete,
			put: mockPut,
		},
	},
}));

import {
	addProjectMember,
	createProject,
	createProjectRole,
	deleteProject,
	deleteProjectRole,
	findEpicType,
	getNormalTaskTypes,
	getProject,
	isEpicType,
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
	projectsInfiniteQueryOptions,
	projectsLookupInfiniteQueryOptions,
	projectsQueryOptions,
	removeProjectMember,
	setDefaultTaskType,
	type TaskType,
	taskTypesQueryOptions,
	updateProject,
	updateProjectMemberRole,
	updateProjectRole,
} from "./project-api";

// ── Fixtures ──────────────────────────────────────────────────────────────────

const mockProject: Project = {
	id: "p1",
	name: "Alpha",
	description: "First project",
	is_public: false,
	task_id_prefix: "ALPH",
	settings: {},
	jev_configured: false,
	jev_base_url: "",
	jev_model: "",
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
	description: "",
};

const mockRole: ProjectRole = {
	id: "r1",
	project_id: "p1",
	role_name: "Developer",
	permissions: { "tasks.*": true },
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

const mockTaskType: TaskType = {
	id: "tt1",
	project_id: "p1",
	name: "Task",
	is_default: true,
	is_system: false,
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

	it("createProject forwards is_public when provided", async () => {
		const publicProject = { ...mockProject, is_public: true };
		mockPost.mockResolvedValue(ok(publicProject));

		await createProject({ name: "Public", is_public: true });
		expect(mockPost).toHaveBeenCalledWith("/projects", {
			name: "Public",
			is_public: true,
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

	it("updateProject forwards is_public when toggling visibility", async () => {
		const publicProject = { ...mockProject, is_public: true };
		mockPatch.mockResolvedValue(ok(publicProject));

		await expect(updateProject("p1", { is_public: true })).resolves.toEqual(
			publicProject,
		);
		expect(mockPatch).toHaveBeenCalledWith("/projects/p1", { is_public: true });
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

		it("projectsInfiniteQueryOptions exposes correct key and initial page param", () => {
			const opts = projectsInfiniteQueryOptions();
			expect(opts.queryKey).toEqual(["projects", "all"]);
			expect(opts.initialPageParam).toBe(1);
			expect(typeof opts.queryFn).toBe("function");
		});

		it("projectsInfiniteQueryOptions.getNextPageParam advances while more pages remain", () => {
			const opts = projectsInfiniteQueryOptions();
			const lastPage: ProjectListResult = {
				items: [],
				total: 120,
				page: 1,
				page_size: 50,
			};
			expect(opts.getNextPageParam?.(lastPage, [], 1, [])).toBe(2);
		});

		it("projectsInfiniteQueryOptions.getNextPageParam stops once every project is loaded", () => {
			const opts = projectsInfiniteQueryOptions();
			const lastPage: ProjectListResult = {
				items: [],
				total: 120,
				page: 3,
				page_size: 50,
			};
			expect(opts.getNextPageParam?.(lastPage, [], 3, [])).toBeUndefined();
		});

		// projectsLookupInfiniteQueryOptions must use a cache key distinct from
		// projectsInfiniteQueryOptions' ["projects", "all"] — they share a
		// QueryClient with consumers that page through ["projects", "all"] at
		// their own pace (the home grid's "Load more" button, the sidebar
		// switcher's scroll). A consumer that eagerly drains every page in the
		// background (assigned-tasks-list.tsx) must do so under its own key,
		// or it silently empties those consumers' pages before the user ever
		// sees a "Load more" button to click.
		it("projectsLookupInfiniteQueryOptions uses a cache key distinct from projectsInfiniteQueryOptions", () => {
			const browsable = projectsInfiniteQueryOptions();
			const lookup = projectsLookupInfiniteQueryOptions();
			expect(lookup.queryKey).not.toEqual(browsable.queryKey);
			expect(lookup.queryKey).toEqual(["projects", "all", "lookup"]);
		});

		it("projectsLookupInfiniteQueryOptions pages the same way as projectsInfiniteQueryOptions", () => {
			const opts = projectsLookupInfiniteQueryOptions();
			expect(opts.initialPageParam).toBe(1);
			expect(typeof opts.queryFn).toBe("function");
			const lastPage: ProjectListResult = {
				items: [],
				total: 120,
				page: 1,
				page_size: 50,
			};
			expect(opts.getNextPageParam?.(lastPage, [], 1, [])).toBe(2);
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

		it("taskTypesQueryOptions exposes correct key and fn", () => {
			const opts = taskTypesQueryOptions("p1");
			expect(opts.queryKey).toEqual(["projects", "p1", "task-types"]);
			expect(typeof opts.queryFn).toBe("function");
		});
	});

	// ── Task types ─────────────────────────────────────────────────────────────

	describe("setDefaultTaskType", () => {
		it("sends PUT to the correct URL and unwraps the updated task type", async () => {
			const updated = { ...mockTaskType, is_default: true };
			mockPut.mockResolvedValue(ok(updated));

			await expect(setDefaultTaskType("p1", "tt1")).resolves.toEqual(updated);
			expect(mockPut).toHaveBeenCalledWith(
				"/projects/p1/task-types/tt1/set-default",
			);
		});
	});

	// ── Task type role helpers ─────────────────────────────────────────────────

	describe("isEpicType", () => {
		it("returns true for a system type named Epic", () => {
			const epic: TaskType = {
				...mockTaskType,
				name: "Epic",
				is_system: true,
			};
			expect(isEpicType(epic)).toBe(true);
		});

		it("returns false when is_system is false even if name is Epic", () => {
			const nonSystem: TaskType = {
				...mockTaskType,
				name: "Epic",
				is_system: false,
			};
			expect(isEpicType(nonSystem)).toBe(false);
		});

		it("returns false for a system type with a different name", () => {
			const subtask: TaskType = {
				...mockTaskType,
				name: "Subtask",
				is_system: true,
			};
			expect(isEpicType(subtask)).toBe(false);
		});

		it("returns false for null or undefined", () => {
			expect(isEpicType(null)).toBe(false);
			expect(isEpicType(undefined)).toBe(false);
		});
	});

	describe("findEpicType / getNormalTaskTypes", () => {
		const epicType: TaskType = {
			...mockTaskType,
			id: "tt-epic",
			name: "Epic",
			is_system: true,
		};
		const normalType: TaskType = {
			...mockTaskType,
			id: "tt-task",
			name: "Task",
			is_system: false,
		};
		const types = [epicType, normalType];

		it("findEpicType returns the Epic system type", () => {
			expect(findEpicType(types)).toEqual(epicType);
		});

		it("findEpicType returns undefined when no Epic system type exists", () => {
			expect(findEpicType([normalType])).toBeUndefined();
		});

		it("getNormalTaskTypes returns only non-epic types", () => {
			expect(getNormalTaskTypes(types)).toEqual([normalType]);
		});

		it("getNormalTaskTypes returns empty array when all types are Epic", () => {
			expect(getNormalTaskTypes([epicType])).toEqual([]);
		});
	});
});
