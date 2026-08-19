import { queryOptions } from "@tanstack/react-query";
import axios from "axios";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

/** Shape of the authenticated user returned by GET /users/me. */
export interface User {
	id: string;
	username: string;
	full_name: string;
	email?: string | null;
	role: string;
	must_change_password: boolean;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
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

/**
 * Sets an account's password using a single-use token — the public,
 * unauthenticated flow a password-set-link email points to (see the
 * plugin-triggered welcome/invite email). The token proves the caller's
 * right to act on the account instead of a session or current password.
 */
export async function setPasswordWithToken(
	token: string,
	newPassword: string,
): Promise<void> {
	await apiClient.instance.post("/auth/password/set", {
		token,
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

/**
 * Resolves to `null` on a definitive 401 (not authenticated) instead of
 * throwing, so every consumer shares one query cache entry — a route
 * `beforeLoad` requiring a signed-in user, and a component just wanting to
 * know who's logged in, both read the exact same cached "auth","me" result.
 * Splitting those into two cache keys used to mean loading a page fired
 * /users/me twice: once from the route's beforeLoad guard, once again from
 * whichever component read the other key. Any other error (network,
 * timeouts) re-throws so React Query keeps the last good data instead of
 * overwriting it with null.
 */
export async function getMe(): Promise<User | null> {
	try {
		const { data } =
			await apiClient.instance.get<SuccessEnvelope<User>>("/users/me");
		return data.data;
	} catch (err) {
		if (axios.isAxiosError(err) && err.response?.status === 401) {
			return null;
		}
		throw err;
	}
}

export const currentUserQueryOptions = queryOptions({
	queryKey: ["auth", "me"],
	queryFn: getMe,
	retry: false,
	staleTime: 5 * 60 * 1000,
});
