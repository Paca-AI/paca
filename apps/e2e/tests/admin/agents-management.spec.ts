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
	authRequest,
	BASE_URL,
	bindGlobalAgentRole,
	cleanupGlobalAgentsByPrefix,
	cleanupGlobalRolesByPrefix,
	cleanupUsersByPrefix,
	createGlobalAgent,
	createUserWithGlobalPermissions,
	globalRoleIdByName,
	listGlobalAgents,
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
		// A global agent's role is a third step of its own (this account may
		// assign roles), not a field of the first step.
		await expect(dialog.getByText("1 / 3")).toBeVisible();
		await expect(dialog.getByRole("combobox")).toHaveCount(0);
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
		await expect(dialog.getByText("2 / 3")).toBeVisible();
		await fillLlmApiKey(dialog);
		await dialog.getByRole("button", { name: "Continue" }).click();

		// The role is optional: keeping "No global role" creates the agent as it is.
		await expect(dialog.getByText("3 / 3")).toBeVisible();
		await expect(
			dialog.getByRole("radio", { name: "No global role" }),
		).toBeChecked();
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

// ===========================================================================
// Rule: Creating a global agent — the global role is a third, optional step
// ===========================================================================

// The API refuses a role on create: it is its own privilege (global_roles.assign)
// and its own request, made once the agent exists. The wizard makes it a step of
// its own, offered only to someone who may assign roles.

const findGlobalAgent = async (request: APIRequestContext, name: string) => {
	await authRequest(request);
	return (await listGlobalAgents(request)).find((a) => a.name === name);
};

test.describe("Creating a global agent with a role", () => {
	test.beforeEach(async ({ request, context }) => {
		await cleanup(request);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("The role is a third step that defaults to no role and lists the real roles", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(ADMIN_AGENTS_URL);

		const dialog = await openCreateDialog(page);
		await expect(
			dialog.getByText("Set up your agent's identity"),
		).toBeVisible();
		await dialog
			.getByRole("textbox", { name: /^Name/ })
			.fill(`${PREFIX}ROLE_STEP`);
		await dialog.getByRole("button", { name: "Continue" }).click();
		await fillLlmApiKey(dialog);
		await dialog.getByRole("button", { name: "Continue" }).click();

		await expect(dialog.getByText("3 / 3")).toBeVisible();
		await expect(
			dialog.getByText("Choose a global role for the agent (optional)"),
		).toBeVisible();
		const roles = dialog.getByRole("radiogroup", { name: "Global role" });
		await expect(
			roles.getByRole("radio", { name: "No global role" }),
		).toBeChecked();
		await expect(
			roles.getByRole("radio", { name: "ADMIN", exact: true }),
		).toBeVisible();
		await expect(
			roles.getByRole("radio", { name: "SUPER_ADMIN", exact: true }),
		).toBeVisible();

		// Backing out is free: nothing has been created yet.
		await dialog.getByRole("button", { name: "Back" }).click();
		await expect(dialog.getByText("2 / 3")).toBeVisible();
	});

	test("A global agent created with a role is bound to it once it exists", async ({
		page,
		request,
	}) => {
		const agentName = `${PREFIX}WITH_ROLE`;
		await signIn(page);
		await page.goto(ADMIN_AGENTS_URL);

		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: /^Name/ }).fill(agentName);
		await dialog.getByRole("button", { name: "Continue" }).click();
		await fillLlmApiKey(dialog);
		await dialog.getByRole("button", { name: "Continue" }).click();
		await dialog.getByRole("radio", { name: "ADMIN", exact: true }).check();
		await dialog.getByRole("button", { name: "Create Agent" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page.getByText(agentName, { exact: true })).toBeVisible();
		const agent = await findGlobalAgent(request, agentName);
		expect(agent?.global_role_id).toBe(
			await globalRoleIdByName(request, "ADMIN"),
		);
	});

	test("Keeping 'No global role' creates the agent without one", async ({
		page,
		request,
	}) => {
		const agentName = `${PREFIX}NO_ROLE`;
		await signIn(page);
		await page.goto(ADMIN_AGENTS_URL);

		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: /^Name/ }).fill(agentName);
		await dialog.getByRole("button", { name: "Continue" }).click();
		await fillLlmApiKey(dialog);
		await dialog.getByRole("button", { name: "Continue" }).click();
		await dialog.getByRole("button", { name: "Create Agent" }).click();

		await expect(dialog).not.toBeVisible();
		const agent = await findGlobalAgent(request, agentName);
		expect(agent).toBeTruthy();
		expect(agent?.global_role_id ?? null).toBeNull();
	});

	test("A user who may write agents but not assign roles creates them in two steps", async ({
		page,
		request,
		playwright,
	}) => {
		const agentName = `${PREFIX}TWO_STEPS`;
		const username = await createRestrictedUser(
			request,
			playwright,
			"TWOSTEP",
			{
				"agents.write": true,
			},
		);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(ADMIN_AGENTS_URL);

		const dialog = await openCreateDialog(page);
		await expect(dialog.getByText("1 / 2")).toBeVisible();
		await dialog.getByRole("textbox", { name: /^Name/ }).fill(agentName);
		await dialog.getByRole("button", { name: "Continue" }).click();
		await expect(dialog.getByText("2 / 2")).toBeVisible();
		await fillLlmApiKey(dialog);
		// No role step: the second step is the last, and creates.
		await expect(dialog.getByRole("button", { name: "Continue" })).toHaveCount(
			0,
		);
		await dialog.getByRole("button", { name: "Create Agent" }).click();

		await expect(dialog).not.toBeVisible();
		const agent = await findGlobalAgent(request, agentName);
		expect(agent).toBeTruthy();
		expect(agent?.global_role_id ?? null).toBeNull();
	});

	test("A user who may also assign roles gets the third step", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await createRestrictedUser(
			request,
			playwright,
			"THREESTEP",
			{
				"agents.write": true,
				"global_roles.read": true,
				"global_roles.assign": true,
			},
		);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(ADMIN_AGENTS_URL);

		const dialog = await openCreateDialog(page);
		await expect(dialog.getByText("1 / 3")).toBeVisible();
	});
});

// ===========================================================================
// Rule: A global agent's Global role tab
// ===========================================================================

test.describe("Global agent role tab", () => {
	test.beforeEach(async ({ request, context }) => {
		await cleanup(request);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("Assigns, changes and removes the role, each as its own request", async ({
		page,
		request,
	}) => {
		const agent = await createGlobalAgent(request, `${PREFIX}ROLE_TAB`);
		await signIn(page);
		await page.goto(`${ADMIN_AGENTS_URL}/${agent.id}#global-role`);

		await expect(page.getByText("No global role")).toBeVisible();
		await expect(
			page.getByText("This agent has no global permissions."),
		).toBeVisible();

		// Assign
		await page.getByRole("button", { name: "Assign role" }).click();
		await expect(
			page.getByRole("button", { name: "Assign role" }),
		).toBeDisabled();
		await page.getByRole("radio", { name: "ADMIN", exact: true }).check();
		await page.getByRole("button", { name: "Assign role" }).click();
		await expect(page.getByText("ADMIN", { exact: true })).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Change role" }),
		).toBeVisible();
		expect((await findGlobalAgent(request, agent.name))?.global_role_id).toBe(
			await globalRoleIdByName(request, "ADMIN"),
		);

		// Change
		await page.getByRole("button", { name: "Change role" }).click();
		const current = page.getByRole("radio", { name: "ADMIN", exact: true });
		await expect(current).toBeChecked();
		await expect(current).toHaveAccessibleDescription(/Current/);
		await page.getByRole("radio", { name: "USER", exact: true }).check();
		await page.getByRole("button", { name: "Assign role" }).click();
		await expect(
			page.getByRole("button", { name: "Change role" }),
		).toBeVisible();
		expect((await findGlobalAgent(request, agent.name))?.global_role_id).toBe(
			await globalRoleIdByName(request, "USER"),
		);

		// Remove: asks first
		await page.getByRole("button", { name: "Remove role" }).click();
		await expect(
			page.getByText(/It will lose the permissions the role grants/),
		).toBeVisible();
		await page.getByRole("button", { name: "Remove role" }).click();
		await expect(page.getByText("No global role")).toBeVisible();
		expect(
			(await findGlobalAgent(request, agent.name))?.global_role_id ?? null,
		).toBeNull();
	});

	test("Flags a full-access role before it is assigned, and cancelling changes nothing", async ({
		page,
		request,
	}) => {
		const agent = await createGlobalAgent(request, `${PREFIX}ROLE_TAB_FULL`);
		await signIn(page);
		await page.goto(`${ADMIN_AGENTS_URL}/${agent.id}#global-role`);

		await page.getByRole("button", { name: "Assign role" }).click();
		await page.getByRole("radio", { name: "SUPER_ADMIN", exact: true }).check();
		await expect(
			page.getByText(/The agent will be able to do everything/),
		).toBeVisible();
		await page.getByRole("button", { name: "Cancel" }).click();

		await expect(page.getByText("No global role")).toBeVisible();
		expect(
			(await findGlobalAgent(request, agent.name))?.global_role_id ?? null,
		).toBeNull();
	});

	test("Is read-only for someone who can view the agent's role but not change it", async ({
		page,
		request,
		playwright,
	}) => {
		const agent = await createGlobalAgent(request, `${PREFIX}ROLE_TAB_RO`);
		await bindGlobalAgentRole(request, agent.id, "ADMIN");
		const username = await createRestrictedUser(
			request,
			playwright,
			"ROLEREADER",
			{
				"agents.read": true,
				"global_roles.read": true,
			},
		);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(`${ADMIN_AGENTS_URL}/${agent.id}#global-role`);

		await expect(page.getByText("ADMIN", { exact: true })).toBeVisible();
		await expect(
			page.getByText(
				/You can see this agent's global role but don't have permission to change it/,
			),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "Change role" })).toHaveCount(
			0,
		);
		await expect(page.getByRole("button", { name: "Remove role" })).toHaveCount(
			0,
		);
	});

	test("Is read-only without global_roles.assign even for someone who may write agents", async ({
		page,
		request,
		playwright,
	}) => {
		const agent = await createGlobalAgent(
			request,
			`${PREFIX}ROLE_TAB_NOASSIGN`,
		);
		const username = await createRestrictedUser(
			request,
			playwright,
			"NOASSIGN",
			{
				"agents.read": true,
				"agents.write": true,
				"global_roles.read": true,
			},
		);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(`${ADMIN_AGENTS_URL}/${agent.id}#global-role`);

		await expect(page.getByText("No global role")).toBeVisible();
		await expect(
			page.getByText(/don't have permission to change it/),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "Assign role" })).toHaveCount(
			0,
		);
	});
});
