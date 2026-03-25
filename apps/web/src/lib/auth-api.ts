import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";

/** Shape of the authenticated user returned by GET /users/me. */
export interface User {
	id: string;
	username: string;
	full_name: string;
	role: string;
	created_at: string;
}

/** API envelope wrapper used by the backend presenter. */
interface Envelope<T> {
	success: boolean;
	data: T;
	error_code?: string;
	request_id?: string;
}

export async function login(username: string, password: string): Promise<void> {
	await apiClient.instance.post("/auth/login", { username, password });
}

export async function logout(): Promise<void> {
	await apiClient.instance.post("/auth/logout");
}

export async function getMe(): Promise<User> {
	const { data } = await apiClient.instance.get<Envelope<User>>("/users/me");
	return data.data;
}

export const currentUserQueryOptions = queryOptions({
	queryKey: ["auth", "me"],
	queryFn: getMe,
	retry: false,
	staleTime: 5 * 60 * 1000,
});
