/**
 * Host-side implementation of the plugin SDK's PluginApiClient contract
 * (see @paca-ai/plugin-sdk-react's api-client.ts, which every plugin
 * frontend bundles its own copy of via Module Federation). apps/web does
 * not depend on that package, so this is duck-typed to match its runtime
 * shape rather than sharing a class — plugin components call `props.api.*`
 * and don't care which implementation answers.
 */

interface SuccessEnvelope<T> {
	success: true;
	data: T;
}

export interface HostPluginApiClient {
	readonly projectId: string;
	listTasks(filters?: Record<string, unknown>): Promise<unknown[]>;
	getTask(taskId: string): Promise<unknown>;
	getProject(): Promise<unknown>;
	listMembers(): Promise<unknown[]>;
	pluginGet<T>(pluginId: string, path: string): Promise<T>;
	pluginPost<T>(pluginId: string, path: string, body: unknown): Promise<T>;
	pluginPatch<T>(pluginId: string, path: string, body: unknown): Promise<T>;
	pluginDelete(pluginId: string, path: string): Promise<void>;
}

const API_BASE_URL = `${window.location.origin}/api/v1`;

function pluginUrl(pluginId: string, path: string): string {
	const p = path.startsWith("/") ? path : `/${path}`;
	return `${API_BASE_URL}/plugins/${pluginId}${p}`;
}

// Project-scoped methods (listTasks/getTask/getProject/listMembers) are only
// meaningful when the client was built with a projectId — admin/global-scope
// pages build one without. Fail loudly instead of hitting a malformed
// `/projects//...` URL, which otherwise surfaces as an opaque 404.
function requireProjectId(projectId: string, method: string): void {
	if (!projectId) {
		throw new Error(
			`[PluginApiClient] ${method}() requires a project-scoped client, but no projectId was provided — this plugin page is rendered without a project context (e.g. an admin/global-scope page).`,
		);
	}
}

async function request<T>(
	method: string,
	url: string,
	body?: unknown,
): Promise<T> {
	const init: RequestInit = {
		method,
		// Auth is via an HttpOnly session cookie (see apiClient's withCredentials
		// use of the same cookie) rather than a bearer token, so plain fetch
		// with credentials included is sufficient — no token to attach.
		credentials: "include",
		headers: { "Content-Type": "application/json" },
	};
	if (body !== undefined) {
		init.body = JSON.stringify(body);
	}

	const res = await fetch(url, init);
	if (!res.ok) {
		const text = await res.text().catch(() => res.statusText);
		throw new Error(
			`[PluginApiClient] ${method} ${url} → ${res.status}: ${text}`,
		);
	}
	if (res.status === 204) return undefined as T;
	const json = (await res.json()) as SuccessEnvelope<T>;
	return json.data;
}

/**
 * Builds the client injected as `props.api` into every plugin extension-point
 * component. `projectId` should be omitted for admin/global-scope pages,
 * matching BaseExtensionProps's contract.
 */
export function createPluginApiClient(projectId = ""): HostPluginApiClient {
	return {
		projectId,
		listTasks: (filters) => {
			requireProjectId(projectId, "listTasks");
			const params = new URLSearchParams();
			for (const [key, value] of Object.entries(filters ?? {})) {
				if (value !== undefined && value !== null && value !== "") {
					params.set(key, String(value));
				}
			}
			if (!params.has("page_size")) params.set("page_size", "200");
			const qs = params.toString();
			return request<{ items: unknown[] }>(
				"GET",
				`${API_BASE_URL}/projects/${projectId}/tasks${qs ? `?${qs}` : ""}`,
			).then((envelope) => envelope.items);
		},
		getTask: (taskId) => {
			requireProjectId(projectId, "getTask");
			return request(
				"GET",
				`${API_BASE_URL}/projects/${projectId}/tasks/${taskId}`,
			);
		},
		getProject: () => {
			requireProjectId(projectId, "getProject");
			return request("GET", `${API_BASE_URL}/projects/${projectId}`);
		},
		listMembers: () => {
			requireProjectId(projectId, "listMembers");
			return request("GET", `${API_BASE_URL}/projects/${projectId}/members`);
		},
		pluginGet: (pluginId, path) => request("GET", pluginUrl(pluginId, path)),
		pluginPost: (pluginId, path, body) =>
			request("POST", pluginUrl(pluginId, path), body),
		pluginPatch: (pluginId, path, body) =>
			request("PATCH", pluginUrl(pluginId, path), body),
		pluginDelete: (pluginId, path) =>
			request("DELETE", pluginUrl(pluginId, path), undefined),
	};
}
