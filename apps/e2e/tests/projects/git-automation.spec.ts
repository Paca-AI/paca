// spec: features/projects/git-automation.feature
// seed: tests/seed.spec.ts
//
// The provider and repository endpoints are answered in the browser (the e2e
// stack has no Git provider plugin); the project's Git settings are real.

import { expect, type Page, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createProjectAgent,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { memberWith } from "../helpers/git";
import { dataOf } from "../helpers/iam";

const PREFIX = "E2E_GITAUTO_";
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
const fail = (status: number, code: string) => ({
	status,
	contentType: "application/json",
	body: JSON.stringify({ success: false, error_code: code, error: code }),
});

interface Stub {
	repositories: unknown[];
	deleted: string[];
	linkError?: { status: number; code: string };
}

/** Answers providers and repositories in the browser; settings stay real. */
async function stubGit(page: Page, projectId: string, stub: Stub) {
	await page.route(`**/api/v1/projects/${projectId}/git/providers`, (route) =>
		route.fulfill(ok([PROVIDER])),
	);
	await page.route(`**/api/v1/projects/${projectId}/git/connections`, (route) =>
		route.fulfill(ok([CONNECTION])),
	);
	await page.route(
		`**/api/v1/projects/${projectId}/git/repositories`,
		(route) => {
			if (route.request().method() === "POST") {
				if (stub.linkError) {
					return route.fulfill(
						fail(stub.linkError.status, stub.linkError.code),
					);
				}
				stub.repositories = [LINKED];
				return route.fulfill(ok(LINKED));
			}
			return route.fulfill(ok(stub.repositories));
		},
	);
	await page.route(
		`**/api/v1/projects/${projectId}/git/repositories/*`,
		(route) => {
			if (route.request().method() === "DELETE") {
				stub.deleted.push(route.request().url().split("/").pop() ?? "");
				stub.repositories = [];
				return route.fulfill({ status: 204 });
			}
			return route.continue();
		},
	);
}

const open = async (page: Page, projectId: string) => {
	await page.goto(`${BASE_URL}/projects/${projectId}/settings#git`);
	await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
};

test.describe("Project Git automation", () => {
	let projectId: string;
	let agentName: string;

	test.beforeAll(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
	});

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		projectId = await createProject(request, name("PROJECT"));
		agentName = name("AGENT");
		await createProjectAgent(request, projectId, agentName);
	});

	test.afterAll(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
	});

	const settings = async (request: Parameters<typeof authRequest>[0]) =>
		dataOf<{
			default_responder_agent_id: string | null;
			paused: boolean;
			min_actor_permission: string;
		}>(await request.get(`${API_URL}/projects/${projectId}/git/settings`));

	test.describe("Linked repositories", () => {
		test("A linked repository shows its branch and the bot to mention", async ({
			page,
		}) => {
			await signIn(page);
			await stubGit(page, projectId, {
				repositories: [LINKED],
				deleted: [],
			});
			await open(page, projectId);

			await expect(page.getByText("acme/web", { exact: true })).toBeVisible();
			await expect(page.getByText("main", { exact: true })).toBeVisible();
			await expect(page.getByText("@paca-bot").first()).toBeVisible();
			await expect(page.getByText("Using it")).toBeVisible();
		});

		test("Unlinking asks for confirmation", async ({ page }) => {
			const stub: Stub = { repositories: [LINKED], deleted: [] };
			await signIn(page);
			await stubGit(page, projectId, stub);
			await open(page, projectId);

			await page.getByRole("button", { name: "Unlink" }).click();
			await expect(
				page.getByRole("button", { name: "Confirm unlink" }),
			).toBeVisible();
			await page.getByRole("button", { name: "Cancel" }).click();
			await expect(page.getByText("acme/web", { exact: true })).toBeVisible();
			expect(stub.deleted).toEqual([]);

			await page.getByRole("button", { name: "Unlink" }).click();
			await page.getByRole("button", { name: "Confirm unlink" }).click();
			await expect(page.getByText("No repositories linked")).toBeVisible();
			expect(stub.deleted).toEqual([LINKED.id]);
		});

		const errors: Array<[number, string, RegExp]> = [
			[409, "GIT_ALREADY_LINKED", /already linked to the project/i],
			[409, "GIT_NO_IDENTITY", /No connection is available for this platform/i],
			[503, "GIT_PROVIDER_UNAVAILABLE", /provider plugin is unavailable/i],
			[404, "GIT_NOT_FOUND", /Not found/i],
		];
		for (const [status, code, text] of errors) {
			test(`A ${code} answer is explained`, async ({ page }) => {
				await signIn(page);
				await stubGit(page, projectId, {
					repositories: [],
					deleted: [],
					linkError: { status, code },
				});
				await open(page, projectId);
				await page.getByLabel("Repository").fill("acme/web");
				await page.getByRole("button", { name: "Link" }).click();
				await expect(page.getByRole("alert")).toContainText(text);
			});
		}

		test("A repository the identity cannot see offers the install", async ({
			page,
		}) => {
			await signIn(page);
			await stubGit(page, projectId, {
				repositories: [],
				deleted: [],
				linkError: { status: 409, code: "GIT_ACCESS_NOT_GRANTED" },
			});
			await open(page, projectId);
			await page
				.getByLabel("Repository")
				.fill("git@git.example.test:other-org/lib.git");
			await page.getByRole("button", { name: "Link" }).click();

			await expect(page.getByRole("alert")).toContainText(
				/can't see this repository/i,
			);
			await expect(
				page.getByRole("button", { name: "Install on other-org" }),
			).toBeVisible();
		});
	});

	test.describe("Automation", () => {
		test("Automation is locked until a repository is linked", async ({
			page,
		}) => {
			await signIn(page);
			await stubGit(page, projectId, { repositories: [], deleted: [] });
			await open(page, projectId);

			await expect(page.getByText("Link a repository first.")).toBeVisible();
			await expect(page.getByLabel("Default responder")).toBeDisabled();
			await expect(page.getByRole("button", { name: "Save" })).toHaveCount(0);
		});

		test("A missing default responder is warned about", async ({ page }) => {
			await signIn(page);
			await stubGit(page, projectId, { repositories: [LINKED], deleted: [] });
			await open(page, projectId);

			await expect(
				page.getByText("mentions that name no agent are ignored"),
			).toBeVisible();
		});

		test("Choosing a responder saves it to the project", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await stubGit(page, projectId, { repositories: [LINKED], deleted: [] });
			await open(page, projectId);

			await page
				.getByLabel("Default responder")
				.selectOption({ label: agentName });
			await page.getByRole("button", { name: "Save" }).click();
			await expect(page.getByText("Saved.")).toBeVisible();
			await expect(
				page.getByText("mentions that name no agent are ignored"),
			).toHaveCount(0);

			await authRequest(request);
			expect((await settings(request)).default_responder_agent_id).toBeTruthy();
		});

		test("Minimum access and the pause switch are saved", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await stubGit(page, projectId, { repositories: [LINKED], deleted: [] });
			await open(page, projectId);

			await page.getByText("Advanced", { exact: true }).click();
			await page
				.getByLabel("Minimum access to start an agent")
				.selectOption({ label: "Admin" });
			await page.getByRole("switch", { name: /pause automation/i }).click();
			await page.getByRole("button", { name: "Save" }).click();
			await expect(page.getByText("Saved.")).toBeVisible();

			await authRequest(request);
			expect(await settings(request)).toMatchObject({
				paused: true,
				min_actor_permission: "admin",
			});

			await page.reload();
			await page.getByText("Advanced", { exact: true }).click();
			await expect(
				page.getByLabel("Minimum access to start an agent"),
			).toHaveValue("admin");
			await expect(
				page.getByRole("switch", { name: /pause automation/i }),
			).toBeChecked();
		});

		test("Save is available only when something changed", async ({ page }) => {
			await signIn(page);
			await stubGit(page, projectId, { repositories: [LINKED], deleted: [] });
			await open(page, projectId);

			const save = page.getByRole("button", { name: "Save" });
			await expect(save).toBeDisabled();
			await page.getByText("Advanced", { exact: true }).click();
			await page.getByRole("switch", { name: /pause automation/i }).click();
			await expect(save).toBeEnabled();
			await page.getByRole("switch", { name: /pause automation/i }).click();
			await expect(save).toBeDisabled();
		});

		test("A reader sees the settings but cannot change them", async ({
			page,
			request,
			playwright,
		}) => {
			const reader = await memberWith(
				request,
				playwright,
				projectId,
				PREFIX,
				RUN_ID,
				"READER",
				["git:read"],
			);
			await signIn(page, reader.username, RESTRICTED_PASSWORD);
			await stubGit(page, projectId, { repositories: [LINKED], deleted: [] });
			await open(page, projectId);

			await expect(page.getByLabel("Default responder")).toBeDisabled();
			await expect(page.getByRole("button", { name: "Save" })).toHaveCount(0);
			await expect(page.getByRole("button", { name: "Unlink" })).toHaveCount(0);
		});
	});
});
