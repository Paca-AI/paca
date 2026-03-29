import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

export interface GlobalRole {
	id: string;
	name: string;
	permissions: Record<string, boolean>;
	created_at: string;
	updated_at: string;
}

export async function getGlobalRoles(): Promise<GlobalRole[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<GlobalRole[]>>(
		"/admin/global-roles",
	);
	return data.data;
}

export async function createGlobalRole(payload: {
	name: string;
	permissions: Record<string, boolean>;
}): Promise<GlobalRole> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<GlobalRole>>(
		"/admin/global-roles",
		payload,
	);
	return data.data;
}

export async function updateGlobalRole(
	roleId: string,
	payload: { name: string; permissions: Record<string, boolean> },
): Promise<GlobalRole> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<GlobalRole>>(
		`/admin/global-roles/${roleId}`,
		payload,
	);
	return data.data;
}

export async function deleteGlobalRole(roleId: string): Promise<void> {
	await apiClient.instance.delete(`/admin/global-roles/${roleId}`);
}

export async function getMyGlobalPermissions(): Promise<string[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ permissions: string[] }>
	>("/users/me/global-permissions");
	return data.data.permissions;
}

export const globalRolesQueryOptions = queryOptions({
	queryKey: ["admin", "global-roles"],
	queryFn: getGlobalRoles,
});

export const myPermissionsQueryOptions = queryOptions({
	queryKey: ["auth", "me", "permissions"],
	queryFn: getMyGlobalPermissions,
	staleTime: 5 * 60 * 1000,
	retry: false,
});
