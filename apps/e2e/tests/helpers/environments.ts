import { type APIRequestContext, expect } from "@playwright/test";
import { API_URL, authRequest } from "./e2e-api";

export interface SeededEnvironment {
	id: string;
	name: string;
	slug: string;
	status: string;
}

// Statuses an environment passes through while agent-runner is still
// provisioning/starting/stopping its backing container. The final status
// (running / stopped / error) depends on whether the e2e stack's
// agent-runner can actually launch a sandbox, so specs never assert on it.
const TRANSITIONAL_STATUSES = ["creating", "starting", "stopping", "deleting"];

/** Creates an environment via the API. It comes back in "creating" status. */
export async function createEnvironment(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<SeededEnvironment> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/environments`,
		{ data: { name } },
	);
	expect(response.ok()).toBeTruthy();
	const env = (await response.json()).data;
	return { id: env.id, name, slug: env.slug, status: env.status };
}

export async function setEnvironmentAccessMode(
	request: APIRequestContext,
	projectId: string,
	environmentId: string,
	accessMode: "open" | "restricted",
): Promise<void> {
	const response = await request.patch(
		`${API_URL}/projects/${projectId}/environments/${environmentId}`,
		{ data: { access_mode: accessMode } },
	);
	expect(response.ok()).toBeTruthy();
}

export async function listEnvironments(
	request: APIRequestContext,
	projectId: string,
): Promise<SeededEnvironment[]> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/environments`,
	);
	if (!response.ok()) return [];
	const environments: Array<{
		id: string;
		name: string;
		slug: string;
		status: string;
	}> = (await response.json()).data?.environments ?? [];
	return environments.map((e) => ({
		id: e.id,
		name: e.name,
		slug: e.slug,
		status: e.status,
	}));
}

/** Polls until the environment leaves every transitional status (or times out). */
export async function waitForSettledStatus(
	request: APIRequestContext,
	projectId: string,
	environmentId: string,
	timeoutMs = 60_000,
): Promise<string> {
	const deadline = Date.now() + timeoutMs;
	let status = "creating";
	while (Date.now() < deadline) {
		const env = (await listEnvironments(request, projectId)).find(
			(e) => e.id === environmentId,
		);
		if (!env) return "deleted";
		status = env.status;
		if (!TRANSITIONAL_STATUSES.includes(status)) return status;
		await new Promise((resolve) => setTimeout(resolve, 1_000));
	}
	return status;
}

/**
 * Deletes every environment in every project whose name starts with
 * `projectPrefix`, waiting for in-flight provisioning to settle first so a
 * half-created sandbox container isn't leaked when its project goes away.
 * Best-effort: failures are swallowed so cleanup never fails a test.
 */
export async function cleanupEnvironmentsInProjectsByPrefix(
	request: APIRequestContext,
	projectPrefix: string,
): Promise<void> {
	await authRequest(request);
	const projects: Array<{ id: string; name: string }> = [];
	for (let page = 1; ; page++) {
		const response = await request.get(
			`${API_URL}/projects?page=${page}&page_size=100`,
		);
		if (!response.ok()) break;
		const data = (await response.json()).data as {
			items?: Array<{ id: string; name: string }>;
			page: number;
			page_size: number;
			total: number;
		};
		const items = data.items ?? [];
		if (items.length === 0) break;
		projects.push(...items);
		if (data.page * data.page_size >= data.total) break;
	}

	for (const project of projects.filter((p) =>
		p.name.startsWith(projectPrefix),
	)) {
		for (const env of await listEnvironments(request, project.id)) {
			await waitForSettledStatus(request, project.id, env.id, 30_000);
			await request
				.delete(`${API_URL}/projects/${project.id}/environments/${env.id}`)
				.catch(() => undefined);
		}
	}
}
