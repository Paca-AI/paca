// spec: features/projects/chat-task-aware.feature
// seed: tests/seed.spec.ts
//
// Phase 2 of chat: task chips, /task, create-task-from-message, Inbox search
// and @agent triggers. The agent run is only checked to START (no model is
// reachable in the e2e stack, so no reply is awaited).

import { type Browser, expect, type Page, test } from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createProjectAgent,
	createUserWithProjectPermissions,
	listProjectConversationIds,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";

const TEST_PREFIX = "E2E_CHATTASK_";
const RUN_ID = newRunId();
let counter = 0;
const uniqueName = (label: string) =>
	`${TEST_PREFIX}${label}_${RUN_ID}${++counter}`;

const paragraph = (...content: unknown[]) => [{ type: "paragraph", content }];
const text = (t: string) => ({ type: "text", text: t, styles: {} });

test.describe("Task-aware chat", () => {
	let projectId: string;
	let channelId: string;

	test.beforeEach(async ({ request, page }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
		projectId = await createProject(request, uniqueName("PROJECT"));
		const created = await request.post(
			`${API_URL}/projects/${projectId}/channels`,
			{ data: { kind: "public", name: uniqueName("room").toLowerCase() } },
		);
		expect(created.ok()).toBeTruthy();
		channelId = (await created.json()).data.id as string;
		await signIn(page);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
	});

	const channelUrl = () =>
		`${BASE_URL}/projects/${projectId}/chat/${channelId}`;
	// The message box is the shared BlockNote editor, in a <fieldset> like the
	// task comment box.
	const composer = (page: Page) =>
		page.locator('fieldset [contenteditable="true"]').first();

	async function createTask(
		request: import("@playwright/test").APIRequestContext,
		title: string,
	) {
		const r = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title },
		});
		expect(r.ok()).toBeTruthy();
		return (await r.json()).data as { id: string; task_number: number };
	}

	async function post(
		request: import("@playwright/test").APIRequestContext,
		content: unknown[],
	) {
		const r = await request.post(
			`${API_URL}/projects/${projectId}/channels/${channelId}/messages`,
			{ data: { content } },
		);
		expect(r.ok()).toBeTruthy();
	}

	async function taskTitles(
		request: import("@playwright/test").APIRequestContext,
	): Promise<string[]> {
		const r = await request.get(
			`${API_URL}/projects/${projectId}/tasks?page_size=100`,
		);
		expect(r.ok()).toBeTruthy();
		const items: Array<{ title: string }> = (await r.json()).data.items ?? [];
		return items.map((t) => t.title);
	}

	test("A task chip shows its live title and opens the task", async ({
		page,
		request,
	}) => {
		const title = uniqueName("Fix login");
		const task = await createTask(request, title);
		await post(
			request,
			paragraph(text("see "), {
				type: "taskReference",
				props: {
					id: task.id,
					projectId,
					number: task.task_number,
				},
			}),
		);
		await page.goto(channelUrl());
		const chip = page.locator("[data-reference='task']").first();
		await expect(chip).toContainText(title);
		await expect(chip).toHaveAttribute(
			"href",
			new RegExp(`/projects/${projectId}/tasks/${task.id}`),
		);
	});

	test("A person who cannot read the task sees a locked chip and no title", async ({
		page,
		request,
		playwright,
		browser,
	}) => {
		const member = uniqueName("member").toLowerCase();
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username: member,
			roleName: uniqueName("ROLE"),
			permissions: { "channels:read": true, "messages:write": true },
		});
		const title = uniqueName("Secret plan");
		const task = await createTask(request, title);
		await post(
			request,
			paragraph({
				type: "taskReference",
				props: { id: task.id, projectId, number: task.task_number },
			}),
		);
		const context = await (browser as Browser).newContext();
		const other = await context.newPage();
		await signIn(other, member, RESTRICTED_PASSWORD);
		await other.goto(channelUrl());
		await expect(other.locator("[data-reference='locked']")).toBeVisible();
		await expect(other.getByText(title)).toHaveCount(0);
		await context.close();
		void page;
	});

	test("/task creates a task and reports it with a chip", async ({
		page,
		request,
	}) => {
		const title = uniqueName("Ship release");
		await page.goto(channelUrl());
		await composer(page).fill(`/task ${title}`);
		await composer(page).press("Enter");
		await expect
			.poll(async () => (await taskTitles(request)).includes(title))
			.toBe(true);
		await expect(page.locator("[data-reference='task']").first()).toContainText(
			title,
		);
	});

	test("A message becomes a task from its toolbar", async ({
		page,
		request,
	}) => {
		const title = uniqueName("Investigate flaky test");
		await post(request, paragraph(text(title)));
		await page.goto(channelUrl());
		const message = page
			.locator("[data-message-id]")
			.filter({ hasText: title });
		await message.hover();
		await message
			.getByRole("button", { name: "Create task from message" })
			.click();
		await expect
			.poll(async () => (await taskTitles(request)).includes(title))
			.toBe(true);
		// the thread opens with the chip as its reply
		await expect(
			page.getByRole("complementary").locator("[data-reference='task']"),
		).toContainText(title);
	});

	test("Messages are found from the Inbox search", async ({
		page,
		request,
	}) => {
		const phrase = `rollback${RUN_ID}plan`;
		await post(request, paragraph(text(`the ${phrase} for the migration`)));
		await page.goto(`${BASE_URL}/chat`);
		const box = page.getByLabel("Search messages");
		await box.fill("ro");
		await expect(
			page.getByText("Type at least 3 characters to search."),
		).toBeVisible();
		await box.fill(phrase.slice(0, 14));
		const hit = page.getByRole("link").filter({ hasText: phrase });
		await expect(hit).toBeVisible();
		await hit.click();
		await expect(page).toHaveURL(new RegExp(`/chat/${channelId}`));
	});

	test("Mentioning a project's agent starts a run", async ({ request }) => {
		const agent = await createProjectAgent(
			request,
			projectId,
			uniqueName("Atlas"),
		);
		const before = await listProjectConversationIds(request, projectId);
		await post(
			request,
			paragraph(
				{ type: "teamMention", props: { id: agent.id, name: agent.name } },
				text(" please review this"),
			),
		);
		await expect
			.poll(
				async () =>
					(await listProjectConversationIds(request, projectId)).length,
				{ timeout: 30_000 },
			)
			.toBeGreaterThan(before.length);
	});
});
