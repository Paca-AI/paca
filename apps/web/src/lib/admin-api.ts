import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";
import type { RoleSummary } from "./role-api";

/** The IAM actions the caller holds at platform level. */
export async function getMyGlobalPermissions(): Promise<string[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ actions: string[] }>
	>("/users/me/global-permissions");
	return data.data.actions;
}

export const myPermissionsQueryOptions = queryOptions({
	queryKey: ["auth", "me", "permissions"],
	queryFn: getMyGlobalPermissions,
	staleTime: 5 * 60 * 1000,
	retry: false,
});

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

export interface User {
	id: string;
	username: string;
	full_name: string;
	email?: string | null;
	/** The platform roles the user holds, sorted by name. */
	roles: RoleSummary[];
	must_change_password: boolean;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
	created_at: string;
}

export interface PagedUsersResponse {
	items: User[];
	total: number;
	page: number;
	page_size: number;
	/** Count across ALL users, not just `items` (the current page) — safe to
	 *  display regardless of which page is showing. */
	must_change_password_count: number;
}

export interface UsersFilter {
	/** Every whitespace-separated word must appear in the username, full name
	 *  or email (case-insensitive). */
	search?: string;
	/** Exact global role name. */
	role?: string;
}

export async function getUsers(
	page = 1,
	pageSize = 20,
	filter: UsersFilter = {},
): Promise<PagedUsersResponse> {
	const params: Record<string, string | number> = {
		page,
		page_size: pageSize,
	};
	if (filter.search) params.search = filter.search;
	if (filter.role) params.role = filter.role;
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<PagedUsersResponse>
	>("/admin/users", { params });
	return data.data;
}

export interface CursorUsersResponse {
	items: User[];
	page_size: number;
	/** Opaque token for the next page; null on the last page. */
	next_cursor: string | null;
}

export async function getUsersByCursor(
	cursor: string | null,
	pageSize = 20,
	filter: UsersFilter = {},
): Promise<CursorUsersResponse> {
	const params: Record<string, string | number> = { page_size: pageSize };
	if (cursor) params.cursor = cursor;
	if (filter.search) params.search = filter.search;
	if (filter.role) params.role = filter.role;
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<CursorUsersResponse>
	>("/admin/users/cursor", { params });
	return data.data;
}

/** Creates a user with the default USER role. To give them another role, call
 *  {@link replaceUserRoles} afterwards — assigning a role needs
 *  `roles:assign`, so the server no longer accepts `role` here. */
export async function createUser(payload: {
	username: string;
	password: string;
	full_name: string;
	email?: string;
}): Promise<User> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<User>>(
		"/admin/users",
		payload,
	);
	return data.data;
}

/** Edits a user's profile. The role is not part of it: change that with
 *  {@link replaceUserRoles}. */
export async function updateUser(
	userId: string,
	payload: { full_name?: string; email?: string },
): Promise<User> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<User>>(
		`/admin/users/${userId}`,
		payload,
	);
	return data.data;
}

export async function deleteUser(userId: string): Promise<void> {
	await apiClient.instance.delete(`/admin/users/${userId}`);
}

export async function resetUserPassword(
	userId: string,
	newPassword: string,
): Promise<void> {
	await apiClient.instance.patch(`/admin/users/${userId}/password`, {
		new_password: newPassword,
	});
}

export function usersQueryOptions(
	page = 1,
	pageSize = 20,
	filter: UsersFilter = {},
) {
	return queryOptions({
		queryKey: [
			"admin",
			"users",
			page,
			pageSize,
			...(filter.search || filter.role
				? [{ search: filter.search ?? "", role: filter.role ?? "" }]
				: []),
		],
		queryFn: () => getUsers(page, pageSize, filter),
	});
}

export const ADMIN_USERS_PAGE_SIZE = 20;

/** Cursor-paginated infinite query over the user list — backs pickers that
 *  page through every user (e.g. the "add team member" dialog). `search` is
 *  sent to the server (it matches username, full name and email) so users
 *  beyond the first page are still findable. */
export const usersInfiniteQueryOptions = (search = "") =>
	infiniteQueryOptions({
		queryKey: ["admin", "users", "cursor", search],
		queryFn: ({ pageParam }: { pageParam: string | null }) =>
			getUsersByCursor(pageParam, ADMIN_USERS_PAGE_SIZE, { search }),
		initialPageParam: null as string | null,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
	});
