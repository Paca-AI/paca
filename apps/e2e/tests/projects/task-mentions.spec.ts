// spec: features/projects/task-mentions.feature
// seed: tests/seed.spec.ts
//
// The admin comments (over the API) and a project member reads what arrives:
// first the notification's JSON, then the cards, filters, badges and divider
// in the UI. Notifications are written by the API's synchronous comment path,
// but badges and cards refresh through realtime, so UI assertions rely on
// Playwright's auto-retry.

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { loginContext } from "../helpers/profile-users";
import { closeAll, track } from "../helpers/realtime";

const TEST_PREFIX = "E2E_MENTION_";
const RUN_ID = newRunId();

let counter = 0;
const uniqueName = (label: string) =>
	`${TEST_PREFIX}${label}_${RUN_ID}${++counter}`;

interface ApiNotification {
	id: string;
	type: string;
	task_id: string | null;
	comment_id: string | null;
	mention_source: string;
	excerpt: string;
	sprint_id: string | null;
	sprint_name: string;
	task_status_name: string;
	task_number: number;
	task_title: string;
	project_name: string;
	read_at: string | null;
}

const mentionBlocks = (userId: string, name: string, text: string) => [
	{
		type: "paragraph",
		content: [
			{ type: "text", text: "Hey ", styles: {} },
			{ type: "teamMention", props: { id: userId, name, avatar: "" } },
			{ type: "text", text: ` ${text}`, styles: {} },
		],
	},
];

const plainBlocks = (text: string) => [
	{ type: "paragraph", content: [{ type: "text", text, styles: {} }] },
];

async function comment(
	request: APIRequestContext,
	projectId: string,
	taskId: string,
	content: unknown[],
): Promise<string> {
	const res = await request.post(
		`${API_URL}/projects/${projectId}/tasks/${taskId}/activities/comments`,
		{ data: { content } },
	);
	expect(res.ok()).toBeTruthy();
	return (await res.json()).data.id as string;
}

async function listNotifications(
	member: APIRequestContext,
	query = "",
): Promise<ApiNotification[]> {
	const res = await member.get(`${API_URL}/users/me/notifications${query}`);
	expect(res.ok()).toBeTruthy();
	return (await res.json()).data.items as ApiNotification[];
}

async function waitForNotifications(
	member: APIRequestContext,
	count: number,
	query = "",
): Promise<ApiNotification[]> {
	await expect
		.poll(async () => (await listNotifications(member, query)).length, {
			timeout: 15_000,
		})
		.toBeGreaterThanOrEqual(count);
	return listNotifications(member, query);
}

test.describe("Mentions that say where", () => {
	let projectId: string;
	let sprintId: string;
	let sprintName: string;
	let taskId: string;
	let taskTitle: string;
	let backlogTaskId: string;
	let member: { userId: string; username: string };
	let memberApi: APIRequestContext;

	test.beforeEach(async ({ request, playwright }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
		projectId = await createProject(request, uniqueName("PROJECT"));

		sprintName = uniqueName("SPRINT");
		const sprint = await request.post(
			`${API_URL}/projects/${projectId}/sprints`,
			{ data: { name: sprintName, status: "planned" } },
		);
		expect(sprint.ok()).toBeTruthy();
		sprintId = (await sprint.json()).data.id as string;

		taskTitle = uniqueName("TASK");
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: taskTitle, sprint_id: sprintId },
		});
		expect(task.ok()).toBeTruthy();
		taskId = (await task.json()).data.id as string;
		const backlog = await request.post(
			`${API_URL}/projects/${projectId}/tasks`,
			{ data: { title: uniqueName("BACKLOG") } },
		);
		backlogTaskId = (await backlog.json()).data.id as string;

		const username = uniqueName("member").toLowerCase();
		const { userId } = await createUserWithProjectPermissions(
			request,
			playwright,
			{
				projectId,
				username,
				roleName: uniqueName("ROLE"),
				permissions: {
					"tasks:read": true,
					"tasks:write": true,
					// the sprint page, for the sprint-header badge
					"sprints:read": true,
				},
			},
		);
		member = { userId, username };
		memberApi = track(await loginContext(playwright, username));
	});

	test.afterEach(async ({ request }) => {
		await closeAll();
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
	});

	async function signInMember(page: Page) {
		await signIn(page, member.username, RESTRICTED_PASSWORD);
	}

	test.describe("The mention card", () => {
		test("A comment mention carries its context over the API", async ({
			request,
		}) => {
			const commentId = await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "can you look?"),
			);
			const [n] = await waitForNotifications(memberApi, 1);
			expect(n).toMatchObject({
				type: "mentioned",
				task_id: taskId,
				comment_id: commentId,
				mention_source: "comment",
				sprint_id: sprintId,
				sprint_name: sprintName,
				task_title: taskTitle,
				read_at: null,
			});
			expect(n.excerpt).toContain(`@${member.username}`);
			expect(n.excerpt).toContain("can you look?");
			expect(n.task_status_name).not.toBe("");
		});

		test("A task in the backlog has no sprint", async ({ request }) => {
			await comment(
				request,
				projectId,
				backlogTaskId,
				mentionBlocks(member.userId, member.username, "backlog item"),
			);
			const [n] = await waitForNotifications(memberApi, 1);
			expect(n.sprint_id).toBeNull();
			expect(n.sprint_name).toBe("");
		});

		test("The excerpt is cut at 140 characters", async ({ request }) => {
			await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "word ".repeat(80)),
			);
			const [n] = await waitForNotifications(memberApi, 1);
			expect([...n.excerpt].length).toBeLessThanOrEqual(141);
			expect(n.excerpt.endsWith("…")).toBe(true);
		});

		test("The /notifications page shows the card", async ({
			page,
			request,
		}) => {
			await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "please review"),
			);
			const [n] = await waitForNotifications(memberApi, 1);
			await signInMember(page);
			await page.goto(`${BASE_URL}/notifications`);

			const card = page.getByRole("button", { name: /mentioned you/ });
			await expect(card).toBeVisible();
			await expect(card).toContainText(n.project_name);
			await expect(card).toContainText(sprintName);
			await expect(card).toContainText(`#${n.task_number} ${taskTitle}`);
			await expect(card).toContainText(n.task_status_name);
			await expect(card.locator("mark")).toHaveText(`@${member.username}`);
		});

		test("A task without a sprint says Backlog", async ({ page, request }) => {
			await comment(
				request,
				projectId,
				backlogTaskId,
				mentionBlocks(member.userId, member.username, "hi"),
			);
			await waitForNotifications(memberApi, 1);
			await signInMember(page);
			await page.goto(`${BASE_URL}/notifications`);
			await expect(page.getByTestId("sprint-chip")).toHaveText("Backlog");
		});

		test("Several mentions on one task collapse into one card", async ({
			page,
			request,
		}) => {
			for (const text of ["first", "second"]) {
				await comment(
					request,
					projectId,
					taskId,
					mentionBlocks(member.userId, member.username, text),
				);
			}
			const items = await waitForNotifications(memberApi, 2);
			await signInMember(page);
			await page.goto(`${BASE_URL}/notifications`);
			await expect(
				page.getByText(`2 mentions in #${items[0].task_number} — ${taskTitle}`),
			).toBeVisible();
			await expect(page.locator("ul.divide-y > li")).toHaveCount(1);
		});

		test("Clicking the card opens the task at the comment", async ({
			page,
			request,
		}) => {
			const commentId = await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "over to you"),
			);
			await waitForNotifications(memberApi, 1);
			await signInMember(page);
			await page.goto(`${BASE_URL}/notifications`);

			await page.getByRole("button", { name: /mentioned you/ }).click();

			await expect(page).toHaveURL(
				new RegExp(`/tasks/${taskId}\\?comment=${commentId}`),
			);
			const entry = page.locator(`[data-entry-id="${commentId}"]`);
			await expect(entry).toBeVisible();
			await expect(entry).toHaveClass(/ring-primary/);
			// Opening the task read the mention.
			await expect
				.poll(
					async () =>
						(await listNotifications(memberApi)).every((x) => x.read_at),
					{ timeout: 10_000 },
				)
				.toBe(true);
		});
	});

	test.describe("Mentions in a description", () => {
		test("A description mention creates an in-app row", async ({ request }) => {
			const res = await request.patch(
				`${API_URL}/projects/${projectId}/tasks/${taskId}`,
				{
					data: {
						description: mentionBlocks(
							member.userId,
							member.username,
							"owns this",
						),
					},
				},
			);
			expect(res.ok()).toBeTruthy();
			const [n] = await waitForNotifications(memberApi, 1);
			expect(n).toMatchObject({
				type: "mentioned",
				mention_source: "description",
				task_id: taskId,
				comment_id: null,
			});
		});
	});

	test.describe("Filters", () => {
		test("The type, actor and project filters narrow the list", async ({
			request,
		}) => {
			await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "ping"),
			);
			// An assignment, as a second notification of another type.
			const assign = await request.patch(
				`${API_URL}/projects/${projectId}/tasks/${backlogTaskId}`,
				{ data: { assignee_ids: [await memberIdOf(request, projectId)] } },
			);
			expect(assign.ok()).toBeTruthy();
			await waitForNotifications(memberApi, 2);

			const mentions = await listNotifications(memberApi, "?type=mentioned");
			expect(mentions.map((n) => n.type)).toEqual(["mentioned"]);
			const assigned = await listNotifications(memberApi, "?type=assigned");
			expect(assigned.map((n) => n.type)).toEqual(["assigned"]);
			expect(await listNotifications(memberApi, "?actor=agent")).toHaveLength(
				0,
			);
			expect(
				await listNotifications(
					memberApi,
					"?project_id=00000000-0000-4000-8000-000000000000",
				),
			).toHaveLength(0);
			const bad = await memberApi.get(
				`${API_URL}/users/me/notifications?type=bogus`,
			);
			expect(bad.status()).toBe(400);
		});

		test("The Mentions and Assigned tabs filter the page", async ({
			page,
			request,
		}) => {
			await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "ping"),
			);
			await request.patch(
				`${API_URL}/projects/${projectId}/tasks/${backlogTaskId}`,
				{ data: { assignee_ids: [await memberIdOf(request, projectId)] } },
			);
			await waitForNotifications(memberApi, 2);
			await signInMember(page);
			await page.goto(`${BASE_URL}/notifications`);
			await expect(page.getByText(/assigned you/)).toBeVisible();
			await expect(page.getByText(/mentioned you/)).toBeVisible();

			await page.getByRole("button", { name: "Mentions", exact: true }).click();
			await expect(page.getByText(/mentioned you/)).toBeVisible();
			await expect(page.getByText(/assigned you/)).toHaveCount(0);

			await page.getByRole("button", { name: "Assigned", exact: true }).click();
			await expect(page.getByText(/assigned you/)).toBeVisible();
			await expect(page.getByText(/mentioned you/)).toHaveCount(0);
		});

		test("The project filter narrows the page", async ({ page, request }) => {
			await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "ping"),
			);
			await waitForNotifications(memberApi, 1);
			await signInMember(page);
			await page.goto(`${BASE_URL}/notifications`);
			await expect(page.getByText(/mentioned you/)).toBeVisible();

			const filter = page.getByLabel("Filter by project");
			const options = await filter.locator("option").allTextContents();
			// The member sees only the project they belong to.
			expect(options.length).toBe(2);
			await filter.selectOption({ index: 1 });
			await expect(page.getByText(/mentioned you/)).toBeVisible();
		});
	});

	test.describe("Unread cues", () => {
		test("The task row shows a badge until the task is opened", async ({
			page,
			request,
		}) => {
			await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "ping"),
			);
			await waitForNotifications(memberApi, 1);
			await signInMember(page);
			await page.goto(
				`${BASE_URL}/projects/${projectId}/interactions/sprints/${sprintId}`,
			);

			await expect(page.getByTestId("task-mention-badge")).toHaveCount(1);

			await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
			await expect(page.getByText(taskTitle).first()).toBeVisible();
			await expect
				.poll(
					async () =>
						(await listNotifications(memberApi)).every((x) => x.read_at),
					{ timeout: 10_000 },
				)
				.toBe(true);

			await page.goto(
				`${BASE_URL}/projects/${projectId}/interactions/sprints/${sprintId}`,
			);
			await expect(page.getByText(taskTitle).first()).toBeVisible();
			await expect(page.getByTestId("task-mention-badge")).toHaveCount(0);
		});

		test("New activity since the last visit gets a divider", async ({
			page,
			request,
		}) => {
			await comment(request, projectId, taskId, plainBlocks("before you came"));
			await signInMember(page);

			// First visit: nothing is "new since" a visit that never happened.
			await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
			await expect(page.getByText("before you came")).toBeVisible();
			await expect(page.getByTestId("unread-divider")).toHaveCount(0);

			// Leave, let the visit be recorded, then new comments arrive.
			await page.goto(`${BASE_URL}/projects/${projectId}`);
			await comment(
				request,
				projectId,
				taskId,
				plainBlocks("while you were away"),
			);

			await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
			const divider = page.getByTestId("unread-divider");
			await expect(divider).toBeVisible();
			await expect(divider).toContainText("New since your last visit");
			// The divider sits above the new comment, not the old one.
			const before = await page.getByText("before you came").boundingBox();
			const after = await page.getByText("while you were away").boundingBox();
			const line = await divider.boundingBox();
			expect(before && line && before.y < line.y).toBe(true);
			expect(after && line && line.y < after.y).toBe(true);
		});
	});

	test.describe("Arriving while online", () => {
		test("A toast announces a mention", async ({ page, request }) => {
			await signInMember(page);
			// Mentions arrive over the socket: wait until this page has one.
			const socketOpened = page.waitForEvent("websocket");
			await page.goto(`${BASE_URL}/projects/${projectId}`);
			await socketOpened;
			await page.waitForTimeout(1_500);

			await comment(
				request,
				projectId,
				taskId,
				mentionBlocks(member.userId, member.username, "toast me"),
			);

			await expect(
				page.getByText(new RegExp(`mentioned you in #\\d+ — ${taskTitle}`)),
			).toBeVisible({ timeout: 15_000 });
			await expect(page.getByText(sprintName).first()).toBeVisible();
			await expect(page.getByText(/toast me/).first()).toBeVisible();

			await page.getByRole("button", { name: "Open", exact: true }).click();
			await expect(page).toHaveURL(new RegExp(`/tasks/${taskId}\\?comment=`));
		});
	});
});

/** The admin's view of the member's project-member id (assignees are member ids). */
async function memberIdOf(
	request: APIRequestContext,
	projectId: string,
): Promise<string> {
	const res = await request.get(
		`${API_URL}/projects/${projectId}/members?page_size=100`,
	);
	expect(res.ok()).toBeTruthy();
	const items = (await res.json()).data.items as Array<{
		id: string;
		username: string;
	}>;
	const found = items.find((m) =>
		m.username.startsWith(TEST_PREFIX.toLowerCase()),
	);
	expect(found, "the member is in the project").toBeTruthy();
	return (found as { id: string }).id;
}
