// spec: features/projects/git-settings.feature
// seed: tests/seed.spec.ts
//
// The Git tab of a project's settings. The e2e stack has no Git provider
// plugin, so the provider-backed endpoints (providers, repositories) are
// answered in the browser with page.route; who may see what is still decided
// by the real permissions of the signed-in member.

import { expect, type Page, test } from "@playwright/test";
import {
	assignGlobalRoles,
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createProject,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { allow, createMember, policyOf, projectRoleFrom } from "../helpers/iam";

const PREFIX = "E2E_GITSET_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

const PROVIDER = {
	id: "com.example.git",
	display_name: "Example Git",
	hosts: ["git.example.test"],
	self_hosted_hosts: false,
	terms: {
		change_request: "pull request",
		change_request_plural: "pull requests",
		repository: "Repository",
	},
	capabilities: { multiple_identities: false, access_grants: false },
};

const LINKED = {
	id: "00000000-0000-4000-8000-0000000000a1",
	provider_id: PROVIDER.id,
	host: "git.example.test",
	full_path: "acme/web",
	default_branch: "main",
	clone_url: "https://git.example.test/acme/web.git",
	identity_id: null,
	identity_handle: "paca-bot",
	created_at: "2026-10-01T00:00:00Z",
};

const CONNECTION = {
	id: "00000000-0000-4000-8000-0000000000c1",
	provider_id: PROVIDER.id,
	host: "git.example.test",
	kind: "app",
	scope: "shared",
	project_id: null,
	handle: "paca-bot",
	display_name: "Paca",
	expires_at: null,
	created_at: "2026-10-01T00:00:00Z",
};

const ok = (data: unknown) => ({
	status: 200,
	contentType: "application/json",
	body: JSON.stringify({ success: true, data }),
});

/**
 * Answers the provider-backed Git endpoints of one project in the browser.
 * `repositories` is what the list returns; a POST links `LINKED` and records
 * the request body.
 */
async function stubGit(
	page: Page,
	projectId: string,
	opts: {
		providers: unknown[];
		connections?: unknown[];
		linkRequests?: unknown[];
	},
) {
	let repositories: unknown[] = [];
	await page.route(`**/api/v1/projects/${projectId}/git/connections`, (route) =>
		route.fulfill(ok(opts.connections ?? [])),
	);
	await page.route(`**/api/v1/projects/${projectId}/git/providers`, (route) =>
		route.fulfill(ok(opts.providers)),
	);
	await page.route(
		`**/api/v1/projects/${projectId}/git/repositories`,
		(route) => {
			const request = route.request();
			if (request.method() === "POST") {
				opts.linkRequests?.push(request.postDataJSON());
				repositories = [LINKED];
				return route.fulfill(ok(LINKED));
			}
			return route.fulfill(ok(repositories));
		},
	);
}

const openGitSettings = async (page: Page, projectId: string) => {
	await page.goto(`${BASE_URL}/projects/${projectId}/settings#git`);
	await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
};

test.describe("Project Git settings", () => {
	let projectId: string;

	test.beforeAll(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
	});

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		projectId = await createProject(request, name("PROJECT"));
	});

	test.afterAll(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
	});

	test.describe("The tab reflects what is installed", () => {
		test("No provider is installed", async ({ page }) => {
			await signIn(page);
			await stubGit(page, projectId, { providers: [] });
			await openGitSettings(page, projectId);

			await expect(page.getByText("No repositories linked")).toBeVisible();
			await expect(
				page.getByText("No Git provider is installed", { exact: false }),
			).toBeVisible();
			await expect(page.getByRole("button", { name: "Link" })).toHaveCount(0);
		});

		test("An administrator links a repository", async ({ page }) => {
			const linkRequests: unknown[] = [];
			await signIn(page);
			await stubGit(page, projectId, {
				providers: [PROVIDER],
				connections: [CONNECTION],
				linkRequests,
			});
			await openGitSettings(page, projectId);

			await page.getByLabel("Repository").fill("acme/web");
			await page.getByRole("button", { name: "Link" }).click();

			await expect(page.getByText("acme/web", { exact: true })).toBeVisible();
			expect(linkRequests).toEqual([
				{ provider_id: PROVIDER.id, path_or_url: "acme/web" },
			]);
		});
	});

	test.describe("Who sees the link form", () => {
		/** A member holding exactly `actions` on the project, signed in to the UI. */
		async function signInAsMember(
			page: Page,
			request: Parameters<typeof createMember>[0],
			playwright: Parameters<typeof createMember>[1],
			label: string,
			actions: string[],
			workspaceActions: string[] = [],
		) {
			const roleId = await projectRoleFrom(
				request,
				projectId,
				name(`ROLE_${label}`),
				allow(["projects:read", ...actions], [`project/${projectId}`]),
				allow(actions, [`project/${projectId}/*`]),
			);
			const member = await createMember(
				request,
				playwright,
				projectId,
				name(label),
				[roleId],
			);
			if (workspaceActions.length > 0) {
				const roleName = name(`WS_${label}`);
				await createGlobalRole(
					request,
					roleName,
					policyOf(allow(workspaceActions, ["git_identity/*"])),
				);
				await assignGlobalRoles(request, member.userId, [roleName]);
			}
			await signIn(page, member.username, RESTRICTED_PASSWORD);
		}

		test("A writer is told to ask for a connection when there is none", async ({
			page,
			request,
			playwright,
		}) => {
			await signInAsMember(page, request, playwright, "WRITER", [
				"git:read",
				"git:write",
			]);
			await stubGit(page, projectId, { providers: [PROVIDER] });
			await openGitSettings(page, projectId);

			await expect(
				page.getByText("Ask someone who can manage Git connections", {
					exact: false,
				}),
			).toBeVisible();
			await expect(page.getByRole("button", { name: "Link" })).toHaveCount(0);
		});

		test("A writer gets the form once a connection is available", async ({
			page,
			request,
			playwright,
		}) => {
			await signInAsMember(page, request, playwright, "MANAGER", [
				"git:read",
				"git:write",
			]);
			await stubGit(page, projectId, {
				providers: [PROVIDER],
				connections: [CONNECTION],
			});
			await openGitSettings(page, projectId);

			await expect(page.getByLabel("Repository")).toBeVisible();
			await expect(page.getByRole("button", { name: "Link" })).toBeVisible();
		});

		test("A reader is told they need write access", async ({
			page,
			request,
			playwright,
		}) => {
			await signInAsMember(page, request, playwright, "READER", ["git:read"]);
			await stubGit(page, projectId, { providers: [PROVIDER] });
			await openGitSettings(page, projectId);

			await expect(
				page.getByText("Git write access", { exact: false }),
			).toBeVisible();
			await expect(page.getByRole("button", { name: "Link" })).toHaveCount(0);
		});
	});
});
