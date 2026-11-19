import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

/** A browser or device the signed-in account is logged in on. */
export interface Session {
	id: string;
	/** True for the session making the request. */
	current: boolean;
	/** Read from the User-Agent; either may be empty. */
	browser: string;
	os: string;
	ip: string;
	remember_me: boolean;
	created_at: string;
	last_active_at: string;
}

export const sessionsQueryOptions = queryOptions({
	queryKey: ["auth", "sessions"],
	queryFn: async () => {
		const { data } =
			await apiClient.instance.get<SuccessEnvelope<Session[]>>(
				"/users/me/sessions",
			);
		return data.data;
	},
});

/** Signs one device out: its tokens stop working immediately. */
export async function revokeSession(id: string): Promise<void> {
	await apiClient.instance.delete(`/users/me/sessions/${id}`);
}

/** Signs out every device but this one and returns how many it ended. */
export async function revokeOtherSessions(): Promise<number> {
	const { data } =
		await apiClient.instance.delete<SuccessEnvelope<{ revoked: number }>>(
			"/users/me/sessions",
		);
	return data.data.revoked;
}
