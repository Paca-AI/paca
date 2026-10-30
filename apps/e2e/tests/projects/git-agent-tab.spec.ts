// spec: features/projects/git-agent-tab.feature
// seed: tests/seed.spec.ts
//
// Providers, identities and triggers are answered in the browser (the e2e stack
// has no Git provider plugin); the agent and the permissions of the signed-in
// user are real.

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
	createProjectAgent,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { memberWith } from "../helpers/git";
import { allow, policyOf } from "../helpers/iam";

const PREFIX = "E2E_GITAGTAB_";
const RUN_ID = newRunId();

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
	capabilities: { multiple_identities: true, access_grants: true },
};

const IDENTITY = {
	id: "00000000-0000-4000-8000-0000000000c1",
	provider_id: PROVIDER.id,
	host: "git.example.test",
	kind: "app",
	scope: "agent",
	handle: "paca-reviewer",
	display_name: "Paca Reviewer",
	expires_at: null,
	created_at: "2026-10-01T00:00:00Z",
};

const KINDS = [
	"mention",
	"own_cr_feedback",
	"ci_failure",
	"review_request",
	"assignment",
	"auto_review",
];

const triggers = (supported: boolean) =>
	KINDS.map((kind) => ({
		kind,
		enabled: kind !== "auto_review",
		explicit: false,
		supported,
		config: {},
	}));

const ok = (data: unknown) => ({
	status: 200,
	contentType: "application/json",
	body: JSON.stringify({ success: true, data }),
});

interface Stub {
	providers: unknown[];
	identities: unknown[];
	supported: boolean;
	calls: Array<{
		method: string;
		path: string;
		body?: unknown;
		query?: string;
	}>;
}

async function stubGit(
	page: Page,
	projectId: string,
	agentId: string,
	stub: Stub,
) {
	await page.route(`**/api/v1/projects/${projectId}/git/providers`, (route) =>
		route.fulfill(ok(stub.providers)),
	);
	await page.route(
		`**/api/v1/projects/${projectId}/agents/${agentId}/git/**`,
		(route) => {
			const request = route.request();
			const url = new URL(request.url());
			const path = url.pathname.split(`/agents/${agentId}/git`)[1];
			if (path === "/identity" && request.method() === "GET") {
				return route.fulfill(ok(stub.identities));
			}
			if (path === "/triggers" && request.method() === "GET") {
				return route.fulfill(ok(triggers(stub.supported)));
			}
			stub.calls.push({
				method: request.method(),
				path,
				body: request.postDataJSON() ?? undefined,
				query: url.search,
			});
			if (path.startsWith("/triggers/")) {
				return route.fulfill(ok(triggers(stub.supported)));
			}
			if (path === "/identity" && request.method() === "DELETE") {
				stub.identities = [];
				return route.fulfill({ status: 204 });
			}
			// setup / access begin: send the browser to a page we control.
			return route.fulfill(
				ok({
					kind: "redirect",
					url: `${BASE_URL}/__git_platform__?step=${path.replaceAll("/", "-")}`,
					state: "s1",
				}),
			);
		},
	);
	await page.route("**/__git_platform__**", (route) =>
		route.fulfill({
			status: 200,
			contentType: "text/html",
			body: "<h1>platform</h1>",
		}),
	);
}

const newStub = (over: Partial<Stub> = {}): Stub => ({
	providers: [PROVIDER],
	identities: [],
	supported: true,
	calls: [],
	...over,
});

test.describe("Agent Git tab", () => {
	let projectId: string;
	let agentId: string;

	test.beforeAll(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
	});

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		projectId = await createProject(request, `${PREFIX}PROJECT_${RUN_ID}`);
		agentId = (
			await createProjectAgent(request, projectId, `${PREFIX}AGENT_${RUN_ID}`)
		).id;
	});

	test.afterAll(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
	});

	const openTab = async (page: Page) => {
		await page.goto(`${BASE_URL}/projects/${projectId}/agents/${agentId}#git`);
		await expect(
			page.getByRole("heading", { name: "Git identity" }),
		).toBeVisible({
			timeout: 15_000,
		});
	};

	test.describe("Identity", () => {
		test("No Git platform is available", async ({ page }) => {
			await signIn(page);
			await stubGit(page, projectId, agentId, newStub({ providers: [] }));
			await openTab(page);
			await expect(page.getByText("No Git platform available")).toBeVisible();
		});

		test("The agent uses the shared identity by default", async ({ page }) => {
			await signIn(page);
			await stubGit(page, projectId, agentId, newStub());
			await openTab(page);
			await expect(page.getByText("Uses the shared identity")).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Create identity" }),
			).toBeVisible();
		});

		test("Creating an own identity starts the platform's setup", async ({
			page,
		}) => {
			const stub = newStub();
			await signIn(page);
			await stubGit(page, projectId, agentId, stub);
			await openTab(page);

			await page.getByRole("button", { name: "Create identity" }).click();
			await page
				.getByRole("button", { name: /continue to git\.example\.test/i })
				.click();
			await expect(page).toHaveURL(/__git_platform__/);
			const setup = stub.calls.find((c) => c.path === "/identity/setup");
			expect(setup?.method).toBe("POST");
			expect(setup?.body).toMatchObject({
				provider_id: PROVIDER.id,
				host: "git.example.test",
			});
			expect(JSON.stringify(setup?.body)).toContain(
				`/projects/${projectId}/agents/${agentId}#git`,
			);
		});

		test("An own identity shows the bot and can be removed", async ({
			page,
		}) => {
			const stub = newStub({ identities: [IDENTITY] });
			await signIn(page);
			await stubGit(page, projectId, agentId, stub);
			await openTab(page);

			await expect(
				page.getByText("Own identity", { exact: true }),
			).toBeVisible();
			await expect(page.getByText("@paca-reviewer")).toBeVisible();
			await page.getByRole("button", { name: "Remove" }).click();
			const dialog = page.getByRole("dialog");
			await expect(dialog).toBeVisible();
			await dialog.getByRole("button", { name: "Remove connection" }).click();
			await expect
				.poll(() =>
					stub.calls.some(
						(c) => c.method === "DELETE" && c.path === "/identity",
					),
				)
				.toBe(true);
		});

		test("Installing on another account asks the platform", async ({
			page,
		}) => {
			const stub = newStub({ identities: [IDENTITY] });
			await signIn(page);
			await stubGit(page, projectId, agentId, stub);
			await openTab(page);

			await page
				.getByText("Install on another account or organization")
				.first()
				.click();
			await page
				.getByPlaceholder("account or organization (optional)")
				.fill("other-org");
			await page.getByRole("button", { name: "Install", exact: true }).click();
			await expect(page).toHaveURL(/__git_platform__/);
			expect(
				stub.calls.find((c) => c.path === "/identity/access/begin")?.body,
			).toMatchObject({
				provider_id: PROVIDER.id,
				owner: "other-org",
			});
		});

		test("Someone without git.identities:manage is told to ask an administrator", async ({
			page,
			request,
			playwright,
		}) => {
			const member = await memberWith(
				request,
				playwright,
				projectId,
				PREFIX,
				RUN_ID,
				"WRITER",
				["agents:read", "agents:write", "git:write"],
			);
			await signIn(page, member.username, RESTRICTED_PASSWORD);
			await stubGit(page, projectId, agentId, newStub());
			await openTab(page);

			await expect(
				page.getByText("Ask a workspace administrator to create one."),
			).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Create identity" }),
			).toHaveCount(0);
		});

		test("A manager gets the create button", async ({
			page,
			request,
			playwright,
		}) => {
			const member = await memberWith(
				request,
				playwright,
				projectId,
				PREFIX,
				RUN_ID,
				"MANAGER",
				["agents:read", "agents:write", "git:write"],
			);
			const role = `${PREFIX}WS_${RUN_ID}`;
			await createGlobalRole(
				request,
				role,
				policyOf(allow(["git.identities:manage"], ["git_identity/*"])),
			);
			await assignGlobalRoles(request, member.userId, [role]);
			await signIn(page, member.username, RESTRICTED_PASSWORD);
			await stubGit(page, projectId, agentId, newStub());
			await openTab(page);

			await expect(
				page.getByRole("button", { name: "Create identity" }),
			).toBeVisible();
		});
	});

	test.describe("Triggers", () => {
		test("Triggers are grouped by purpose", async ({ page }) => {
			await signIn(page);
			await stubGit(page, projectId, agentId, newStub());
			await openTab(page);

			await expect(page.getByText("Respond to requests")).toBeVisible();
			await expect(
				page.getByText("Follow up on its own pull requests"),
			).toBeVisible();
			await expect(page.getByText("Act on its own")).toBeVisible();
			await expect(page.getByText("Review every pull request")).toBeVisible();
		});

		test("Switching a trigger saves it", async ({ page }) => {
			const stub = newStub();
			await signIn(page);
			await stubGit(page, projectId, agentId, stub);
			await openTab(page);

			await page.getByRole("switch", { name: /Mentioned/ }).click();
			await expect
				.poll(
					() => stub.calls.find((c) => c.path === "/triggers/mention")?.body,
				)
				.toMatchObject({ enabled: false });
			expect(
				stub.calls.find((c) => c.path === "/triggers/mention")?.method,
			).toBe("PUT");
		});

		test("Triggers the platforms cannot deliver say so", async ({ page }) => {
			await signIn(page);
			await stubGit(page, projectId, agentId, newStub({ supported: false }));
			await openTab(page);

			await expect(
				page.getByText("Not supported by the connected Git platforms.").first(),
			).toBeVisible();
			await expect(page.locator("#git-trigger-mention")).toBeDisabled();
		});

		test("A reader cannot change triggers", async ({
			page,
			request,
			playwright,
		}) => {
			const member = await memberWith(
				request,
				playwright,
				projectId,
				PREFIX,
				RUN_ID,
				"READER",
				["agents:read"],
			);
			await signIn(page, member.username, RESTRICTED_PASSWORD);
			await stubGit(page, projectId, agentId, newStub());
			await openTab(page);

			await expect(page.locator("#git-trigger-mention")).toBeDisabled();
		});
	});
});
