import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPost, mockPatch, mockPut, mockDelete } = vi.hoisted(
	() => ({
		mockGet: vi.fn(),
		mockPost: vi.fn(),
		mockPatch: vi.fn(),
		mockPut: vi.fn(),
		mockDelete: vi.fn(),
	}),
);

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			post: mockPost,
			patch: mockPatch,
			put: mockPut,
			delete: mockDelete,
		},
	},
}));

import {
	createUser,
	deleteUser,
	getMyGlobalPermissions,
	getUsers,
	getUsersByCursor,
	myPermissionsQueryOptions,
	resetUserPassword,
	type User,
	updateUser,
	usersInfiniteQueryOptions,
	usersQueryOptions,
} from "./admin-api";

describe("admin-api", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("unwraps global permissions list from response", async () => {
		mockGet.mockResolvedValue({
			data: {
				data: { actions: ["users:read", "projects:*"] },
				error_code: null,
				message: "ok",
			},
		});

		await expect(getMyGlobalPermissions()).resolves.toEqual([
			"users:read",
			"projects:*",
		]);
		expect(mockGet).toHaveBeenCalledWith("/users/me/global-permissions");
	});

	it("exposes query option contracts", () => {
		expect(myPermissionsQueryOptions.queryKey).toEqual([
			"auth",
			"me",
			"permissions",
		]);
		expect(typeof myPermissionsQueryOptions.queryFn).toBe("function");
		expect(myPermissionsQueryOptions.staleTime).toBe(5 * 60 * 1000);
		expect(myPermissionsQueryOptions.retry).toBe(false);
	});

	// Users API
	// -----------------------------------------------------------------------

	const mockUser: User = {
		id: "u1",
		username: "alice",
		full_name: "Alice Smith",
		roles: [{ id: "r1", name: "Admin" }],
		must_change_password: false,
		created_at: "2026-01-01T00:00:00.000Z",
	};

	it("fetches paged users", async () => {
		const response = {
			items: [mockUser],
			total: 1,
			page: 1,
			page_size: 20,
			must_change_password_count: 0,
		};
		mockGet.mockResolvedValue({
			data: { data: response, error_code: null, message: "ok" },
		});

		await expect(getUsers(1, 20)).resolves.toEqual(response);
		expect(mockGet).toHaveBeenCalledWith("/admin/users", {
			params: { page: 1, page_size: 20 },
		});
	});

	it("uses default page and page_size for getUsers", async () => {
		mockGet.mockResolvedValue({
			data: {
				data: {
					items: [],
					total: 0,
					page: 1,
					page_size: 20,
					must_change_password_count: 0,
				},
				error_code: null,
				message: "ok",
			},
		});

		await getUsers();
		expect(mockGet).toHaveBeenCalledWith("/admin/users", {
			params: { page: 1, page_size: 20 },
		});
	});

	it("sends cursor, search and page_size to getUsersByCursor only when set", async () => {
		const page = { items: [], page_size: 20, next_cursor: "c2" };
		mockGet.mockResolvedValue({
			data: { data: page, error_code: null, message: "ok" },
		});

		await expect(getUsersByCursor(null)).resolves.toEqual(page);
		expect(mockGet).toHaveBeenLastCalledWith("/admin/users/cursor", {
			params: { page_size: 20 },
		});

		await getUsersByCursor("c1", 5, { search: "alice" });
		expect(mockGet).toHaveBeenLastCalledWith("/admin/users/cursor", {
			params: { page_size: 5, cursor: "c1", search: "alice" },
		});
	});

	it("usersInfiniteQueryOptions follows next_cursor and stops when null", () => {
		const opts = usersInfiniteQueryOptions("al");
		expect(opts.queryKey).toEqual(["admin", "users", "cursor", "al"]);
		expect(opts.initialPageParam).toBeNull();
		const next = opts.getNextPageParam as (p: unknown) => unknown;
		expect(next({ items: [], page_size: 20, next_cursor: "abc" })).toBe("abc");
		expect(
			next({ items: [], page_size: 20, next_cursor: null }),
		).toBeUndefined();
	});

	it("sends search and role to getUsers only when set", async () => {
		mockGet.mockResolvedValue({
			data: {
				data: {
					items: [],
					total: 0,
					page: 1,
					page_size: 20,
					must_change_password_count: 0,
				},
				error_code: null,
				message: "ok",
			},
		});

		await getUsers(2, 20, { search: "alice", role: "ADMIN" });
		expect(mockGet).toHaveBeenCalledWith("/admin/users", {
			params: { page: 2, page_size: 20, search: "alice", role: "ADMIN" },
		});
		expect(usersQueryOptions(1, 20, { search: "alice" }).queryKey).toEqual([
			"admin",
			"users",
			1,
			20,
			{ search: "alice", role: "" },
		]);
	});

	it("posts payload to create user and unwraps response", async () => {
		mockPost.mockResolvedValue({
			data: { data: mockUser, error_code: null, message: "ok" },
		});

		// No role: assigning one is a separate call (assignUserGlobalRole).
		const payload = {
			username: "alice",
			password: "P@ssw0rd!",
			full_name: "Alice Smith",
		};

		await expect(createUser(payload)).resolves.toEqual(mockUser);
		expect(mockPost).toHaveBeenCalledWith("/admin/users", payload);
	});

	it("patches user by id and unwraps response", async () => {
		const updated = { ...mockUser, full_name: "Alice J. Smith" };
		mockPatch.mockResolvedValue({
			data: { data: updated, error_code: null, message: "ok" },
		});

		await expect(
			updateUser("u1", { full_name: "Alice J. Smith" }),
		).resolves.toEqual(updated);
		expect(mockPatch).toHaveBeenCalledWith("/admin/users/u1", {
			full_name: "Alice J. Smith",
		});
	});

	it("deletes user by id", async () => {
		mockDelete.mockResolvedValue({});

		await expect(deleteUser("u1")).resolves.toBeUndefined();
		expect(mockDelete).toHaveBeenCalledWith("/admin/users/u1");
	});

	it("patches user password by id", async () => {
		mockPatch.mockResolvedValue({});

		await expect(resetUserPassword("u1", "NewP@ss1!")).resolves.toBeUndefined();
		expect(mockPatch).toHaveBeenCalledWith("/admin/users/u1/password", {
			new_password: "NewP@ss1!",
		});
	});

	it("exposes usersQueryOptions query key contract", () => {
		const opts = usersQueryOptions(2, 10);
		expect(opts.queryKey).toEqual(["admin", "users", 2, 10]);
		expect(typeof opts.queryFn).toBe("function");
	});

	it("usersQueryOptions defaults to page 1 and page_size 20", () => {
		const opts = usersQueryOptions();
		expect(opts.queryKey).toEqual(["admin", "users", 1, 20]);
	});
});
