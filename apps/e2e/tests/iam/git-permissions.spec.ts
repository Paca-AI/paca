// spec: features/iam/git-permissions.feature
// seed: tests/seed.spec.ts
//
// The router gates of the Git integration (docs/ai-agent/git-agent-workflow.md,
// section 10), asserted over the API. The e2e stack has no Git provider plugin,
// so nothing here reaches a platform: a request that passes its gate then fails
// on something else (a missing provider, an unknown repository), and these
// specs only tell "refused for permissions" (401/403) from "got past the gate".
//
// Members sign in as themselves (asUser), so each request is judged against
// their own roles.

import {
	type APIRequestContext,
	type APIResponse,
	expect,
	test,
} from "@playwright/test";
import {
	API_URL,
	assignGlobalRoles,
	authRequest,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createProject,
	newRunId,
} from "../helpers/e2e-api";
import {
	allow,
	asUser,
	createMember,
	createPerson,
	policyOf,
	projectRoleFrom,
} from "../helpers/iam";

const PREFIX = "E2E_GITPERM_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

const REPOSITORY = "00000000-0000-4000-8000-000000000001";

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
}

type Playwright = Parameters<typeof asUser>[0];

/** An API context with no credentials at all. */
async function anonymous<T>(
	playwright: Playwright,
	fn: (api: APIRequestContext) => Promise<T>,
): Promise<T> {
	const api = await playwright.request.newContext();
	try {
		return await fn(api);
	} finally {
		await api.dispose();
	}
}

/** A member of `projectId` whose only project role allows `actions` there. */
async function memberWith(
	request: APIRequestContext,
	playwright: Playwright,
	projectId: string,
	label: string,
	actions: string[],
) {
	const roleId = await projectRoleFrom(
		request,
		projectId,
		name(`ROLE_${label}`),
		allow(["projects:read", ...actions], [`project/${projectId}`]),
		allow(actions, [`project/${projectId}/*`]),
	);
	return createMember(request, playwright, projectId, name(label), [roleId]);
}

/** Gives `userId` exactly one workspace role allowing git.identities:manage. */
async function grantIdentitiesManage(
	request: APIRequestContext,
	userId: string,
	label: string,
) {
	const roleName = name(`WS_${label}`);
	await createGlobalRole(
		request,
		roleName,
		policyOf(allow(["git.identities:manage"], ["git_identity/*"])),
	);
	await assignGlobalRoles(request, userId, [roleName]);
}

const gitUrl = (projectId: string, path = "") =>
	`${API_URL}/projects/${projectId}/git${path}`;
const repoUrl = (projectId: string, path = "") =>
	gitUrl(projectId, `/repositories/${REPOSITORY}${path}`);

const LINK_BODY = { provider_id: "no-such-provider", path_or_url: "acme/web" };

/** True when the request was refused for permissions rather than for its content. */
function refused(response: APIResponse): boolean {
	return response.status() === 401 || response.status() === 403;
}

test.describe("Git integration permissions", () => {
	let projectId: string;

	test.beforeAll(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		projectId = await createProject(request, name("PROJECT"));
	});

	test.afterAll(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test.describe("Reading and writing a project's Git integration", () => {
		test("A caller who is not signed in is refused", async ({ playwright }) => {
			await anonymous(playwright, async (api) => {
				for (const path of [
					"/providers",
					"/repositories",
					"/settings",
					"/runs",
				]) {
					expect((await api.get(gitUrl(projectId, path))).status(), path).toBe(
						401,
					);
				}
				expect(
					(
						await api.post(gitUrl(projectId, "/repositories"), {
							data: LINK_BODY,
						})
					).status(),
				).toBe(401);
			});
		});

		test("A signed-in user with no role on the project is refused", async ({
			request,
			playwright,
		}) => {
			const stranger = await createPerson(
				request,
				playwright,
				name("STRANGER"),
			);
			await asUser(playwright, stranger.username, async (api) => {
				for (const path of ["/providers", "/repositories", "/settings"]) {
					expect((await api.get(gitUrl(projectId, path))).status(), path).toBe(
						403,
					);
				}
			});
		});

		test("git:read reads and cannot change anything", async ({
			request,
			playwright,
		}) => {
			const reader = await memberWith(
				request,
				playwright,
				projectId,
				"READER",
				["git:read"],
			);
			await asUser(playwright, reader.username, async (api) => {
				for (const path of [
					"/providers",
					"/repositories",
					"/settings",
					"/runs",
				]) {
					expect((await api.get(gitUrl(projectId, path))).status(), path).toBe(
						200,
					);
				}
				const repositories = await api.get(gitUrl(projectId, "/repositories"));
				expect((await repositories.json()).data).toEqual([]);

				const writes: Array<[string, APIResponse]> = [
					[
						"settings",
						await api.put(gitUrl(projectId, "/settings"), {
							data: { paused: true },
						}),
					],
					[
						"link",
						await api.post(gitUrl(projectId, "/repositories"), {
							data: LINK_BODY,
						}),
					],
					[
						"access/begin",
						await api.post(gitUrl(projectId, "/access/begin"), { data: {} }),
					],
					[
						"write credential",
						await api.post(repoUrl(projectId, "/credential/write"), {
							data: {},
						}),
					],
					["unlink", await api.delete(repoUrl(projectId))],
				];
				for (const [what, response] of writes) {
					expect(response.status(), what).toBe(403);
				}
				// git:read is enough for a read-only credential: past the gate, then
				// the repository does not exist.
				expect(
					refused(
						await api.post(repoUrl(projectId, "/credential/read"), {
							data: {},
						}),
					),
				).toBe(false);
			});
		});

		test("Unlinking reaches its handler for a project writer", async ({
			request,
			playwright,
		}) => {
			const writer = await memberWith(
				request,
				playwright,
				projectId,
				"UNLINKER",
				["git:read", "git:write"],
			);
			await asUser(playwright, writer.username, async (api) => {
				const response = await api.delete(repoUrl(projectId));
				// The repository does not exist, so the handler answers a JSON 404. A
				// route that is not registered answers a plain-text "404 page not found".
				expect(response.status()).toBe(404);
				expect(response.headers()["content-type"]).toContain(
					"application/json",
				);
			});
		});

		test("A role on another project grants nothing here", async ({
			request,
			playwright,
		}) => {
			const otherId = await createProject(request, name("OTHER"));
			const member = await memberWith(
				request,
				playwright,
				otherId,
				"ELSEWHERE",
				["git:write", "git:read"],
			);
			await asUser(playwright, member.username, async (api) => {
				expect(
					(await api.get(gitUrl(projectId, "/repositories"))).status(),
				).toBe(403);
				expect((await api.get(gitUrl(otherId, "/repositories"))).status()).toBe(
					200,
				);
			});
		});
	});

	test.describe("Linking a repository", () => {
		test("git:write alone passes the link gate", async ({
			request,
			playwright,
		}) => {
			const writer = await memberWith(
				request,
				playwright,
				projectId,
				"WRITER",
				["git:read", "git:write"],
			);
			await asUser(playwright, writer.username, async (api) => {
				const response = await api.post(gitUrl(projectId, "/repositories"), {
					data: LINK_BODY,
				});
				// No provider is installed, so linking fails, but not for permissions:
				// the connections it would go through were made available by whoever
				// holds git.identities:manage, or by the project itself.
				expect(refused(response), `status ${response.status()}`).toBe(false);
				expect(response.ok()).toBe(false);
				expect(
					refused(
						await api.put(gitUrl(projectId, "/settings"), {
							data: { paused: true },
						}),
					),
				).toBe(false);
			});
		});

		test("git:read cannot link a repository", async ({ request, playwright }) => {
			const reader = await memberWith(request, playwright, projectId, "READER", [
				"git:read",
			]);
			await asUser(playwright, reader.username, async (api) => {
				expect(
					(
						await api.post(gitUrl(projectId, "/repositories"), {
							data: LINK_BODY,
						})
					).status(),
				).toBe(403);
			});
		});

		test("git.identities:manage alone cannot link a repository", async ({
			request,
			playwright,
		}) => {
			const admin = await createPerson(request, playwright, name("MANAGEONLY"));
			await grantIdentitiesManage(request, admin.userId, "MANAGEONLY");
			await asUser(playwright, admin.username, async (api) => {
				expect(
					(
						await api.post(gitUrl(projectId, "/repositories"), {
							data: LINK_BODY,
						})
					).status(),
				).toBe(403);
			});
		});

		test("a project's own connection needs git:write and git.connections:manage", async ({
			request,
			playwright,
		}) => {
			const setup = (api: APIRequestContext) =>
				api.post(gitUrl(projectId, "/connections/setup"), {
					data: { provider_id: "com.paca.github" },
				});
			const writer = await memberWith(request, playwright, projectId, "CONN_W", [
				"git:read",
				"git:write",
			]);
			const connector = await memberWith(request, playwright, projectId, "CONN_M", [
				"git:read",
				"git:write",
				"git.connections:manage",
			]);
			const onlyManage = await memberWith(request, playwright, projectId, "CONN_O", [
				"git:read",
				"git.connections:manage",
			]);
			await asUser(playwright, writer.username, async (api) => {
				expect((await setup(api)).status()).toBe(403);
			});
			await asUser(playwright, onlyManage.username, async (api) => {
				expect((await setup(api)).status()).toBe(403);
			});
			await asUser(playwright, connector.username, async (api) => {
				// No provider is installed, so it fails, but not for permissions.
				expect(refused(await setup(api))).toBe(false);
			});
		});

		test("the workspace action does not stand in for the project one", async ({
			request,
			playwright,
		}) => {
			const both = await memberWith(request, playwright, projectId, "BOTH", [
				"git:read",
				"git:write",
			]);
			await grantIdentitiesManage(request, both.userId, "BOTH");
			await asUser(playwright, both.username, async (api) => {
				expect(
					(
						await api.post(gitUrl(projectId, "/connections/setup"), {
							data: { provider_id: "com.paca.github" },
						})
					).status(),
				).toBe(403);
			});
		});
	});

	test.describe("Approving and merging are their own actions", () => {
		const approve = (id: string) => repoUrl(id, "/change-requests/1/approvals");
		const merge = (id: string) => repoUrl(id, "/change-requests/1/merge");

		test("git:write cannot approve or merge", async ({
			request,
			playwright,
		}) => {
			const writer = await memberWith(
				request,
				playwright,
				projectId,
				"NOAPPROVE",
				["git:read", "git:write"],
			);
			await asUser(playwright, writer.username, async (api) => {
				expect(
					(await api.post(approve(projectId), { data: {} })).status(),
				).toBe(403);
				expect((await api.post(merge(projectId), { data: {} })).status()).toBe(
					403,
				);
			});
		});

		test("git:approve and git:merge each open only their own route", async ({
			request,
			playwright,
		}) => {
			const approver = await memberWith(
				request,
				playwright,
				projectId,
				"APPROVER",
				["git:approve"],
			);
			const merger = await memberWith(
				request,
				playwright,
				projectId,
				"MERGER",
				["git:merge"],
			);
			await asUser(playwright, approver.username, async (api) => {
				expect(refused(await api.post(approve(projectId), { data: {} }))).toBe(
					false,
				);
				expect((await api.post(merge(projectId), { data: {} })).status()).toBe(
					403,
				);
			});
			await asUser(playwright, merger.username, async (api) => {
				expect(refused(await api.post(merge(projectId), { data: {} }))).toBe(
					false,
				);
				expect(
					(await api.post(approve(projectId), { data: {} })).status(),
				).toBe(403);
			});
		});
	});

	test.describe("Bot identities are a workspace privilege", () => {
		test("Listing providers and identities needs git.identities:manage", async ({
			request,
			playwright,
		}) => {
			const manager = await createPerson(request, playwright, name("MANAGER"));
			await grantIdentitiesManage(request, manager.userId, "MANAGER");
			const gitOnly = await createPerson(request, playwright, name("GITSTAR"));
			const roleName = name("WS_GITSTAR");
			await createGlobalRole(
				request,
				roleName,
				policyOf(allow(["git:*"], ["*"])),
			);
			await assignGlobalRoles(request, gitOnly.userId, [roleName]);

			await asUser(playwright, manager.username, async (api) => {
				expect((await api.get(`${API_URL}/git/providers`)).status()).toBe(200);
				expect((await api.get(`${API_URL}/git/identities`)).status()).toBe(200);
			});
			await asUser(playwright, gitOnly.username, async (api) => {
				expect((await api.get(`${API_URL}/git/providers`)).status()).toBe(403);
				expect((await api.get(`${API_URL}/git/identities`)).status()).toBe(403);
			});
		});

		test("A project role cannot reach the workspace routes", async ({
			request,
			playwright,
		}) => {
			const member = await memberWith(
				request,
				playwright,
				projectId,
				"PROJGIT",
				["git:*"],
			);
			await asUser(playwright, member.username, async (api) => {
				expect((await api.get(`${API_URL}/git/identities`)).status()).toBe(403);
				expect(
					(
						await api.post(`${API_URL}/git/identities/setup`, { data: {} })
					).status(),
				).toBe(403);
				expect(
					(
						await api.delete(`${API_URL}/git/identities/${REPOSITORY}`)
					).status(),
				).toBe(403);
			});
		});
	});

	test.describe("The webhook intake is public", () => {
		test("An unknown endpoint key is a 404, not a 401", async ({
			playwright,
		}) => {
			await anonymous(playwright, async (api) => {
				const response = await api.post(
					`${API_URL}/git/webhooks/${name("NO_SUCH_KEY").toLowerCase()}`,
					{ data: { zen: "keep it logically awesome" } },
				);
				expect(response.status()).toBe(404);
			});
		});
	});
});
