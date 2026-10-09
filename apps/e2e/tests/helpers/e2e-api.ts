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

export interface ApiRole {
	id: string;
	name: string;
	description?: string;
	project_id: string | null;
	is_system?: boolean;
	is_default?: boolean;
}

/**
 * The id of the role called `roleName` that belongs to `projectId`. Every
 * project starts with the roles Admin, Editor and Viewer, owned by the
 * project (the project's role list can also show platform roles, which are not
 * what this means).
 */
export async function projectRoleIdByName(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
): Promise<string> {
	const response = await request.get(`${API_URL}/projects/${projectId}/roles`);
	expect(response.ok()).toBeTruthy();
	const roles: ApiRole[] = (await response.json()).data ?? [];
	const role = roles.find(
		(r) => r.project_id === projectId && r.name === roleName,
	);
	expect(role, `project role ${roleName} exists`).toBeTruthy();
	return role?.id ?? "";
}

/** The id of a project's built-in Admin role (the one its creator holds). */
export async function firstProjectRoleId(
	request: APIRequestContext,
	projectId: string,
): Promise<string> {
	return projectRoleIdByName(request, projectId, "Admin");
}

// ─── Users / roles ───────────────────────────────────────────────────────────

export interface PolicyStatement {
	/** Optional label, shown when a request is simulated. */
	sid?: string;
	effect: "Allow" | "Deny";
	actions: string[];
	resources: string[];
	conditions?: Record<string, Record<string, unknown>>;
}

export interface RolePolicy {
	version: string;
	statements: PolicyStatement[];
}

export const POLICY_VERSION = "2026-10-01";

/** The resources the role editor's simple view writes for a workspace role. */
const PLATFORM_RESOURCES = [
	"user",
	"user/*",
	"role",
	"role/*",
	"plugin",
	"plugin/*",
	"settings",
	"sso",
	"agent",
	"agent/*",
	"project",
];

/**
 * The policy a role gets from the editor's simple view when exactly `actions`
 * are switched on: one Allow statement over the workspace resources (or, for a
 * project role, over everything in that project). `*` is full access.
 */
export function allowPolicy(
	actions: string[],
	scope: { projectId?: string } = {},
): RolePolicy {
	if (actions.length === 0) {
		return { version: POLICY_VERSION, statements: [] };
	}
	// A project role may only name resources inside its own project:
	// "project/<projectId>/*" is what the role editor writes for it.
	const resources = scope.projectId
		? [`project/${scope.projectId}/*`]
		: actions.includes("*")
			? ["*"]
			: PLATFORM_RESOURCES;
	return {
		version: POLICY_VERSION,
		statements: [{ effect: "Allow", actions, resources }],
	};
}

/** The actions switched on in a `{ action: boolean }` map. */
export function grantedActions(permissions: Record<string, boolean>): string[] {
	return Object.keys(permissions).filter((key) => permissions[key]);
}

export async function createGlobalRole(
	request: APIRequestContext,
	name: string,
	policy: RolePolicy,
): Promise<ApiRole> {
	const response = await request.post(`${API_URL}/admin/roles`, {
		data: { name, description: "", policy },
	});
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data as ApiRole;
}

export async function createProjectRole(
	request: APIRequestContext,
	projectId: string,
	name: string,
	policy: RolePolicy,
	description = "",
): Promise<ApiRole> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/roles`,
		{ data: { name, description, policy } },
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data as ApiRole;
}

/** The id of the workspace role called `roleName` (built-ins: SUPER_ADMIN, ADMIN, USER). */
export async function globalRoleIdByName(
	request: APIRequestContext,
	roleName: string,
): Promise<string> {
	const list = await request.get(`${API_URL}/admin/roles`);
	expect(list.ok()).toBeTruthy();
	const roles: ApiRole[] = (await list.json()).data ?? [];
	const role = roles.find((r) => r.name === roleName);
	expect(role, `global role ${roleName} exists`).toBeTruthy();
	return role?.id ?? "";
}

/**
 * Sets the workspace roles a user holds, by name. A user holds any number of
 * roles; this replaces the whole set (`roles:assign`), so the user ends up with
 * exactly these.
 */
export async function assignGlobalRoles(
	request: APIRequestContext,
	userId: string,
	roleNames: string[],
): Promise<void> {
	const roleIds = await Promise.all(
		roleNames.map((name) => globalRoleIdByName(request, name)),
	);
	const assigned = await request.put(`${API_URL}/admin/users/${userId}/roles`, {
		data: { role_ids: roleIds },
	});
	expect(assigned.ok()).toBeTruthy();
}

/** Sets a user's one workspace role by name. */
export async function assignGlobalRole(
	request: APIRequestContext,
	userId: string,
	roleName: string,
): Promise<void> {
	await assignGlobalRoles(request, userId, [roleName]);
}

/**
 * Sets the workspace roles a global agent holds, by name, on the agent's own
 * endpoint (`agents:write` + `roles:assign`): creating an agent never carries
 * any, it starts with the default role.
 */
export async function bindGlobalAgentRole(
	request: APIRequestContext,
	agentId: string,
	roleName: string,
): Promise<void> {
	const bound = await request.put(`${API_URL}/admin/agents/${agentId}/roles`, {
		data: { role_ids: [await globalRoleIdByName(request, roleName)] },
	});
	expect(bound.ok()).toBeTruthy();
}

/** Replaces the roles a project member holds in that project. */
export async function replaceMemberRoles(
	request: APIRequestContext,
	projectId: string,
	memberId: string,
	roleIds: string[],
): Promise<void> {
	const response = await request.put(
		`${API_URL}/projects/${projectId}/members/${memberId}/roles`,
		{ data: { role_ids: roleIds } },
	);
	expect(response.ok()).toBeTruthy();
}

/** Adds a user to a project with the given roles; returns the member id. */
export async function addProjectMember(
	request: APIRequestContext,
	projectId: string,
	userId: string,
	roleIds: string[],
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/members`,
		{ data: { user_id: userId, role_ids: roleIds } },
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.id as string;
}

export async function createUserWithPassword(
	request: APIRequestContext,
	playwright: Playwright,
	user: { username: string; fullName: string; roles?: string[] },
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
	if (user.roles && user.roles.length > 0) {
		await assignGlobalRoles(request, userId, user.roles);
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
	const response = await request.get(`${API_URL}/admin/roles`);
	if (!response.ok()) return;
	const roles: Array<{ id: string; name: string }> =
		(await response.json()).data ?? [];
	await Promise.all(
		roles
			.filter((r) => r.name.startsWith(prefix))
			.map((r) => request.delete(`${API_URL}/admin/roles/${r.id}`)),
	);
}

/**
 * Creates a user whose only workspace role grants exactly `permissions`, a map
 * of IAM actions ("users:read", "roles:*") to true.
 */
export async function createUserWithGlobalPermissions(
	request: APIRequestContext,
	playwright: Playwright,
	opts: {
		username: string;
		roleName: string;
		permissions: Record<string, boolean>;
	},
): Promise<void> {
	await createGlobalRole(
		request,
		opts.roleName,
		allowPolicy(grantedActions(opts.permissions)),
	);
	await createUserWithPassword(request, playwright, {
		username: opts.username,
		fullName: opts.username,
		roles: [opts.roleName],
	});
}

/**
 * Creates a user who is a member of `projectId` whose only role there grants
 * `permissions`, a map of IAM actions ("tasks:read", "agents:*") to true, plus
 * projects:read so the member can open the project. Returns the user, member
 * and role ids.
 */
export async function createUserWithProjectPermissions(
	request: APIRequestContext,
	playwright: Playwright,
	opts: {
		projectId: string;
		username: string;
		roleName: string;
		permissions: Record<string, boolean>;
	},
): Promise<{ userId: string; memberId: string; roleId: string }> {
	const userId = await createUserWithPassword(request, playwright, {
		username: opts.username,
		fullName: opts.username,
	});
	const role = await createProjectRole(
		request,
		opts.projectId,
		opts.roleName,
		// Opening the project needs projects:read on it, which only a role can
		// give (the built-in project roles all include it); keep it so that a
		// role "granting exactly" the permissions under test still lets the
		// member in.
		allowPolicy(
			[...new Set(["projects:read", ...grantedActions(opts.permissions)])],
			{ projectId: opts.projectId },
		),
	);
	const memberId = await addProjectMember(request, opts.projectId, userId, [
		role.id,
	]);
	return { userId, memberId, roleId: role.id };
}

/**
 * A policy that denies `actions` on one agent or environment of a project.
 * Restricting a resource is nothing special: it is an ordinary role with a
 * Deny statement, and Deny always wins over whatever other roles allow. The
 * trailing `/*` also covers what hangs off the resource (chat sessions, SSH
 * keys, port forwards).
 */
export function denyResourcePolicy(
	projectId: string,
	kind: "agent" | "environment",
	resourceId: string,
	actions: string[],
): RolePolicy {
	return {
		version: POLICY_VERSION,
		statements: [
			{
				effect: "Deny",
				actions,
				resources: [`project/${projectId}/${kind}/${resourceId}/*`],
			},
		],
	};
}

/**
 * Restricts an agent or environment for one member: creates a project role
 * that denies `actions` on it and adds that role to the member's roles
 * (`keepRoleIds` are the roles they hold already, which a role replacement
 * would otherwise drop).
 */
export async function denyResourceToMember(
	request: APIRequestContext,
	opts: {
		projectId: string;
		memberId: string;
		keepRoleIds: string[];
		roleName: string;
		kind: "agent" | "environment";
		resourceId: string;
		actions: string[];
	},
): Promise<ApiRole> {
	const role = await createProjectRole(
		request,
		opts.projectId,
		opts.roleName,
		denyResourcePolicy(
			opts.projectId,
			opts.kind,
			opts.resourceId,
			opts.actions,
		),
	);
	await replaceMemberRoles(request, opts.projectId, opts.memberId, [
		...opts.keepRoleIds,
		role.id,
	]);
	return role;
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
				role_ids: [await firstProjectRoleId(request, projectId)],
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

export async function listGlobalAgents(request: APIRequestContext): Promise<
	Array<{
		id: string;
		name: string;
		roles?: Array<{ id: string; name: string }>;
	}>
> {
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
