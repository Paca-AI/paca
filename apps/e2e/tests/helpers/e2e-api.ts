import {
	type APIRequest,
	type APIRequestContext,
	expect,
	type Page,
} from "@playwright/test";

type Playwright = { request: Pick<APIRequest, "newContext"> };

export const BASE_URL = process.env.E2E_BASE_URL ?? "http://localhost";
export const API_URL = `${BASE_URL}/api/v1`;
export const USERNAME = process.env.E2E_USERNAME ?? "admin";
export const PASSWORD = process.env.E2E_PASSWORD ?? "e2e-admin-password";

const TEMP_PASSWORD = "TempPassword123!";
export const RESTRICTED_PASSWORD = "E2eRestricted123!";

export function newRunId(): string {
	return Date.now().toString(36).slice(-5).toUpperCase();
}

export async function authRequest(request: APIRequestContext): Promise<void> {
	const response = await request.post(`${API_URL}/auth/login`, {
		data: { username: USERNAME, password: PASSWORD, rememberMe: false },
	});
	expect(response.ok()).toBeTruthy();
}

/**
 * Waits for the login form after a navigation. The app shell occasionally boots
 * to a blank page while the API is busy with other workers; one reload fixes it.
 */
export async function ensureLoginForm(page: Page): Promise<void> {
	try {
		await page
			.getByRole("textbox", { name: "Username" })
			.waitFor({ state: "visible", timeout: 10_000 });
	} catch {
		await page.reload();
	}
}

export async function signIn(
	page: Page,
	username = USERNAME,
	password = PASSWORD,
): Promise<void> {
	await page.goto(`${BASE_URL}/`);
	await ensureLoginForm(page);
	await page.getByRole("textbox", { name: "Username" }).fill(username);
	await page.getByRole("textbox", { name: "Password" }).fill(password);
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(
		page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
	).toBeVisible();
}

async function listAllPages<T>(
	request: APIRequestContext,
	path: string,
): Promise<T[]> {
	const all: T[] = [];
	for (let page = 1; ; page++) {
		const response = await request.get(
			`${API_URL}${path}?page=${page}&page_size=100`,
		);
		if (!response.ok()) break;
		const body = await response.json();
		const items: T[] = body?.data?.items ?? [];
		if (items.length === 0) break;
		all.push(...items);
		const {
			page: current,
			page_size,
			total,
		} = body.data as {
			page: number;
			page_size: number;
			total: number;
		};
		if (current * page_size >= total) break;
	}
	return all;
}

// ─── Projects ────────────────────────────────────────────────────────────────

export async function cleanupProjectsByPrefix(
	request: APIRequestContext,
	prefix: string,
): Promise<void> {
	await authRequest(request);
	const projects = await listAllPages<{ id: string; name: string }>(
		request,
		"/projects",
	);
	await Promise.all(
		projects
			.filter((p) => p.name.startsWith(prefix))
			.map((p) => request.delete(`${API_URL}/projects/${p.id}`)),
	);
}

export async function createProject(
	request: APIRequestContext,
	name: string,
): Promise<string> {
	const response = await request.post(`${API_URL}/projects`, {
		data: { name },
	});
	expect(response.ok()).toBeTruthy();
	const body = await response.json();
	return body.data.id as string;
}

export async function firstProjectRoleId(
	request: APIRequestContext,
	projectId: string,
): Promise<string> {
	const response = await request.get(`${API_URL}/projects/${projectId}/roles`);
	expect(response.ok()).toBeTruthy();
	const body = await response.json();
	const roles: Array<{ id: string }> = body.data ?? [];
	expect(roles.length).toBeGreaterThan(0);
	return roles[0].id;
}

// ─── Users / roles ───────────────────────────────────────────────────────────

/**
 * Sets a user's global role by name. The API assigns roles by id, on their own
 * endpoint (`global_roles.assign`) — creating or editing a user never carries
 * one — so this looks the id up first.
 */
export async function assignGlobalRole(
	request: APIRequestContext,
	userId: string,
	roleName: string,
): Promise<void> {
	const list = await request.get(`${API_URL}/admin/global-roles`);
	expect(list.ok()).toBeTruthy();
	const roles: Array<{ id: string; name: string }> = (await list.json()).data ?? [];
	const role = roles.find((r) => r.name === roleName);
	expect(role, `global role ${roleName} exists`).toBeTruthy();

	const assigned = await request.put(`${API_URL}/admin/users/${userId}/global-roles`, {
		data: { role_ids: [role?.id] },
	});
	expect(assigned.ok()).toBeTruthy();
}

async function createUserWithPassword(
	request: APIRequestContext,
	playwright: Playwright,
	user: { username: string; fullName: string; role?: string },
): Promise<string> {
	const created = await request.post(`${API_URL}/admin/users`, {
		data: {
			username: user.username,
			full_name: user.fullName,
			password: TEMP_PASSWORD,
		},
	});
	expect(created.ok()).toBeTruthy();
	const userId = (await created.json()).data.id as string;
	if (user.role && user.role !== "USER") {
		await assignGlobalRole(request, userId, user.role);
	}

	// A freshly created account must change its temporary password before
	// it can do anything else; do that over the API so UI tests can sign in
	// directly with RESTRICTED_PASSWORD.
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

export async function cleanupUsersByPrefix(
	request: APIRequestContext,
	prefix: string,
): Promise<void> {
	await authRequest(request);
	const users = await listAllPages<{ id: string; username: string }>(
		request,
		"/admin/users",
	);
	await Promise.all(
		users
			.filter((u) => u.username.startsWith(prefix))
			.map((u) => request.delete(`${API_URL}/admin/users/${u.id}`)),
	);
}

export async function cleanupGlobalRolesByPrefix(
	request: APIRequestContext,
	prefix: string,
): Promise<void> {
	await authRequest(request);
	const response = await request.get(`${API_URL}/admin/global-roles`);
	if (!response.ok()) return;
	const roles: Array<{ id: string; name: string }> =
		(await response.json()).data ?? [];
	await Promise.all(
		roles
			.filter((r) => r.name.startsWith(prefix))
			.map((r) => request.delete(`${API_URL}/admin/global-roles/${r.id}`)),
	);
}

/** Creates a user whose only global role grants exactly `permissions`. */
export async function createUserWithGlobalPermissions(
	request: APIRequestContext,
	playwright: Playwright,
	opts: {
		username: string;
		roleName: string;
		permissions: Record<string, boolean>;
	},
): Promise<void> {
	const role = await request.post(`${API_URL}/admin/global-roles`, {
		data: { name: opts.roleName, permissions: opts.permissions },
	});
	expect(role.ok()).toBeTruthy();
	await createUserWithPassword(request, playwright, {
		username: opts.username,
		fullName: opts.username,
		role: opts.roleName,
	});
}

/** Creates a user who is a member of `projectId` with exactly `permissions`. */
export async function createUserWithProjectPermissions(
	request: APIRequestContext,
	playwright: Playwright,
	opts: {
		projectId: string;
		username: string;
		roleName: string;
		permissions: Record<string, boolean>;
	},
): Promise<void> {
	const userId = await createUserWithPassword(request, playwright, {
		username: opts.username,
		fullName: opts.username,
	});
	const role = await request.post(
		`${API_URL}/projects/${opts.projectId}/roles`,
		{ data: { role_name: opts.roleName, permissions: opts.permissions } },
	);
	expect(role.ok()).toBeTruthy();
	const roleId = (await role.json()).data.id as string;
	const member = await request.post(
		`${API_URL}/projects/${opts.projectId}/members`,
		{ data: { user_id: userId, project_role_id: roleId } },
	);
	expect(member.ok()).toBeTruthy();
}

// ─── Agents ──────────────────────────────────────────────────────────────────

export interface SeededAgent {
	id: string;
	name: string;
	handle: string;
}

export function handleFor(name: string): string {
	return name
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, "-")
		.replace(/^-+|-+$/g, "");
}

function agentTypeFields(
	kind: "llm" | "acp",
	opts: { llmProvider?: string; llmModel?: string; acpProvider?: string },
) {
	return kind === "llm"
		? {
				agent_type: "llm",
				llm_provider: opts.llmProvider ?? "anthropic",
				llm_model: opts.llmModel ?? "claude-sonnet-4-6",
				llm_api_key: "sk-ant-e2e-placeholder-key",
				llm_base_url: "https://api.anthropic.com",
			}
		: {
				agent_type: "acp",
				acp_provider: opts.acpProvider ?? "claude-code",
			};
}

export async function createProjectAgent(
	request: APIRequestContext,
	projectId: string,
	name: string,
	kind: "llm" | "acp" = "llm",
	opts: { llmProvider?: string; llmModel?: string; acpProvider?: string } = {},
): Promise<SeededAgent> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/agents`,
		{
			data: {
				name,
				handle: handleFor(name),
				project_role_id: await firstProjectRoleId(request, projectId),
				...agentTypeFields(kind, opts),
			},
		},
	);
	expect(response.ok()).toBeTruthy();
	const agent = (await response.json()).data;
	return { id: agent.id, name, handle: agent.handle };
}

export async function createGlobalAgent(
	request: APIRequestContext,
	name: string,
): Promise<SeededAgent> {
	const response = await request.post(`${API_URL}/admin/agents`, {
		data: { name, handle: handleFor(name), ...agentTypeFields("llm", {}) },
	});
	expect(response.ok()).toBeTruthy();
	const agent = (await response.json()).data;
	return { id: agent.id, name, handle: agent.handle };
}

export async function listGlobalAgents(
	request: APIRequestContext,
): Promise<Array<{ id: string; name: string }>> {
	const response = await request.get(`${API_URL}/admin/agents`);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.items ?? [];
}

export async function cleanupGlobalAgentsByPrefix(
	request: APIRequestContext,
	prefix: string,
): Promise<void> {
	await authRequest(request);
	const agents = await listGlobalAgents(request);
	await Promise.all(
		agents
			.filter((a) => a.name.startsWith(prefix))
			.map((a) => request.delete(`${API_URL}/admin/agents/${a.id}`)),
	);
}

// ─── Conversations ───────────────────────────────────────────────────────────

export interface SeededConversation {
	id: string;
	chatSessionId: string;
}

const TERMINAL_CONVERSATION_STATUSES = ["finished", "failed", "stopped"];

export async function startProjectChat(
	request: APIRequestContext,
	projectId: string,
	agentId: string,
	message: string,
): Promise<SeededConversation> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/agents/${agentId}/chat-sessions`,
		{ data: { message } },
	);
	expect(response.ok()).toBeTruthy();
	const { conversation, session } = (await response.json()).data;
	return { id: conversation.id, chatSessionId: session.id };
}

export async function startGlobalChat(
	request: APIRequestContext,
	agentId: string,
	message: string,
): Promise<SeededConversation> {
	const response = await request.post(
		`${API_URL}/agents/${agentId}/chat-sessions`,
		{ data: { message } },
	);
	expect(response.ok()).toBeTruthy();
	const { conversation, session } = (await response.json()).data;
	return { id: conversation.id, chatSessionId: session.id };
}

/**
 * Polls a conversation until it reaches a terminal status. The e2e stack has
 * no valid LLM credentials, so every seeded chat conversation ends `failed`
 * within a few seconds ("acp session/prompt: Authentication required").
 */
export async function waitForConversationTerminal(
	request: APIRequestContext,
	conversationPath: string,
): Promise<string> {
	let status = "";
	await expect
		.poll(
			async () => {
				const response = await request.get(`${API_URL}${conversationPath}`);
				status = (await response.json()).data?.status ?? "";
				return TERMINAL_CONVERSATION_STATUSES.includes(status);
			},
			{ timeout: 90_000, intervals: [1_000, 2_000] },
		)
		.toBe(true);
	return status;
}

export async function listProjectConversationIds(
	request: APIRequestContext,
	projectId: string,
): Promise<string[]> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/conversations?page_size=100`,
	);
	expect(response.ok()).toBeTruthy();
	const items: Array<{ id: string }> = (await response.json()).data.items ?? [];
	return items.map((c) => c.id);
}

/** Soft-deletes every global conversation the admin has with an agent whose name starts with `agentPrefix`. */
export async function cleanupGlobalConversationsByAgentPrefix(
	request: APIRequestContext,
	agentPrefix: string,
): Promise<void> {
	await authRequest(request);
	const agentsResponse = await request.get(`${API_URL}/agents`);
	if (!agentsResponse.ok()) return;
	const agents: Array<{ id: string; name: string }> =
		(await agentsResponse.json()).data.items ?? [];
	const agentIds = new Set(
		agents.filter((a) => a.name.startsWith(agentPrefix)).map((a) => a.id),
	);
	if (agentIds.size === 0) return;

	let cursor: string | null = null;
	do {
		const url: string = `${API_URL}/agents/conversations?page_size=100${
			cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""
		}`;
		const response = await request.get(url);
		if (!response.ok()) return;
		const data: {
			items?: Array<{ id: string; agent_id: string }>;
			next_cursor?: string | null;
		} = (await response.json()).data;
		await Promise.all(
			(data.items ?? [])
				.filter((c) => agentIds.has(c.agent_id))
				.map((c) => request.delete(`${API_URL}/agents/conversations/${c.id}`)),
		);
		cursor = data.next_cursor ?? null;
	} while (cursor);
}
