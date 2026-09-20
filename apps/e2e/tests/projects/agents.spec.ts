// spec: features/projects/agents.feature
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
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createProjectAgent,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";

const PROJECT_PREFIX = "E2E_AGENTS_";
const RUN_ID = newRunId();

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
	await cleanupUsersByPrefix(request, PROJECT_PREFIX);
}

const agentsUrl = (projectId: string) =>
	`${BASE_URL}/projects/${projectId}/agents`;

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

const fillAgentName = async (dialog: Locator, name: string) => {
	await dialog.getByRole("textbox", { name: /^Name/ }).fill(name);
};

const selectFirstProjectRole = async (page: Page, dialog: Locator) => {
	await dialog.getByRole("combobox").click();
	await page.getByRole("option").first().click();
};

const continueToStep2 = async (dialog: Locator) => {
	await dialog.getByRole("button", { name: "Continue" }).click();
	await expect(dialog.getByText("2 / 2")).toBeVisible();
};

const fillLlmApiKey = async (dialog: Locator) => {
	await dialog.getByLabel(/^API Key/).fill("sk-ant-e2e-placeholder-key");
	const baseUrl = dialog.getByRole("textbox", { name: /^Base URL/ });
	if ((await baseUrl.inputValue()) === "") {
		await baseUrl.fill("https://api.anthropic.com");
	}
};

// ===========================================================================
// Rule: Project Agents page — loading, empty state, and permission-gated actions
// ===========================================================================

test.describe("Project Agents page", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanup(request);
		projectId = await createProject(
			request,
			`${PROJECT_PREFIX}PROJECT_${RUN_ID}`,
		);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("The Agents page shows a loading skeleton while agents are being fetched", async ({
		page,
	}) => {
		await signIn(page);

		// Hold the agent list response so the skeleton stays on screen.
		const listUrl = new RegExp(`/api/v1/projects/${projectId}/agents(\\?.*)?$`);
		await page.route(listUrl, async (route) => {
			if (route.request().method() !== "GET") {
				await route.continue();
				return;
			}
			await new Promise((resolve) => setTimeout(resolve, 2_000));
			await route.continue();
		});

		await page.goto(agentsUrl(projectId));

		// Skeleton cards expose no role or text — data-slot is the only handle.
		await expect(page.locator('[data-slot="skeleton"]').first()).toBeVisible();
		await expect(page.getByText("No agents yet")).not.toBeVisible();

		await expect(page.getByText("No agents yet")).toBeVisible({
			timeout: 10_000,
		});
	});

	test("A project with no agents shows an empty state", async ({ page }) => {
		await signIn(page);
		await page.goto(agentsUrl(projectId));

		await expect(page.getByText("No agents yet")).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Create your first agent" }),
		).toBeVisible();
	});

	test('The "New Agent" button is visible with agents.write and project.members.write permissions', async ({
		page,
	}) => {
		// The admin account holds every permission.
		await signIn(page);
		await page.goto(agentsUrl(projectId));

		await expect(
			page.getByRole("heading", { name: "AI Agents" }),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "New Agent" })).toBeVisible();
	});

	test('The "New Agent" button is hidden without agents.write permission', async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PROJECT_PREFIX}READER_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PROJECT_PREFIX}READ_ONLY_${RUN_ID}`,
			permissions: { "agents.read": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(agentsUrl(projectId));

		await expect(
			page.getByRole("heading", { name: "AI Agents" }),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "New Agent" })).toHaveCount(
			0,
		);
	});

	test('The "New Agent" button stays hidden with agents.write but no project.members.write', async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PROJECT_PREFIX}WRITER_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PROJECT_PREFIX}WRITE_ONLY_${RUN_ID}`,
			permissions: { "agents.read": true, "agents.write": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(agentsUrl(projectId));

		await expect(
			page.getByRole("heading", { name: "AI Agents" }),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "New Agent" })).toHaveCount(
			0,
		);
	});

	test("The per-card configure/delete menu is hidden without agents.write permission", async ({
		page,
		request,
		playwright,
	}) => {
		const agentName = `${PROJECT_PREFIX}READONLY_BOT`;
		await createProjectAgent(request, projectId, agentName);
		const username = `${PROJECT_PREFIX}READER_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PROJECT_PREFIX}READ_ONLY_${RUN_ID}`,
			permissions: { "agents.read": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(agentsUrl(projectId));

		await expect(page.getByText(agentName, { exact: true })).toBeVisible();
		await expect(agentCard(page, agentName).getByRole("button")).toHaveCount(0);
	});

	test("An agent card shows its name, handle, and provider badge", async ({
		page,
		request,
	}) => {
		const agentName = `${PROJECT_PREFIX}CARD_BOT`;
		await createProjectAgent(request, projectId, agentName, "llm", {
			llmProvider: "anthropic",
			llmModel: "claude-sonnet-4-6",
		});

		await signIn(page);
		await page.goto(agentsUrl(projectId));

		const card = agentCard(page, agentName);
		await expect(card.getByText("@e2e-agents-card-bot")).toBeVisible();
		await expect(card.getByText("anthropic", { exact: true })).toBeVisible();
		await expect(card.getByText("anthropic/claude-sonnet-4-6")).toBeVisible();
	});

	test("An ACP agent card shows a connection status dot instead of a model", async ({
		page,
		request,
	}) => {
		const agentName = `${PROJECT_PREFIX}ACP_BOT`;
		await createProjectAgent(request, projectId, agentName, "acp", {
			acpProvider: "claude-code",
		});

		await signIn(page);
		await page.goto(agentsUrl(projectId));

		const card = agentCard(page, agentName);
		await expect(card.getByText("claude-code", { exact: true })).toBeVisible();
		await expect(card.getByText("Offline")).toBeVisible();
	});
});

// ===========================================================================
// Rule: Creating an LLM-type agent
// ===========================================================================

test.describe("Creating an LLM-type agent", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context, page }) => {
		await cleanup(request);
		projectId = await createProject(
			request,
			`${PROJECT_PREFIX}CREATE_PROJECT_${RUN_ID}`,
		);
		await context.clearCookies();
		await signIn(page);
		await page.goto(agentsUrl(projectId));
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("The create dialog opens on step 1 with the LLM type selected by default", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);

		await expect(dialog.getByText("1 / 2")).toBeVisible();
		await expect(
			dialog.getByRole("button", { name: /LLM \(API key\)/ }),
		).toHaveClass(/border-primary/);
		await expect(dialog.getByText("Start from a preset")).toBeVisible();
		await expect(
			dialog.getByRole("button", { name: /Software Engineer/ }),
		).toBeVisible();
	});

	test("Step 1 requires a name, a handle, and a project role before continuing", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);
		const continueButton = dialog.getByRole("button", { name: "Continue" });

		await expect(continueButton).toBeDisabled();

		await fillAgentName(dialog, `${PROJECT_PREFIX}NEW_BOT`);
		await expect(dialog.getByRole("textbox", { name: /^Handle/ })).toHaveValue(
			"e2e-agents-new-bot",
		);
		await expect(continueButton).toBeDisabled();

		await selectFirstProjectRole(page, dialog);
		await expect(continueButton).toBeEnabled();
	});

	test("Selecting a preset pre-fills the provider, model, and system prompt", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);

		await dialog.getByRole("button", { name: /Code Reviewer/ }).click();
		await fillAgentName(dialog, `${PROJECT_PREFIX}PRESET_BOT`);
		await selectFirstProjectRole(page, dialog);
		await continueToStep2(dialog);

		await expect(dialog.getByRole("combobox").first()).toContainText(
			"anthropic",
		);
		await expect(
			dialog.getByRole("textbox", { name: /System Prompt/ }),
		).toHaveValue(/meticulous code reviewer/);
	});

	test("Step 2 requires a provider, model, base URL, and API key before creating an LLM agent", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);
		await fillAgentName(dialog, `${PROJECT_PREFIX}LLM_BOT`);
		await selectFirstProjectRole(page, dialog);
		await continueToStep2(dialog);

		const createButton = dialog.getByRole("button", { name: "Create Agent" });
		await expect(createButton).toBeDisabled();

		await fillLlmApiKey(dialog);
		await expect(createButton).toBeEnabled();
	});

	test("Creating a valid LLM agent adds it to the list and closes the dialog", async ({
		page,
	}) => {
		const agentName = `${PROJECT_PREFIX}LLM_CREATED`;
		const dialog = await openCreateDialog(page);
		await fillAgentName(dialog, agentName);
		await selectFirstProjectRole(page, dialog);
		await continueToStep2(dialog);
		await fillLlmApiKey(dialog);
		await dialog.getByRole("button", { name: "Create Agent" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page.getByText(agentName, { exact: true })).toBeVisible();
	});

	test("Cancelling step 1 discards the in-progress agent", async ({ page }) => {
		const agentName = `${PROJECT_PREFIX}CANCELLED`;
		const dialog = await openCreateDialog(page);
		await fillAgentName(dialog, agentName);
		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page.getByText(agentName, { exact: true })).toHaveCount(0);
	});
});

// ===========================================================================
// Rule: Creating an ACP-type agent and setting up its local bridge
// ===========================================================================

test.describe("Creating an ACP-type agent and setting up its local bridge", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanup(request);
		projectId = await createProject(
			request,
			`${PROJECT_PREFIX}ACP_PROJECT_${RUN_ID}`,
		);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	const selectAcpType = async (dialog: Locator) => {
		await dialog.getByRole("button", { name: /ACP \(local CLI\)/ }).click();
	};

	test("Switching to the ACP agent type hides the LLM-only fields", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(agentsUrl(projectId));

		const dialog = await openCreateDialog(page);
		await selectAcpType(dialog);
		await expect(dialog.getByText("Start from a preset")).toHaveCount(0);

		await fillAgentName(dialog, `${PROJECT_PREFIX}ACP_FIELDS`);
		await selectFirstProjectRole(page, dialog);
		await continueToStep2(dialog);

		await expect(dialog.getByText("ACP Server")).toBeVisible();
		await expect(
			dialog.getByRole("textbox", { name: /System Prompt/ }),
		).toHaveCount(0);
	});

	test("The custom ACP provider requires a shell command before continuing", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(agentsUrl(projectId));

		const dialog = await openCreateDialog(page);
		await selectAcpType(dialog);
		await fillAgentName(dialog, `${PROJECT_PREFIX}CUSTOM_ACP`);
		await selectFirstProjectRole(page, dialog);
		await continueToStep2(dialog);

		await dialog.getByRole("combobox").click();
		await page.getByRole("option", { name: "Custom…" }).click();

		const createButton = dialog.getByRole("button", { name: "Create Agent" });
		await expect(createButton).toBeDisabled();

		await dialog
			.getByRole("textbox", { name: "Command" })
			.fill("npx -y my-acp-server");
		await expect(createButton).toBeEnabled();
	});

	test("Creating an ACP agent opens the bridge setup dialog with a token already generated", async ({
		page,
	}) => {
		const agentName = `${PROJECT_PREFIX}ACP_CREATED`;
		await signIn(page);
		await page.goto(agentsUrl(projectId));

		const dialog = await openCreateDialog(page);
		await selectAcpType(dialog);
		await fillAgentName(dialog, agentName);
		await selectFirstProjectRole(page, dialog);
		await continueToStep2(dialog);
		await dialog.getByRole("button", { name: "Create Agent" }).click();

		await expect(dialog).not.toBeVisible();
		const setup = page.getByRole("dialog", {
			name: "Connect your local ACP bridge",
		});
		await expect(setup).toBeVisible();
		await expect(setup.getByText(new RegExp(agentName))).toBeVisible();

		await expect(setup.getByText("Install the ACP bridge")).toBeVisible();
		await expect(
			setup.getByText("Install the skill & connect the MCP server"),
		).toBeVisible();
		await expect(setup.getByText("Run the local bridge")).toBeVisible();

		// Token is generated as part of creation — it is shown immediately.
		await expect(
			setup.getByText("Copy this now — it won't be shown again.", {
				exact: true,
			}),
		).toBeVisible();
		await expect(
			setup.getByText(/paca-acp-bridge run --agent-id/),
		).toBeVisible();
		await expect(setup.getByText("Not connected")).toBeVisible();

		await setup.getByRole("button", { name: "Done" }).click();
		await expect(setup).not.toBeVisible();
		await expect(page.getByText(agentName, { exact: true })).toBeVisible();
	});

	test("The Local Bridge panel on an ACP agent's detail page walks through install, skill/MCP, and run steps", async ({
		page,
		request,
	}) => {
		const agent = await createProjectAgent(
			request,
			projectId,
			`${PROJECT_PREFIX}BRIDGE_BOT`,
			"acp",
		);

		await signIn(page);
		await page.goto(`${agentsUrl(projectId)}/${agent.id}`);

		await expect(page.getByText("Local Bridge", { exact: true })).toBeVisible();
		await expect(page.getByText("Install the ACP bridge")).toBeVisible();
		await expect(
			page.getByText(/curl -fsSL .*install-acp-bridge\.sh/),
		).toBeVisible();
		await expect(
			page.getByText("Install the skill & connect the MCP server"),
		).toBeVisible();
		await expect(page.getByText("Run the local bridge")).toBeVisible();
	});

	test("Generating a bridge token reveals a one-time run command", async ({
		page,
		request,
	}) => {
		const agent = await createProjectAgent(
			request,
			projectId,
			`${PROJECT_PREFIX}TOKEN_BOT`,
			"acp",
		);

		await signIn(page);
		await page.goto(`${agentsUrl(projectId)}/${agent.id}`);

		await expect(
			page.getByText("Generate a token to reveal the run command."),
		).toBeVisible();
		await page.getByRole("button", { name: "Generate token" }).click();

		await expect(
			page.getByText("Copy this now — it won't be shown again.", {
				exact: true,
			}),
		).toBeVisible();
		await expect(
			page.getByText(/paca-acp-bridge run --agent-id/),
		).toBeVisible();
		await expect(page.getByText("Not connected")).toBeVisible();
	});

	test("Generate token is disabled without agents.write permission", async ({
		page,
		request,
		playwright,
	}) => {
		const agent = await createProjectAgent(
			request,
			projectId,
			`${PROJECT_PREFIX}LOCKED_BOT`,
			"acp",
		);
		const username = `${PROJECT_PREFIX}READER_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PROJECT_PREFIX}READ_ONLY_${RUN_ID}`,
			permissions: { "agents.read": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(`${agentsUrl(projectId)}/${agent.id}`);

		await expect(
			page.getByRole("button", { name: "Generate token" }),
		).toBeDisabled();
	});
});

// ===========================================================================
// Rule: Deleting a project agent
// ===========================================================================

test.describe("Deleting a project agent", () => {
	const agentName = `${PROJECT_PREFIX}TO_DELETE`;
	let projectId: string;

	test.beforeEach(async ({ request, context, page }) => {
		await cleanup(request);
		projectId = await createProject(
			request,
			`${PROJECT_PREFIX}DELETE_PROJECT_${RUN_ID}`,
		);
		await createProjectAgent(request, projectId, agentName);
		await context.clearCookies();
		await signIn(page);
		await page.goto(agentsUrl(projectId));
		await expect(page.getByText(agentName, { exact: true })).toBeVisible();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("Deleting an agent asks for confirmation before removing it", async ({
		page,
	}) => {
		await openCardMenu(page, agentName);
		await page.getByRole("menuitem", { name: "Delete" }).click();

		const dialog = page.getByRole("dialog", { name: `Delete ${agentName}?` });
		await expect(dialog).toBeVisible();
		await dialog.getByRole("button", { name: "Delete", exact: true }).click();

		await expect(page.getByText(agentName, { exact: true })).toHaveCount(0);
	});

	test("Cancelling the delete confirmation keeps the agent", async ({
		page,
	}) => {
		await openCardMenu(page, agentName);
		await page.getByRole("menuitem", { name: "Delete" }).click();

		const dialog = page.getByRole("dialog", { name: `Delete ${agentName}?` });
		await expect(dialog).toBeVisible();
		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page.getByText(agentName, { exact: true })).toBeVisible();
	});
});
