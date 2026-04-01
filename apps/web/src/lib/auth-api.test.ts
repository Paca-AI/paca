import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockPost, mockGet } = vi.hoisted(() => ({
	mockPost: vi.fn(),
	mockGet: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			post: mockPost,
			get: mockGet,
		},
	},
}));

import {
	currentUserQueryOptions,
	getMe,
	login,
	logout,
	type User,
} from "./auth-api";

describe("auth-api", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("calls login endpoint with mapped payload", async () => {
		mockPost.mockResolvedValue({});

		await login("alice", "secret", true);

		expect(mockPost).toHaveBeenCalledWith("/auth/login", {
			username: "alice",
			password: "secret",
			remember_me: true,
		});
	});

	it("calls logout endpoint", async () => {
		mockPost.mockResolvedValue({});

		await logout();

		expect(mockPost).toHaveBeenCalledWith("/auth/logout");
	});

	it("unwraps user payload from getMe response", async () => {
		const user: User = {
			id: "u1",
			username: "alice",
			full_name: "Alice Example",
			role: "admin",
			must_change_password: false,
			created_at: "2026-03-28T10:00:00.000Z",
		};
		mockGet.mockResolvedValue({
			data: {
				data: user,
				error_code: null,
				message: "ok",
			},
		});

		await expect(getMe()).resolves.toEqual(user);
		expect(mockGet).toHaveBeenCalledWith("/users/me");
	});

	it("exposes query options for current user", () => {
		expect(currentUserQueryOptions.queryKey).toEqual(["auth", "me"]);
		expect(currentUserQueryOptions.retry).toBe(false);
		expect(currentUserQueryOptions.staleTime).toBe(5 * 60 * 1000);
		expect(typeof currentUserQueryOptions.queryFn).toBe("function");
	});
});
