// spec: features/admin/agents.feature
// seed: tests/seed.spec.ts

import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	test,
} from "@playwright/test";
import {
	BASE_URL,
	cleanupGlobalAgentsByPrefix,
	cleanupGlobalRolesByPrefix,
	cleanupUsersByPrefix,
	createGlobalAgent,
	createUserWithGlobalPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_ADMAGENTS_";
const RUN_ID = newRunId();
const ADMIN_AGENTS_URL = `${BASE_URL}/admin/agents`;

// Users must go first: a global role that still has members can't be deleted.
async function cleanup(request: APIRequestContext) {
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
	await cleanupGlobalAgentsByPrefix(request, PREFIX);
}

async function createRestrictedUser(
	request: APIRequestContext,
	playwright: Parameters<typeof createUserWithGlobalPermissions>[1],
	label: string,
	permissions: Record<string, boolean>,
): Promise<string> {
	const username = `${PREFIX}${label}_${RUN_ID}`;
	await createUserWithGlobalPermissions(request, playwright, {
		username,
		roleName: `${PREFIX}ROLE_${label}_${RUN_ID}`,
		// A role must grant something; projects.read is irrelevant to Agents
		// and lets the account reach the home page after signing in.
		permissions: { "projects.read": true, ...permissions },
	});
	return username;
}

// The agent card is a plain <div class="group ..."> with no role or test id;
// anchoring on its (unique) name text and taking the innermost `group`
// ancestor is the least brittle handle available.
const agentCard = (page: Page, name: string): Locator =>
	page
		.locator("div.group")
		.filter({ has: page.getByText(name, { exact: true }) })
		.last();

const openCardMenu = async (page: Page, name: string) => {
	// The trigger is `opacity-0 group-hover:opacity-100`, and hover never
	// fires on touch projects, so force the click.
	await agentCard(page, name).getByRole("button").click({ force: true });
};

const openCreateDialog = async (page: Page) => {
	await page.getByRole("button", { name: "New Agent" }).click();
	const dialog = page.getByRole("dialog", { name: "Create AI Agent" });
	await expect(dialog).toBeVisible();
	return dialog;
};

const fillLlmApiKey = async (dialog: Locator) => {
	await dialog.getByLabel(/^API Key/).fill("sk-ant-e2e-placeholder-key");
	const baseUrl = dialog.getByRole("textbox", { name: /^Base URL/ });
	if ((await baseUrl.inputValue()) === "") {
		await baseUrl.fill("https://api.anthropic.com");
	}
};

// ===========================================================================
// Rule: Admin > Agents — the global agent equivalent (permission gating)
// ===========================================================================

test.describe("Admin Agents page permission gating", () => {
	test.beforeEach(async ({ request, context }) => {
		await cleanup(request);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("A user without agents.read or agents.write is redirected away from Admin > Agents", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await createRestrictedUser(
			request,
			playwright,
			"NOAGENTS",
			{},
		);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(ADMIN_AGENTS_URL);

		await expect(page).toHaveURL(/\/home/);
		await expect(page).not.toHaveURL(/\/admin\/agents/);
		await expect(
			page.getByRole("heading", { name: "Global Agents" }),
		).toHaveCount(0);
	});

	test("A user with agents.read can view the global agents list but not create agents", async ({
		page,
		request,
		playwright,
	}) => {
		const agentName = `${PREFIX}READ_ONLY_BOT`;
		await createGlobalAgent(request, agentName);
		const username = await createRestrictedUser(request, playwright, "READER", {
			"agents.read": true,
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(ADMIN_AGENTS_URL);

		await expect(
			page.getByRole("heading", { name: "Global Agents" }),
		).toBeVisible();
		await expect(page.getByText(agentName, { exact: true })).toBeVisible();
		await expect(page.getByRole("button", { name: "New Agent" })).toHaveCount(
			0,
		);
	});

	test("A user with only agents.write sees a no-permission notice but can still create agents", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await createRestrictedUser(request, playwright, "WRITER", {
			"agents.write": true,
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(ADMIN_AGENTS_URL);

		await expect(
			page.getByRole("heading", { name: "Global Agents" }),
		).toBeVisible();
		await expect(
			page.getByText("You don't have permission to view agents"),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "New Agent" })).toBeVisible();
	});
});

// ===========================================================================
// Rule: Admin > Agents — empty state and the create dialog entry points
// ===========================================================================

test.describe("Admin Agents empty state and create dialog", () => {
	test.beforeEach(async ({ request, context }) => {
		await cleanup(request);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("A user with agents.read and agents.write sees an empty state and can open the create dialog", async ({
		page,
	}) => {
		// Global agents are instance-wide and other specs (running in parallel
		// workers) create their own, so stub the list to make it deterministically empty.
		await page.route(/\/api\/v1\/admin\/agents(\?.*)?$/, (route) =>
			route.request().method() === "GET"
				? route.fulfill({ json: { success: true, data: { items: [] } } })
				: route.fallback(),
		);

		await signIn(page);
		await page.goto(ADMIN_AGENTS_URL);

		await expect(page.getByText("No global agents yet")).toBeVisible();
		await page.getByRole("button", { name: "Create agent" }).click();

		const dialog = page.getByRole("dialog", { name: "Create AI Agent" });
		await expect(dialog).toBeVisible();
		await expect(dialog.getByText("1 / 2")).toBeVisible();

		// The picker defaults to "No role" and always offers it as an option.
		const roleSelect = dialog.getByRole("combobox");
		await expect(roleSelect).toContainText("No role");
		await roleSelect.click();
		await expect(page.getByRole("option", { name: "No role" })).toBeVisible();
	});

	test("Visiting Admin Agents with ?create=true opens the create dialog immediately", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(`${ADMIN_AGENTS_URL}?create=true`);

		const dialog = page.getByRole("dialog", { name: "Create AI Agent" });
		await expect(dialog).toBeVisible();

		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page).not.toHaveURL(/create=true/);
	});
});

// ===========================================================================
// Rule: Admin > Agents — creating, deleting, and opening a global agent
// ===========================================================================

test.describe("Managing global agents", () => {
	test.beforeEach(async ({ request, context }) => {
		await cleanup(request);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("A global LLM agent appears in the grid after creation", async ({
		page,
	}) => {
		const agentName = `${PREFIX}LLM_BOT`;

		await signIn(page);
		await page.goto(ADMIN_AGENTS_URL);

		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: /^Name/ }).fill(agentName);
		await dialog.getByRole("button", { name: "Continue" }).click();
		await expect(dialog.getByText("2 / 2")).toBeVisible();
		await fillLlmApiKey(dialog);
		await dialog.getByRole("button", { name: "Create Agent" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page.getByText(agentName, { exact: true })).toBeVisible();
	});

	test("Deleting a global agent shows the global-scoped confirmation copy", async ({
		page,
		request,
	}) => {
		const agentName = `${PREFIX}TO_DELETE`;
		await createGlobalAgent(request, agentName);

		await signIn(page);
		await page.goto(ADMIN_AGENTS_URL);

		await openCardMenu(page, agentName);
		await page.getByRole("menuitem", { name: "Delete" }).click();

		const dialog = page.getByRole("dialog", { name: `Delete ${agentName}?` });
		await expect(dialog).toBeVisible();
		await expect(
			dialog.getByText(
				/removes it from every project it has been invited into/,
			),
		).toBeVisible();

		await dialog.getByRole("button", { name: "Delete", exact: true }).click();

		await expect(page.getByText(agentName, { exact: true })).toHaveCount(0);
	});

	test("A global agent card navigates to /admin/agents/:agentId, not the project route", async ({
		page,
		request,
	}) => {
		const agentName = `${PREFIX}NAV_BOT`;
		const agent = await createGlobalAgent(request, agentName);

		await signIn(page);
		await page.goto(ADMIN_AGENTS_URL);

		await page.getByText(agentName, { exact: true }).click();

		await expect(page).toHaveURL(
			new RegExp(`/admin/agents/${agent.id}(\\?.*)?$`),
		);
		await expect(page).not.toHaveURL(/\/projects\//);
	});
});
