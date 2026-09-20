import {
	type APIRequest,
	type APIRequestContext,
	expect,
} from "@playwright/test";
import { API_URL, RESTRICTED_PASSWORD } from "./e2e-api";

type PlaywrightLike = { request: Pick<APIRequest, "newContext"> };

const TEMP_PASSWORD = "TempPassword123!";

/**
 * Creates a throwaway non-admin user (optionally with an email), clears the
 * forced first-login password change over the API, and returns the user id.
 * The account can then sign in with RESTRICTED_PASSWORD.
 *
 * `request` must already be authenticated as the admin.
 */
export async function createUserWithEmail(
	request: APIRequestContext,
	playwright: PlaywrightLike,
	user: { username: string; fullName: string; email?: string },
): Promise<string> {
	const created = await request.post(`${API_URL}/admin/users`, {
		data: {
			username: user.username,
			full_name: user.fullName,
			password: TEMP_PASSWORD,
			...(user.email ? { email: user.email } : {}),
		},
	});
	expect(created.ok()).toBeTruthy();
	const userId = (await created.json()).data.id as string;

	const userContext = await playwright.request.newContext();
	try {
		const login = await userContext.post(`${API_URL}/auth/login`, {
			data: {
				username: user.username,
				password: TEMP_PASSWORD,
				rememberMe: false,
			},
		});
		expect(login.ok()).toBeTruthy();
		const changed = await userContext.patch(`${API_URL}/users/me/password`, {
			data: {
				current_password: TEMP_PASSWORD,
				new_password: RESTRICTED_PASSWORD,
			},
		});
		expect(changed.ok()).toBeTruthy();
	} finally {
		await userContext.dispose();
	}
	return userId;
}

/** Returns an API context logged in as `username`. Caller must dispose it. */
export async function loginContext(
	playwright: PlaywrightLike,
	username: string,
	password = RESTRICTED_PASSWORD,
): Promise<APIRequestContext> {
	const context = await playwright.request.newContext();
	const login = await context.post(`${API_URL}/auth/login`, {
		data: { username, password, rememberMe: false },
	});
	expect(login.ok()).toBeTruthy();
	return context;
}
