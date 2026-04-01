import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

/** Shape of the authenticated user returned by GET /users/me. */
export interface User {
	id: string;
	username: string;
	full_name: string;
	role: string;
	must_change_password: boolean;
	created_at: string;
}

export async function changeMyPassword(
	currentPassword: string,
	newPassword: string,
): Promise<void> {
	await apiClient.instance.patch("/users/me/password", {
		current_password: currentPassword,
		new_password: newPassword,
	});
}

export async function login(
	username: string,
	password: string,
	rememberMe: boolean,
): Promise<void> {
	await apiClient.instance.post("/auth/login", {
		username,
		password,
		remember_me: rememberMe,
	});
}

export async function logout(): Promise<void> {
	await apiClient.instance.post("/auth/logout");
}

export async function getMe(): Promise<User> {
	const { data } =
		await apiClient.instance.get<SuccessEnvelope<User>>("/users/me");
	return data.data;
}

export const currentUserQueryOptions = queryOptions({
	queryKey: ["auth", "me"],
	queryFn: getMe,
	retry: false,
	staleTime: 5 * 60 * 1000,
});
