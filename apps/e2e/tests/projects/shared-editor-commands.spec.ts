// spec: features/projects/shared-editor-commands.feature
// seed: tests/seed.spec.ts
//
// Chat and task/document comments share one editor (MessageEditor) and one
// "/" command list (useSlashCommands).

import { expect, type Page, test } from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";
import { createDoc, listDocTitles } from "../helpers/iam";

const TEST_PREFIX = "E2E_SHAREDCMD_";
const RUN_ID = newRunId();
let counter = 0;
const uniqueName = (label: string) =>
	`${TEST_PREFIX}${label}_${RUN_ID}${++counter}`;

const box = (page: Page) =>
	page.locator('fieldset [contenteditable="true"]').first();

test.describe("Shared editor commands", () => {
	let projectId: string;
	let channelId: string;
	let taskId: string;
	let docId: string;

	test.beforeEach(async ({ request, page }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		projectId = await createProject(request, uniqueName("PROJECT"));
		const channel = await request.post(
			`${API_URL}/projects/${projectId}/channels`,
			{ data: { kind: "public", name: uniqueName("room").toLowerCase() } },
		);
		expect(channel.ok()).toBeTruthy();
		channelId = (await channel.json()).data.id as string;
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: uniqueName("Host task") },
		});
		expect(task.ok()).toBeTruthy();
		taskId = (await task.json()).data.id as string;
		docId = await createDoc(request, projectId, uniqueName("Host doc"));
		await signIn(page);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
	});

	const channelUrl = () =>
		`${BASE_URL}/projects/${projectId}/chat/${channelId}`;
	const taskUrl = () => `${BASE_URL}/projects/${projectId}/tasks/${taskId}`;

	async function taskTitles(
		request: import("@playwright/test").APIRequestContext,
	) {
		const r = await request.get(
			`${API_URL}/projects/${projectId}/tasks?page_size=100`,
		);
		expect(r.ok()).toBeTruthy();
		const items: Array<{ title: string }> = (await r.json()).data.items ?? [];
		return items.map((t) => t.title);
	}

	test("The / menu lists the same commands in chat and in a task comment", async ({
		page,
	}) => {
		for (const url of [channelUrl(), taskUrl()]) {
			await page.goto(url);
			await expect(box(page)).toBeVisible({ timeout: 15_000 });
			await box(page).click();
			await page.keyboard.type("/");
			await expect(
				page.getByText("Create task", { exact: true }),
			).toBeVisible();
			await expect(
				page.getByText("Create document", { exact: true }),
			).toBeVisible();
			await page.keyboard.press("Escape");
		}
	});

	test("/doc in chat creates a document and reports it with a chip", async ({
		page,
		request,
	}) => {
		const title = uniqueName("Runbook");
		await page.goto(channelUrl());
		await box(page).fill(`/doc ${title}`);
		await box(page).press("Enter");
		await expect
			.poll(async () => (await listDocTitles(request, projectId)).titles)
			.toContain(title);
		await expect(page.locator("[data-reference='doc']").first()).toContainText(
			title,
		);
	});

	test("/task in a task comment creates a task and comments a chip", async ({
		page,
		request,
	}) => {
		const title = uniqueName("From a comment");
		await page.goto(taskUrl());
		await expect(box(page)).toBeVisible({ timeout: 15_000 });
		await box(page).fill(`/task ${title}`);
		await box(page).press("Control+Enter");
		await expect.poll(async () => await taskTitles(request)).toContain(title);
		await expect(page.locator("[data-reference='task']").first()).toContainText(
			title,
		);
	});

	test("/doc in a document comment creates a document and comments a chip", async ({
		page,
		request,
	}) => {
		const title = uniqueName("From a doc comment");
		await page.goto(`${BASE_URL}/projects/${projectId}/docs/${docId}`);
		await page.getByTitle("Comments & activity").click();
		await expect(box(page)).toBeVisible({ timeout: 15_000 });
		await box(page).fill(`/doc ${title}`);
		await box(page).press("Control+Enter");
		await expect
			.poll(async () => (await listDocTitles(request, projectId)).titles)
			.toContain(title);
		await expect(page.locator("[data-reference='doc']").first()).toContainText(
			title,
		);
	});
});
