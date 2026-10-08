import {
	type APIRequest,
	type APIRequestContext,
	type APIResponse,
	expect,
} from "@playwright/test";
import {
	API_URL,
	addProjectMember,
	createProjectRole,
	createUserWithPassword,
	POLICY_VERSION,
	type PolicyStatement,
	type RolePolicy,
} from "./e2e-api";
import { loginContext } from "./profile-users";

type Playwright = { request: Pick<APIRequest, "newContext"> };

// ─── Policy builders ─────────────────────────────────────────────────────────

/** A policy document made of the given statements. */
export function policyOf(...statements: PolicyStatement[]): RolePolicy {
	return { version: POLICY_VERSION, statements };
}

/** An Allow statement. */
export function allow(
	actions: string[],
	resources: string[],
	conditions?: PolicyStatement["conditions"],
): PolicyStatement {
	return {
		effect: "Allow",
		actions,
		resources,
		...(conditions ? { conditions } : {}),
	};
}

/** A Deny statement. */
export function deny(
	actions: string[],
	resources: string[],
	conditions?: PolicyStatement["conditions"],
): PolicyStatement {
	return {
		effect: "Deny",
		actions,
		resources,
		...(conditions ? { conditions } : {}),
	};
}

/**
 * The statement that lets a member open a project and nothing inside it: the
 * project-level checks that let list and create requests in name the project
 * itself, not what hangs off it.
 */
export function openProject(projectId: string, actions: string[] = []) {
	return allow(["projects:read", ...actions], [`project/${projectId}`]);
}

// ─── Responses ───────────────────────────────────────────────────────────────

/** The `error_code` of a failed API response. */
export async function errorCode(response: APIResponse): Promise<string> {
	return ((await response.json()) as { error_code?: string }).error_code ?? "";
}

/** The `issues` list of a ROLE_POLICY_INVALID response. */
export async function policyIssues(
	response: APIResponse,
): Promise<Array<{ path: string; message: string }>> {
	return (
		(
			(await response.json()) as {
				issues?: Array<{ path: string; message: string }>;
			}
		).issues ?? []
	);
}

/** A response's `data` payload. */
export async function dataOf<T>(response: APIResponse): Promise<T> {
	return (await response.json()).data as T;
}

// ─── Users and members ───────────────────────────────────────────────────────

export interface Person {
	username: string;
	userId: string;
}

/** Creates a user with a known password and no role beyond the default. */
export async function createPerson(
	request: APIRequestContext,
	playwright: Playwright,
	username: string,
): Promise<Person> {
	const userId = await createUserWithPassword(request, playwright, {
		username,
		fullName: username,
	});
	return { username, userId };
}

export interface Member extends Person {
	memberId: string;
}

/** Creates a user and adds them to the project holding exactly `roleIds`. */
export async function createMember(
	request: APIRequestContext,
	playwright: Playwright,
	projectId: string,
	username: string,
	roleIds: string[],
): Promise<Member> {
	const person = await createPerson(request, playwright, username);
	const memberId = await addProjectMember(
		request,
		projectId,
		person.userId,
		roleIds,
	);
	return { ...person, memberId };
}

/**
 * Runs `fn` with an API context signed in as `username`, and disposes it
 * afterwards. Every request it makes is judged against that user's own roles.
 */
export async function asUser<T>(
	playwright: Playwright,
	username: string,
	fn: (api: APIRequestContext) => Promise<T>,
): Promise<T> {
	const api = await loginContext(playwright, username);
	try {
		return await fn(api);
	} finally {
		await api.dispose();
	}
}

/** Creates a project role from statements and returns its id. */
export async function projectRoleFrom(
	request: APIRequestContext,
	projectId: string,
	name: string,
	...statements: PolicyStatement[]
): Promise<string> {
	return (
		await createProjectRole(request, projectId, name, policyOf(...statements))
	).id;
}

// ─── Project content ─────────────────────────────────────────────────────────

export async function createSprint(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/sprints`,
		{
			data: { name },
		},
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.id as string;
}

/** Creates a task as `request`'s user; returns the raw response. */
export function postTask(
	request: APIRequestContext,
	projectId: string,
	title: string,
	sprintId?: string | null,
): Promise<APIResponse> {
	return request.post(`${API_URL}/projects/${projectId}/tasks`, {
		data: { title, ...(sprintId ? { sprint_id: sprintId } : {}) },
	});
}

/** Creates a task as the admin and returns its id. */
export async function createTask(
	request: APIRequestContext,
	projectId: string,
	title: string,
	sprintId?: string | null,
): Promise<string> {
	const response = await postTask(request, projectId, title, sprintId);
	expect(response.ok(), await response.text()).toBeTruthy();
	return (await response.json()).data.id as string;
}

export interface TaskPage {
	status: number;
	ids: string[];
	titles: string[];
	total: number;
	nextCursor: string | null;
}

/** One page of the task list as `request`'s user sees it. */
export async function listTasks(
	request: APIRequestContext,
	projectId: string,
	query = "",
): Promise<TaskPage> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/tasks${query ? `?${query}` : ""}`,
	);
	if (!response.ok()) {
		return {
			status: response.status(),
			ids: [],
			titles: [],
			total: 0,
			nextCursor: null,
		};
	}
	const data = (await response.json()).data as {
		items: Array<{ id: string; title: string }>;
		total_count: number;
		next_cursor: string | null;
	};
	return {
		status: response.status(),
		ids: data.items.map((t) => t.id),
		titles: data.items.map((t) => t.title).sort(),
		total: data.total_count,
		nextCursor: data.next_cursor,
	};
}

/** Every task title the user can list, following the cursor. */
export async function listAllTaskTitles(
	request: APIRequestContext,
	projectId: string,
	pageSize = 2,
): Promise<{ titles: string[]; pages: number; total: number }> {
	const titles: string[] = [];
	let cursor: string | null = null;
	let pages = 0;
	let total = 0;
	do {
		const page: TaskPage = await listTasks(
			request,
			projectId,
			`page_size=${pageSize}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
		);
		expect(page.status).toBe(200);
		titles.push(...page.titles);
		total = page.total;
		cursor = page.nextCursor;
		pages += 1;
		expect(pages, "the cursor must end").toBeLessThan(50);
	} while (cursor);
	return { titles: titles.sort(), pages, total };
}

/** The views a user sees in one sprint, or in the backlog / timeline. */
export async function listViewIds(
	request: APIRequestContext,
	projectId: string,
	query: string,
): Promise<{ status: number; ids: string[] }> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/views?${query}`,
	);
	if (!response.ok()) return { status: response.status(), ids: [] };
	const items: Array<{ id: string }> = (await response.json()).data.items ?? [];
	return { status: response.status(), ids: items.map((v) => v.id) };
}

/** An id plus its sprint, for every sprint view the admin can see. */
export async function sprintViewIds(
	request: APIRequestContext,
	projectId: string,
	sprintId: string,
): Promise<string[]> {
	const { status, ids } = await listViewIds(
		request,
		projectId,
		`context=sprint&sprint_id=${sprintId}`,
	);
	expect(status).toBe(200);
	return ids;
}

export async function createFolder(
	request: APIRequestContext,
	projectId: string,
	name: string,
	parentId?: string,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/docs/folders`,
		{ data: { name, ...(parentId ? { parent_id: parentId } : {}) } },
	);
	expect(response.ok(), await response.text()).toBeTruthy();
	return (await response.json()).data.id as string;
}

export function postDoc(
	request: APIRequestContext,
	projectId: string,
	title: string,
	folderId?: string,
): Promise<APIResponse> {
	return request.post(`${API_URL}/projects/${projectId}/docs`, {
		data: { title, ...(folderId ? { folder_id: folderId } : {}) },
	});
}

export async function createDoc(
	request: APIRequestContext,
	projectId: string,
	title: string,
	folderId?: string,
): Promise<string> {
	const response = await postDoc(request, projectId, title, folderId);
	expect(response.ok(), await response.text()).toBeTruthy();
	return (await response.json()).data.id as string;
}

export async function listDocTitles(
	request: APIRequestContext,
	projectId: string,
): Promise<{ status: number; titles: string[] }> {
	const response = await request.get(`${API_URL}/projects/${projectId}/docs`);
	if (!response.ok()) return { status: response.status(), titles: [] };
	const items: Array<{ title: string }> =
		(await response.json()).data.items ?? [];
	return {
		status: response.status(),
		titles: items.map((d) => d.title).sort(),
	};
}

export async function listSprintIds(
	request: APIRequestContext,
	projectId: string,
): Promise<{ status: number; ids: string[] }> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/sprints`,
	);
	if (!response.ok()) return { status: response.status(), ids: [] };
	const items: Array<{ id: string }> = (await response.json()).data.items ?? [];
	return { status: response.status(), ids: items.map((s) => s.id) };
}

// ─── Agents acting as themselves ─────────────────────────────────────────────

/**
 * The shared secret the agent runner presents (`AGENT_API_KEY` of the e2e
 * stack). Together with `X-Agent-ID` it makes a request the agent's own, judged
 * against the agent's attachments and not those of the bot user behind the key.
 */
export const AGENT_API_KEY =
	process.env.E2E_AGENT_API_KEY ?? "e2e-agent-api-key";

/** An API context that acts as the agent. Dispose it when done. */
export async function agentContext(
	playwright: Playwright,
	agentId: string,
): Promise<APIRequestContext> {
	return playwright.request.newContext({
		extraHTTPHeaders: {
			"X-API-Key": AGENT_API_KEY,
			"X-Agent-ID": agentId,
		},
	});
}

/** Runs `fn` as the agent and disposes the context. */
export async function asAgent<T>(
	playwright: Playwright,
	agentId: string,
	fn: (api: APIRequestContext) => Promise<T>,
): Promise<T> {
	const api = await agentContext(playwright, agentId);
	try {
		return await fn(api);
	} finally {
		await api.dispose();
	}
}

// ─── Roles ───────────────────────────────────────────────────────────────────

export interface RoleRecord {
	id: string;
	name: string;
	description: string;
	project_id: string | null;
	is_system: boolean;
	is_default: boolean;
	policy: RolePolicy;
}

export async function listProjectRoles(
	request: APIRequestContext,
	projectId: string,
): Promise<RoleRecord[]> {
	const response = await request.get(`${API_URL}/projects/${projectId}/roles`);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data ?? [];
}

export async function listGlobalRoles(
	request: APIRequestContext,
): Promise<RoleRecord[]> {
	const response = await request.get(`${API_URL}/admin/roles`);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data ?? [];
}

/** Creates a workspace role and returns it (the raw response is for refusals). */
export function postGlobalRole(
	request: APIRequestContext,
	name: string,
	policy: RolePolicy,
	description = "",
): Promise<APIResponse> {
	return request.post(`${API_URL}/admin/roles`, {
		data: { name, description, policy },
	});
}

export function postProjectRole(
	request: APIRequestContext,
	projectId: string,
	name: string,
	policy: RolePolicy,
	description = "",
): Promise<APIResponse> {
	return request.post(`${API_URL}/projects/${projectId}/roles`, {
		data: { name, description, policy },
	});
}

/** Replaces the project roles of a member, returning the raw response. */
export function putMemberRoles(
	request: APIRequestContext,
	projectId: string,
	memberId: string,
	roleIds: string[],
): Promise<APIResponse> {
	return request.put(
		`${API_URL}/projects/${projectId}/members/${memberId}/roles`,
		{ data: { role_ids: roleIds } },
	);
}

/** Replaces the workspace roles of a user, returning the raw response. */
export function putUserRoles(
	request: APIRequestContext,
	userId: string,
	roleIds: string[],
): Promise<APIResponse> {
	return request.put(`${API_URL}/admin/users/${userId}/roles`, {
		data: { role_ids: roleIds },
	});
}

/** The actions a member holds in a project, as the permissions endpoint reports them. */
export async function projectActions(
	request: APIRequestContext,
	projectId: string,
): Promise<string[]> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/members/me/permissions`,
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.actions as string[];
}

/** The names of the roles a project member holds. */
export async function memberRoleNames(
	request: APIRequestContext,
	projectId: string,
	memberId: string,
): Promise<string[]> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/members/${memberId}/roles`,
	);
	expect(response.ok()).toBeTruthy();
	const roles: Array<{ name: string }> = (await response.json()).data ?? [];
	return roles.map((r) => r.name).sort();
}
