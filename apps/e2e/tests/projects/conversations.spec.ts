// spec: features/projects/conversations.feature
// seed: tests/seed.spec.ts
//
// The e2e stack has no valid LLM credentials, so a chat conversation seeded
// through the API ends `failed` ("Authentication required") within seconds.
// Scenarios that need a specific status / trigger / event history keep the
// real conversation but override the relevant API response with page.route
// (see overrideJson); scenarios that need a live agent reply are test.fixme.
//
// A chat conversation is private to the member who started it, so the
// read-only-member scenarios return a synthetic project-shared list instead of
// relying on a conversation someone else started.
//
// The Conversations layout is a master/detail split on mobile viewports, so
// this file only runs on the desktop projects.

import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupGlobalAgentsByPrefix,
	cleanupGlobalConversationsByAgentPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalAgent,
	createProject,
	createProjectAgent,
	createUserWithProjectPermissions,
	listProjectConversationIds,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
	startGlobalChat,
	startProjectChat,
	waitForConversationTerminal,
} from "../helpers/e2e-api";

test.skip(
	({ isMobile }) => isMobile,
	"Conversations use a mobile master/detail layout; desktop only",
);

const PROJECT_PREFIX = "E2E_CONV_";
const GLOBAL_AGENT_PREFIX = "E2E_CONV_GLOBAL_";
const MEMBER_PREFIX = "E2E_CONV_MEMBER_";
const RUN_ID = newRunId();
const FIRST_MESSAGE = "E2E_CONV_FIRST_MESSAGE";
const MISSING_CONVERSATION_ID = "00000000-0000-4000-8000-000000000000";
const FAILURE_TEXT = "Sandbox crashed";

let counter = 0;

type Json = Record<string, unknown>;

function uniqueName(label: string): string {
	counter += 1;
	return `${PROJECT_PREFIX}${label}_${RUN_ID}${counter}`;
}

function escapeRegExp(text: string): string {
	return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// ─── API helpers ─────────────────────────────────────────────────────────────

async function cleanup(request: APIRequestContext): Promise<void> {
	await cleanupGlobalConversationsByAgentPrefix(request, GLOBAL_AGENT_PREFIX);
	await cleanupGlobalAgentsByPrefix(request, GLOBAL_AGENT_PREFIX);
	await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
	await cleanupUsersByPrefix(request, MEMBER_PREFIX);
}

async function seedProject(
	request: APIRequestContext,
	label: string,
): Promise<string> {
	return createProject(request, uniqueName(label));
}

async function seedAgent(
	request: APIRequestContext,
	projectId: string,
	label: string,
): Promise<{ id: string; name: string }> {
	const agent = await createProjectAgent(
		request,
		projectId,
		uniqueName(label),
		"llm",
	);
	return { id: agent.id, name: agent.name };
}

async function seedConversation(
	request: APIRequestContext,
	projectId: string,
	agentId: string,
	message = FIRST_MESSAGE,
): Promise<string> {
	return (await startProjectChat(request, projectId, agentId, message)).id;
}

async function seedEndedConversation(
	request: APIRequestContext,
	projectId: string,
	agentId: string,
): Promise<string> {
	const id = await seedConversation(request, projectId, agentId);
	await waitForConversationTerminal(
		request,
		`/projects/${projectId}/conversations/${id}`,
	);
	return id;
}

async function getConversationTitle(
	request: APIRequestContext,
	projectId: string,
	conversationId: string,
): Promise<string | null> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/conversations/${conversationId}`,
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.title ?? null;
}

async function createMember(
	request: APIRequestContext,
	playwright: Parameters<typeof createUserWithProjectPermissions>[1],
	projectId: string,
	permissions: Record<string, boolean>,
): Promise<string> {
	const username = `${MEMBER_PREFIX}${RUN_ID}_${++counter}`;
	await createUserWithProjectPermissions(request, playwright, {
		projectId,
		username,
		roleName: `${username}_role`,
		permissions,
	});
	return username;
}

// ─── Route helpers ───────────────────────────────────────────────────────────

const projectListUrl = (projectId: string) =>
	new RegExp(`/api/v1/projects/${projectId}/conversations(\\?.*)?$`);
const projectDetailUrl = (projectId: string, conversationId: string) =>
	new RegExp(`/api/v1/projects/${projectId}/conversations/${conversationId}$`);
const projectEventsUrl = (projectId: string, conversationId: string) =>
	new RegExp(
		`/api/v1/projects/${projectId}/conversations/${conversationId}/events(\\?.*)?$`,
	);
const projectHeartbeatUrl = (projectId: string, conversationId: string) =>
	new RegExp(
		`/api/v1/projects/${projectId}/conversations/${conversationId}/heartbeat$`,
	);
const GLOBAL_LIST_URL = /\/api\/v1\/agents\/conversations(\?.*)?$/;

/** Lets the real request through, then rewrites `data` of a GET response. */
async function overrideJson(
	page: Page,
	url: RegExp,
	patch: (data: Json) => Json,
): Promise<void> {
	await page.route(url, async (route) => {
		if (route.request().method() !== "GET") return route.fallback();
		const response = await route.fetch();
		const body = await response.json();
		body.data = patch(body.data as Json);
		await route.fulfill({ response, json: body });
	});
}

async function fulfillJson(
	page: Page,
	url: RegExp,
	method: string,
	status: number,
	json: Json,
): Promise<void> {
	await page.route(url, async (route) => {
		if (route.request().method() !== method) return route.fallback();
		await route.fulfill({ status, json });
	});
}

function syntheticConversation(fields: Json): Json {
	const now = new Date().toISOString();
	return {
		iteration_count: 0,
		input_tokens: 0,
		output_tokens: 0,
		total_tokens: 0,
		status: "finished",
		trigger_type: "task_assigned",
		triggered_by_member_id: "00000000-0000-4000-8000-0000000000aa",
		created_at: now,
		updated_at: now,
		...fields,
	};
}

// ─── UI helpers ──────────────────────────────────────────────────────────────

const projectConversationsUrl = (projectId: string) =>
	`${BASE_URL}/projects/${projectId}/conversations`;
const projectConversationUrl = (projectId: string, conversationId: string) =>
	`${projectConversationsUrl(projectId)}/${conversationId}`;
const GLOBAL_CONVERSATIONS_URL = `${BASE_URL}/conversations`;
const UUID = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";

function listItem(page: Page, text: string): Locator {
	return page.getByRole("link", { name: new RegExp(escapeRegExp(text)) });
}

function composerInput(page: Page): Locator {
	return page.getByRole("textbox", { name: "Message input" });
}

async function selectAgent(page: Page, agentName: string): Promise<void> {
	// The agent picker is the first combobox in the composer's action row.
	await page.getByRole("combobox").first().click();
	await page.getByRole("option", { name: agentName, exact: true }).click();
}

async function sendMessage(page: Page, text: string): Promise<void> {
	await composerInput(page).fill(text);
	await page.getByRole("button", { name: "Send message" }).click();
}

async function openItemMenu(
	page: Page,
	itemText: string,
	action: "Rename" | "Delete",
): Promise<void> {
	const item = listItem(page, itemText).locator("..");
	await item.hover();
	await item.getByRole("button", { name: "More actions" }).click();
	await page.getByRole("menuitem", { name: action, exact: true }).click();
}

async function renameVia(
	page: Page,
	itemText: string,
	title: string,
): Promise<void> {
	await openItemMenu(page, itemText, "Rename");
	const dialog = page.getByRole("dialog", { name: "Rename conversation" });
	await dialog.getByRole("textbox").fill(title);
	await dialog.getByRole("button", { name: "Rename", exact: true }).click();
}

/** The status badge sits right after the "Chat session"/"Task session" label. */
function headerBadge(page: Page, sessionLabel: string): Locator {
	return page
		.getByText(sessionLabel, { exact: true })
		.locator("xpath=following-sibling::*[1]");
}

test.afterEach(async ({ request }) => {
	await cleanup(request);
});

// ─── Project-scoped conversations list ───────────────────────────────────────

test.describe("Project-scoped conversations list", () => {
	test.beforeEach(async ({ request, page }) => {
		await authRequest(request);
		await cleanup(request);
		await signIn(page);
	});

	test("A project with no conversations shows an empty state", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_EMPTY");

		await page.goto(projectConversationsUrl(projectId));

		await expect(page.getByText("No conversations yet")).toBeVisible();
	});

	test("The list shows loading skeletons before conversations arrive", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_SKELETON");
		await page.route(projectListUrl(projectId), async (route) => {
			await new Promise((resolve) => setTimeout(resolve, 2_000));
			await route.continue();
		});

		await page.goto(projectConversationsUrl(projectId));

		await expect(page.locator('[data-slot="skeleton"]').first()).toBeVisible();
		await expect(page.getByText("No conversations yet")).toBeVisible();
	});

	test("Landing on the bare Conversations route shows the blank composer", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_BLANK");
		await seedAgent(request, projectId, "BLANK_A");
		await seedAgent(request, projectId, "BLANK_B");

		await page.goto(projectConversationsUrl(projectId));

		await expect(composerInput(page)).toBeVisible();
		await expect(composerInput(page)).toBeEnabled();
		await expect(page.getByRole("combobox").first()).toContainText(
			"Select an agent…",
		);
	});

	test("A conversation list item shows the agent name, status badge, and trigger label", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_ITEM");
		const agent = await seedAgent(request, projectId, "AGENT");
		await seedConversation(request, projectId, agent.id);

		await page.goto(projectConversationsUrl(projectId));

		const item = listItem(page, agent.name);
		await expect(item).toBeVisible();
		await expect(item).toContainText("Chat");
		await expect(item).toContainText(
			/Queued|Running|Waiting for reply|Finished|Failed|Stopped/,
		);
	});

	test("A task-assignment-triggered conversation is labelled Task", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_TASK");
		const agent = await seedAgent(request, projectId, "AGENT");
		await seedConversation(request, projectId, agent.id);
		await overrideJson(page, projectListUrl(projectId), (data) => ({
			...data,
			items: (data.items as Json[]).map((c) => ({
				...c,
				trigger_type: "task_assigned",
				triggered_by_member_id: "00000000-0000-4000-8000-0000000000aa",
			})),
		}));

		await page.goto(projectConversationsUrl(projectId));

		await expect(listItem(page, agent.name)).toContainText("Task");
	});

	test("A conversation triggered by an automation with no human actor is labelled Automation", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_AUTO");
		const agent = await seedAgent(request, projectId, "AGENT");
		await seedConversation(request, projectId, agent.id);
		await overrideJson(page, projectListUrl(projectId), (data) => ({
			...data,
			items: (data.items as Json[]).map((c) => ({
				...c,
				trigger_type: "task_assigned",
				triggered_by_member_id: null,
				actor_user_id: null,
			})),
		}));

		await page.goto(projectConversationsUrl(projectId));

		await expect(listItem(page, agent.name)).toContainText("Automation");
	});

	test("Filtering the list by agent narrows the visible conversations", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_FILTER");
		const agentA = await seedAgent(request, projectId, "FILTER_A");
		const agentB = await seedAgent(request, projectId, "FILTER_B");
		await seedConversation(request, projectId, agentA.id);
		await seedConversation(request, projectId, agentB.id);

		await page.goto(projectConversationsUrl(projectId));
		await expect(listItem(page, agentA.name)).toBeVisible();
		await expect(listItem(page, agentB.name)).toBeVisible();

		await page.getByRole("button", { name: "Filters" }).click();
		await page.getByRole("checkbox", { name: agentA.name }).check();

		await expect(listItem(page, agentA.name)).toBeVisible();
		await expect(listItem(page, agentB.name)).toHaveCount(0);
	});

	test("A filtered list with no matches shows the filtered empty state", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_NOMATCH");
		const agentA = await seedAgent(request, projectId, "NOMATCH_A");
		const agentB = await seedAgent(request, projectId, "NOMATCH_B");
		await seedConversation(request, projectId, agentB.id);

		await page.goto(projectConversationsUrl(projectId));
		await expect(listItem(page, agentB.name)).toBeVisible();

		await page.getByRole("button", { name: "Filters" }).click();
		await page.getByRole("checkbox", { name: agentA.name }).check();

		await expect(page.getByText("No matching conversations")).toBeVisible();
		await expect(page.getByText("No conversations yet")).toHaveCount(0);
	});

	test("Scrolling to the bottom of a long conversation list loads the next page", async ({
		page,
		request,
	}) => {
		const projectId = await seedProject(request, "LIST_PAGES");
		const agent = await seedAgent(request, projectId, "AGENT");
		const titleFor = (n: number) =>
			`E2E_CONV_PAGE_${String(n).padStart(2, "0")}`;
		const OLDER_TITLE = "E2E_CONV_PAGE_OLDER";

		// A real second page would need 21 seeded conversations (each dispatches
		// a sandbox), so serve two synthetic pages keyed on the cursor param.
		await page.route(projectListUrl(projectId), async (route) => {
			if (route.request().method() !== "GET") return route.fallback();
			const cursor = new URL(route.request().url()).searchParams.get("cursor");
			const items = cursor
				? [
						syntheticConversation({
							id: "00000000-0000-4000-8000-0000000000f1",
							agent_id: agent.id,
							title: OLDER_TITLE,
						}),
					]
				: Array.from({ length: 20 }, (_, i) =>
						syntheticConversation({
							id: `00000000-0000-4000-8000-0000000001${String(i).padStart(2, "0")}`,
							agent_id: agent.id,
							title: titleFor(i + 1),
						}),
					);
			await route.fulfill({
				json: {
					success: true,
					data: {
						items,
						next_cursor: cursor ? null : "page-2",
						page_size: 20,
					},
				},
			});
		});

		await page.goto(projectConversationsUrl(projectId));
		await expect(listItem(page, titleFor(1))).toBeVisible();

		await listItem(page, titleFor(20)).scrollIntoViewIfNeeded();

		await expect(listItem(page, OLDER_TITLE)).toBeVisible();
	});
});

// ─── Conversations permissions in a project ──────────────────────────────────

test.describe("Conversations permissions in a project", () => {
	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test("A member without conversations.read sees the no-permission state instead of the list", async ({
		page,
		request,
		playwright,
	}) => {
		const projectId = await seedProject(request, "PERM_NONE");
		const username = await createMember(request, playwright, projectId, {
			"tasks.read": true,
		});
		await signIn(page, username, RESTRICTED_PASSWORD);

		await page.goto(projectConversationsUrl(projectId));

		await expect(
			page.getByText("You don't have permission to view conversations"),
		).toBeVisible();
	});

	test("A member with only conversations.read can browse the list but not manage it", async ({
		page,
		request,
		playwright,
	}) => {
		const projectId = await seedProject(request, "PERM_READ");
		const username = await createMember(request, playwright, projectId, {
			"conversations.read": true,
		});
		await signIn(page, username, RESTRICTED_PASSWORD);
		await overrideJson(page, projectListUrl(projectId), (data) => ({
			...data,
			items: [
				syntheticConversation({
					id: "00000000-0000-4000-8000-0000000000e1",
					agent_id: "00000000-0000-4000-8000-0000000000e2",
					title: "E2E_CONV_PERM_SHARED",
				}),
			],
		}));

		await page.goto(projectConversationsUrl(projectId));

		const item = listItem(page, "E2E_CONV_PERM_SHARED");
		await expect(item).toBeVisible();
		await expect(
			page.getByRole("button", { name: "New conversation" }),
		).toHaveCount(0);
		await expect(
			item.locator("..").getByRole("button", { name: "More actions" }),
		).toHaveCount(0);
	});

	test("A member with only conversations.read cannot use the composer", async ({
		page,
		request,
		playwright,
	}) => {
		const projectId = await seedProject(request, "PERM_COMPOSER");
		const username = await createMember(request, playwright, projectId, {
			"conversations.read": true,
		});
		await signIn(page, username, RESTRICTED_PASSWORD);

		await page.goto(projectConversationsUrl(projectId));

		await expect(
			page.getByRole("heading", { name: "Conversations" }),
		).toBeVisible();
		await expect(composerInput(page)).toHaveCount(0);
	});
});

// ─── Starting a new project-scoped conversation ──────────────────────────────

test.describe("Starting a new project-scoped conversation", () => {
	let projectId: string;
	let agentA: { id: string; name: string };

	test.beforeEach(async ({ request, page }) => {
		await authRequest(request);
		await cleanup(request);
		projectId = await seedProject(request, "START");
		// Two agents: a project with exactly one agent auto-selects it.
		agentA = await seedAgent(request, projectId, "START_AGENT");
		await seedAgent(request, projectId, "OTHER_AGENT");
		await signIn(page);
	});

	test("The send action is disabled until an agent is selected", async ({
		page,
	}) => {
		await page.goto(projectConversationsUrl(projectId));

		await composerInput(page).fill("Please review the open pull requests");

		await expect(
			page.getByRole("button", { name: "Send message" }),
		).toBeDisabled();
	});

	test("Selecting an agent in the inline picker enables the send action", async ({
		page,
	}) => {
		await page.goto(projectConversationsUrl(projectId));

		await composerInput(page).fill("Please review the open pull requests");
		await selectAgent(page, agentA.name);

		await expect(
			page.getByRole("button", { name: "Send message" }),
		).toBeEnabled();
	});

	test("A project with a single agent auto-selects it", async ({
		page,
		request,
	}) => {
		const soloProjectId = await seedProject(request, "START_SOLO");
		await seedAgent(request, soloProjectId, "SOLO_AGENT");

		await page.goto(projectConversationsUrl(soloProjectId));
		await composerInput(page).fill("Hello");

		await expect(
			page.getByRole("button", { name: "Send message" }),
		).toBeEnabled();
	});

	test("Sending the first message starts a chat session and opens the new conversation", async ({
		page,
		request,
	}) => {
		await page.goto(projectConversationsUrl(projectId));

		await selectAgent(page, agentA.name);
		await sendMessage(page, "Please review the open pull requests");

		await expect(page).toHaveURL(
			new RegExp(`/projects/${projectId}/conversations/${UUID}$`),
		);
		await expect(
			page.getByText("Please review the open pull requests").first(),
		).toBeVisible();
		expect(await listProjectConversationIds(request, projectId)).toHaveLength(
			1,
		);
	});

	test('Clicking "New conversation" from an open conversation returns to the blank composer', async ({
		page,
		request,
	}) => {
		const conversationId = await seedConversation(
			request,
			projectId,
			agentA.id,
		);
		await page.goto(projectConversationUrl(projectId, conversationId));
		await expect(page.getByText("Chat session", { exact: true })).toBeVisible();

		await page.getByRole("button", { name: "New conversation" }).click();

		await expect(page).toHaveURL(
			new RegExp(`/projects/${projectId}/conversations(\\?.*)?$`),
		);
		await expect(composerInput(page)).toBeEnabled();
		await expect(composerInput(page)).toHaveValue("");
		await expect(page.getByRole("combobox").first()).toContainText(
			"Select an agent…",
		);
	});
});

// ─── Viewing a project conversation's timeline and controls ──────────────────

test.describe("Viewing a project conversation's timeline and controls", () => {
	let projectId: string;
	let agent: { id: string; name: string };

	test.beforeEach(async ({ request, page }) => {
		await authRequest(request);
		await cleanup(request);
		projectId = await seedProject(request, "VIEW");
		agent = await seedAgent(request, projectId, "VIEW_AGENT");
		await signIn(page);
	});

	test('Opening a conversation shows the "Chat session" header, its status badge, and its messages', async ({
		page,
		request,
	}) => {
		const conversationId = await seedEndedConversation(
			request,
			projectId,
			agent.id,
		);

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(page.getByText("Chat session", { exact: true })).toBeVisible();
		await expect(headerBadge(page, "Chat session")).toHaveText("Failed");
		await expect(page.getByText(FIRST_MESSAGE).first()).toBeVisible();
	});

	test("A running conversation shows the status badge and a Stop control", async ({
		page,
		request,
	}) => {
		const conversationId = await seedConversation(request, projectId, agent.id);
		await overrideJson(
			page,
			projectDetailUrl(projectId, conversationId),
			(data) => ({ ...data, status: "running", error_message: null }),
		);
		await fulfillJson(
			page,
			projectHeartbeatUrl(projectId, conversationId),
			"POST",
			200,
			{ success: true, data: {} },
		);

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(headerBadge(page, "Chat session")).toHaveText("Running");
		await expect(
			page.getByRole("button", { name: "Stop", exact: true }),
		).toBeVisible();
	});

	test("An ended conversation hides the Stop control", async ({
		page,
		request,
	}) => {
		const conversationId = await seedEndedConversation(
			request,
			projectId,
			agent.id,
		);

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(headerBadge(page, "Chat session")).toHaveText("Failed");
		await expect(
			page.getByRole("button", { name: "Stop", exact: true }),
		).toHaveCount(0);
	});

	test("A failed conversation with no messages shows a dedicated failure state", async ({
		page,
		request,
	}) => {
		const conversationId = await seedEndedConversation(
			request,
			projectId,
			agent.id,
		);
		await overrideJson(
			page,
			projectDetailUrl(projectId, conversationId),
			(data) => ({ ...data, status: "failed", error_message: FAILURE_TEXT }),
		);
		await fulfillJson(
			page,
			projectEventsUrl(projectId, conversationId),
			"GET",
			200,
			{
				success: true,
				data: { items: [], total: 0, next_cursor: null, prev_cursor: null },
			},
		);

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(page.getByText("Conversation failed")).toBeVisible();
		await expect(page.getByText(FAILURE_TEXT)).toBeVisible();
	});

	test("A failed conversation that already produced messages still renders its timeline", async ({
		page,
		request,
	}) => {
		const conversationId = await seedEndedConversation(
			request,
			projectId,
			agent.id,
		);
		await overrideJson(
			page,
			projectDetailUrl(projectId, conversationId),
			(data) => ({ ...data, status: "failed", error_message: FAILURE_TEXT }),
		);

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(page.getByText(FIRST_MESSAGE).first()).toBeVisible();
		await expect(page.getByText(FAILURE_TEXT)).toBeVisible();
		await expect(page.getByText("Conversation failed")).toHaveCount(0);
	});

	test("A branch name and PR link are shown when the conversation produced one", async ({
		page,
		request,
	}) => {
		const conversationId = await seedEndedConversation(
			request,
			projectId,
			agent.id,
		);
		const prUrl = "https://example.com/e2e/pull/1";
		await overrideJson(
			page,
			projectDetailUrl(projectId, conversationId),
			(data) => ({ ...data, branch_name: "feature/e2e-conv", pr_url: prUrl }),
		);

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(page.getByText("feature/e2e-conv")).toBeVisible();
		await expect(
			page.getByRole("link", { name: "PR", exact: true }),
		).toHaveAttribute("href", prUrl);
	});

	test("The composer of an ended chat conversation is not offered", async ({
		page,
		request,
	}) => {
		const conversationId = await seedEndedConversation(
			request,
			projectId,
			agent.id,
		);

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(page.getByText("Chat session", { exact: true })).toBeVisible();
		await expect(composerInput(page)).toHaveCount(0);
	});

	test("A task-triggered conversation with no chat session has no reply composer", async ({
		page,
		request,
	}) => {
		const conversationId = await seedEndedConversation(
			request,
			projectId,
			agent.id,
		);
		await overrideJson(
			page,
			projectDetailUrl(projectId, conversationId),
			(data) => ({
				...data,
				trigger_type: "task_assigned",
				chat_session_id: null,
			}),
		);

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(page.getByText("Task session", { exact: true })).toBeVisible();
		await expect(composerInput(page)).toHaveCount(0);
	});

	test("Navigating directly to a conversation that does not exist shows a not-found state", async ({
		page,
	}) => {
		await page.goto(projectConversationUrl(projectId, MISSING_CONVERSATION_ID));

		await expect(page.getByText("Conversation not found")).toBeVisible();
	});

	test.fixme("A live agent reply streams into the timeline", async () => {
		// Needs an agent with working LLM credentials (or a reachable ACP
		// bridge); the e2e stack ships none, so every run ends
		// "Authentication required" before the agent can reply.
	});
});

// ─── Renaming and deleting a project conversation ────────────────────────────

test.describe("Renaming and deleting a project conversation", () => {
	let projectId: string;
	let agent: { id: string; name: string };
	let conversationId: string;

	test.beforeEach(async ({ request, page }) => {
		await authRequest(request);
		await cleanup(request);
		projectId = await seedProject(request, "MANAGE");
		agent = await seedAgent(request, projectId, "MANAGE_AGENT");
		conversationId = await seedConversation(request, projectId, agent.id);
		await signIn(page);
	});

	async function renameViaApi(
		request: APIRequestContext,
		title: string,
	): Promise<void> {
		const response = await request.patch(
			`${API_URL}/projects/${projectId}/conversations/${conversationId}`,
			{ data: { title } },
		);
		expect(response.ok()).toBeTruthy();
	}

	test("Renaming a conversation replaces the agent name in the list with the new title", async ({
		page,
		request,
	}) => {
		await page.goto(projectConversationsUrl(projectId));
		await expect(listItem(page, agent.name)).toBeVisible();

		await renameVia(page, agent.name, "E2E_CONV_RENAMED");

		await expect(
			page.getByRole("dialog", { name: "Rename conversation" }),
		).toBeHidden();
		await expect(listItem(page, "E2E_CONV_RENAMED")).toBeVisible();
		expect(await getConversationTitle(request, projectId, conversationId)).toBe(
			"E2E_CONV_RENAMED",
		);
	});

	test("A renamed conversation keeps its title after reloading the page", async ({
		page,
		request,
	}) => {
		await renameViaApi(request, "E2E_CONV_PERSISTED");

		await page.goto(projectConversationsUrl(projectId));

		await expect(listItem(page, "E2E_CONV_PERSISTED")).toBeVisible();
	});

	test("The rename dialog opens pre-filled with the current title", async ({
		page,
		request,
	}) => {
		await renameViaApi(request, "E2E_CONV_PREFILLED");
		await page.goto(projectConversationsUrl(projectId));

		await openItemMenu(page, "E2E_CONV_PREFILLED", "Rename");

		await expect(
			page
				.getByRole("dialog", { name: "Rename conversation" })
				.getByRole("textbox"),
		).toHaveValue("E2E_CONV_PREFILLED");
	});

	test("Cancelling the rename dialog leaves the conversation unchanged", async ({
		page,
	}) => {
		await page.goto(projectConversationsUrl(projectId));
		await openItemMenu(page, agent.name, "Rename");
		const dialog = page.getByRole("dialog", { name: "Rename conversation" });
		await dialog.getByRole("textbox").fill("E2E_CONV_DISCARDED");

		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).toBeHidden();
		await expect(listItem(page, agent.name)).toBeVisible();
		await expect(listItem(page, "E2E_CONV_DISCARDED")).toHaveCount(0);
	});

	test("The rename action is disabled while the title is blank", async ({
		page,
	}) => {
		await page.goto(projectConversationsUrl(projectId));
		await openItemMenu(page, agent.name, "Rename");
		const dialog = page.getByRole("dialog", { name: "Rename conversation" });

		await dialog.getByRole("textbox").fill("");

		await expect(
			dialog.getByRole("button", { name: "Rename", exact: true }),
		).toBeDisabled();
	});

	test("The rename field accepts at most 200 characters", async ({ page }) => {
		await page.goto(projectConversationsUrl(projectId));
		await openItemMenu(page, agent.name, "Rename");
		const input = page
			.getByRole("dialog", { name: "Rename conversation" })
			.getByRole("textbox");

		// fill() would bypass maxlength; real keystrokes honour it.
		await input.pressSequentially("x".repeat(250));

		await expect(input).toHaveValue("x".repeat(200));
	});

	test("A failed rename keeps the dialog open and shows an error", async ({
		page,
	}) => {
		await fulfillJson(
			page,
			projectDetailUrl(projectId, conversationId),
			"PATCH",
			500,
			{ success: false },
		);
		await page.goto(projectConversationsUrl(projectId));

		await renameVia(page, agent.name, "E2E_CONV_WONT_SAVE");

		const dialog = page.getByRole("dialog", { name: "Rename conversation" });
		await expect(dialog).toBeVisible();
		await expect(
			dialog.getByText("Couldn't rename the conversation. Please try again."),
		).toBeVisible();
	});

	test("Deleting a conversation asks for confirmation naming the conversation", async ({
		page,
	}) => {
		await page.goto(projectConversationsUrl(projectId));

		await openItemMenu(page, agent.name, "Delete");

		const dialog = page.getByRole("dialog", { name: "Delete conversation?" });
		await expect(dialog).toBeVisible();
		await expect(dialog).toContainText(agent.name);
	});

	test("Cancelling the delete confirmation keeps the conversation", async ({
		page,
	}) => {
		await page.goto(projectConversationsUrl(projectId));
		await openItemMenu(page, agent.name, "Delete");
		const dialog = page.getByRole("dialog", { name: "Delete conversation?" });

		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).toBeHidden();
		await expect(listItem(page, agent.name)).toBeVisible();
	});

	test("Confirming the delete removes the conversation from the list", async ({
		page,
		request,
	}) => {
		await page.goto(projectConversationsUrl(projectId));
		await openItemMenu(page, agent.name, "Delete");

		await page
			.getByRole("dialog", { name: "Delete conversation?" })
			.getByRole("button", { name: "Delete", exact: true })
			.click();

		await expect(page.getByText("No conversations yet")).toBeVisible();
		const response = await request.get(
			`${API_URL}/projects/${projectId}/conversations/${conversationId}`,
		);
		expect(response.status()).toBe(404);
	});

	test("Deleting the open conversation returns to the blank composer", async ({
		page,
	}) => {
		await page.goto(projectConversationUrl(projectId, conversationId));
		await expect(page.getByText("Chat session", { exact: true })).toBeVisible();
		await openItemMenu(page, agent.name, "Delete");

		await page
			.getByRole("dialog", { name: "Delete conversation?" })
			.getByRole("button", { name: "Delete", exact: true })
			.click();

		await expect(page).toHaveURL(
			new RegExp(`/projects/${projectId}/conversations(\\?.*)?$`),
		);
		await expect(composerInput(page)).toBeVisible();
		await expect(composerInput(page)).toHaveValue("");
	});

	test("A failed delete keeps the dialog open and shows an error", async ({
		page,
	}) => {
		await fulfillJson(
			page,
			projectDetailUrl(projectId, conversationId),
			"DELETE",
			500,
			{ success: false },
		);
		await page.goto(projectConversationsUrl(projectId));
		await openItemMenu(page, agent.name, "Delete");
		const dialog = page.getByRole("dialog", { name: "Delete conversation?" });

		await dialog.getByRole("button", { name: "Delete", exact: true }).click();

		await expect(dialog).toBeVisible();
		await expect(
			dialog.getByText("Couldn't delete the conversation. Please try again."),
		).toBeVisible();
	});

	test("Navigating directly to a deleted conversation's URL shows a not-found state", async ({
		page,
		request,
	}) => {
		const response = await request.delete(
			`${API_URL}/projects/${projectId}/conversations/${conversationId}`,
		);
		expect(response.ok()).toBeTruthy();

		await page.goto(projectConversationUrl(projectId, conversationId));

		await expect(page.getByText("Conversation not found")).toBeVisible();
	});
});

// ─── Conversation title and delete API contract ──────────────────────────────

test.describe("Conversation title and delete API contract", () => {
	let projectId: string;
	let conversationId: string;
	let conversationUrl: string;

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
		projectId = await seedProject(request, "API");
		const agent = await seedAgent(request, projectId, "API_AGENT");
		conversationId = await seedConversation(request, projectId, agent.id);
		conversationUrl = `${API_URL}/projects/${projectId}/conversations/${conversationId}`;
	});

	test("A blank title is rejected", async ({ request }) => {
		const response = await request.patch(conversationUrl, {
			data: { title: "   " },
		});

		expect(response.status()).toBe(400);
		expect((await response.json()).error).toBe("title is required");
	});

	test("A title over 200 characters is rejected", async ({ request }) => {
		const response = await request.patch(conversationUrl, {
			data: { title: "a".repeat(201) },
		});

		expect(response.status()).toBe(400);
		expect((await response.json()).error).toBe("title exceeds 200 characters");
	});

	test("The 200-character limit counts characters, not bytes", async ({
		request,
	}) => {
		const title = "б".repeat(200);

		const response = await request.patch(conversationUrl, { data: { title } });

		expect(response.status()).toBe(200);
		expect(await getConversationTitle(request, projectId, conversationId)).toBe(
			title,
		);
	});

	test("A deleted conversation is soft-deleted from the user's point of view", async ({
		request,
	}) => {
		const deleted = await request.delete(conversationUrl);
		expect(deleted.ok()).toBeTruthy();

		expect((await request.get(conversationUrl)).status()).toBe(404);
		expect(await listProjectConversationIds(request, projectId)).not.toContain(
			conversationId,
		);
		expect(
			(await request.patch(conversationUrl, { data: { title: "x" } })).status(),
		).toBe(404);
	});
});

// ─── Global conversations page ───────────────────────────────────────────────

test.describe("Global conversations page", () => {
	let agent: { id: string; name: string };

	test.beforeEach(async ({ request, page }) => {
		await authRequest(request);
		await cleanup(request);
		const created = await createGlobalAgent(
			request,
			`${GLOBAL_AGENT_PREFIX}${RUN_ID}${++counter}`,
		);
		agent = { id: created.id, name: created.name };
		await signIn(page);
	});

	test("The global Conversations page lists the caller's own conversations with global agents", async ({
		page,
		request,
	}) => {
		await startGlobalChat(request, agent.id, FIRST_MESSAGE);

		await page.goto(GLOBAL_CONVERSATIONS_URL);

		await expect(listItem(page, agent.name)).toBeVisible();
	});

	test("The global Conversations page shows an empty state when the caller has no conversations", async ({
		page,
	}) => {
		await fulfillJson(page, GLOBAL_LIST_URL, "GET", 200, {
			success: true,
			data: { items: [], next_cursor: null, page_size: 20 },
		});

		await page.goto(GLOBAL_CONVERSATIONS_URL);

		await expect(page.getByText("No conversations yet")).toBeVisible();
	});

	test("Starting a new global conversation uses the chattable global agents list", async ({
		page,
		request,
	}) => {
		await createGlobalAgent(
			request,
			`${GLOBAL_AGENT_PREFIX}OTHER_${RUN_ID}${++counter}`,
		);
		await page.goto(GLOBAL_CONVERSATIONS_URL);

		await selectAgent(page, agent.name);
		await sendMessage(page, "What's on my plate today?");

		await expect(page).toHaveURL(new RegExp(`/conversations/${UUID}$`));
		await expect(
			page.getByText("What's on my plate today?").first(),
		).toBeVisible();
	});

	test("Opening a global conversation renders the same header and timeline as a project conversation", async ({
		page,
		request,
	}) => {
		const { id } = await startGlobalChat(request, agent.id, FIRST_MESSAGE);
		await waitForConversationTerminal(request, `/agents/conversations/${id}`);

		await page.goto(GLOBAL_CONVERSATIONS_URL);
		await listItem(page, agent.name).click();

		await expect(page).toHaveURL(new RegExp(`/conversations/${id}$`));
		await expect(page.getByText("Chat session", { exact: true })).toBeVisible();
		await expect(headerBadge(page, "Chat session")).toHaveText("Failed");
		await expect(page.getByText(FIRST_MESSAGE).first()).toBeVisible();
	});

	test("A global conversation can be renamed and deleted by its owner", async ({
		page,
		request,
	}) => {
		await startGlobalChat(request, agent.id, FIRST_MESSAGE);
		await page.goto(GLOBAL_CONVERSATIONS_URL);
		await expect(listItem(page, agent.name)).toBeVisible();

		await renameVia(page, agent.name, "E2E_CONV_GLOBAL_RENAMED");
		await expect(listItem(page, "E2E_CONV_GLOBAL_RENAMED")).toBeVisible();

		await openItemMenu(page, "E2E_CONV_GLOBAL_RENAMED", "Delete");
		await page
			.getByRole("dialog", { name: "Delete conversation?" })
			.getByRole("button", { name: "Delete", exact: true })
			.click();

		await expect(listItem(page, "E2E_CONV_GLOBAL_RENAMED")).toHaveCount(0);
	});

	test("Navigating directly to a global conversation that does not exist shows a not-found state", async ({
		page,
	}) => {
		await page.goto(`${GLOBAL_CONVERSATIONS_URL}/${MISSING_CONVERSATION_ID}`);

		await expect(page.getByText("Conversation not found")).toBeVisible();
	});
});
