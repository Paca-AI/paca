// spec: features/iam/list-scoping.feature
// seed: tests/seed.spec.ts
//
// A route gate on a project cannot hide single rows, so list endpoints apply the
// caller's scope inside the SQL query, before pagination and counts. These
// scenarios check that pages, totals and cursors add up for a member limited to
// one sprint's tasks, and that the workspace home (open-task count, "assigned
// to me") only counts and lists the tasks the member may read.

import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	newRunId,
	projectRoleIdByName,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import {
	allow,
	asUser,
	createMember,
	createSprint,
	createTask,
	dataOf,
	listAllTaskTitles,
	listTasks,
	openProject,
	projectRoleFrom,
} from "../helpers/iam";

const PREFIX = "E2E_IAMLIST_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
}

/** A role that reads and writes only the tasks of the given sprints. */
function sprintTasksRole(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
	...sprintIds: string[]
) {
	return projectRoleFrom(
		request,
		projectId,
		roleName,
		openProject(projectId, ["tasks:read", "tasks:write"]),
		allow(["tasks:read", "tasks:write"], [`project/${projectId}/task/*`], {
			In: { "task.sprint_id": sprintIds },
		}),
	);
}

async function workspaceOpenTasks(api: APIRequestContext): Promise<number> {
	const response = await api.get(`${API_URL}/projects/workspace-stats`);
	expect(response.status()).toBe(200);
	return (await dataOf<{ open_task_count: number }>(response)).open_task_count;
}

async function assignedToMe(api: APIRequestContext): Promise<string[]> {
	const titles: string[] = [];
	let cursor: string | null = null;
	for (let i = 0; i < 20; i++) {
		const response = await api.get(
			`${API_URL}/users/me/tasks?page_size=2${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
		);
		expect(response.status()).toBe(200);
		const data = await dataOf<{
			items: Array<{ title: string }>;
			next_cursor: string | null;
		}>(response);
		titles.push(...data.items.map((t) => t.title));
		cursor = data.next_cursor;
		if (!cursor) break;
	}
	return titles.sort();
}

test.describe("Pages and counts under a scoped role", () => {
	let projectId: string;
	let sprint1: string;
	let sprint2: string;

	test.beforeEach(async ({ request, context }) => {
		await authRequest(request);
		await cleanup(request);
		await context.clearCookies();
		projectId = await createProject(request, name("PAGES"));
		sprint1 = await createSprint(request, projectId, name("S1"));
		sprint2 = await createSprint(request, projectId, name("S2"));
		for (let i = 1; i <= 5; i++) {
			await createTask(request, projectId, `S1-${i}`, sprint1);
		}
		for (let i = 1; i <= 4; i++) {
			await createTask(request, projectId, `S2-${i}`, sprint2);
		}
		for (let i = 1; i <= 3; i++) {
			await createTask(request, projectId, `BL-${i}`);
		}
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test("Every page and the total count only the tasks of the sprint", async ({
		request,
		playwright,
	}) => {
		const role = await sprintTasksRole(
			request,
			projectId,
			name("S1ONLY"),
			sprint1,
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("PAGED"),
			[role],
		);

		await asUser(playwright, member.username, async (api) => {
			const first = await listTasks(api, projectId, "page_size=2");
			expect(first.status).toBe(200);
			expect(first.ids).toHaveLength(2);
			expect(first.total).toBe(5);
			expect(first.nextCursor).not.toBeNull();

			// Walk the cursor: three pages (2 + 2 + 1), the total never drifts and
			// no task of another sprint or of the backlog ever shows up.
			const all = await listAllTaskTitles(api, projectId, 2);
			expect(all.pages).toBe(3);
			expect(all.total).toBe(5);
			expect(all.titles).toEqual(["S1-1", "S1-2", "S1-3", "S1-4", "S1-5"]);

			// A page size larger than the scope returns it whole, with no cursor.
			const whole = await listTasks(api, projectId, "page_size=50");
			expect(whole.ids).toHaveLength(5);
			expect(whole.nextCursor).toBeNull();
		});
	});

	test("The admin and a scoped member each see their own totals for the same project", async ({
		request,
		playwright,
	}) => {
		const role = await sprintTasksRole(
			request,
			projectId,
			name("S2ONLY"),
			sprint2,
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("S2MEMBER"),
			[role],
		);
		const viewer = await createMember(
			request,
			playwright,
			projectId,
			name("FULL"),
			[await projectRoleIdByName(request, projectId, "Viewer")],
		);

		expect((await listTasks(request, projectId)).total).toBe(12);
		await asUser(playwright, viewer.username, async (api) => {
			expect((await listTasks(api, projectId)).total).toBe(12);
		});
		await asUser(playwright, member.username, async (api) => {
			const page = await listTasks(api, projectId);
			expect(page.total).toBe(4);
			expect(page.titles).toEqual(["S2-1", "S2-2", "S2-3", "S2-4"]);
		});
	});

	test("Two scoped roles on one member add up their sprints", async ({
		request,
		playwright,
	}) => {
		const first = await sprintTasksRole(
			request,
			projectId,
			name("FIRST"),
			sprint1,
		);
		const second = await sprintTasksRole(
			request,
			projectId,
			name("SECOND"),
			sprint2,
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("BOTH"),
			[first, second],
		);

		await asUser(playwright, member.username, async (api) => {
			const all = await listAllTaskTitles(api, projectId, 4);
			expect(all.total).toBe(9);
			expect(all.titles).toHaveLength(9);
			expect(all.titles.filter((t) => t.startsWith("BL-"))).toEqual([]);
		});
	});

	test("Filters and search narrow the scope further, never widen it", async ({
		request,
		playwright,
	}) => {
		const role = await sprintTasksRole(
			request,
			projectId,
			name("S1FILTER"),
			sprint1,
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("FILTERED"),
			[role],
		);

		await asUser(playwright, member.username, async (api) => {
			// Search matches titles in every sprint ("-1" is in S1-1, S2-1, BL-1)
			// but only the allowed one is returned.
			const search = await listTasks(api, projectId, "search=-1");
			expect(search.titles).toEqual(["S1-1"]);
			expect(search.total).toBe(1);
			// A sprint filter naming several sprints still only yields the allowed one.
			const several = await listTasks(
				api,
				projectId,
				`sprint_ids=${sprint1},${sprint2}`,
			);
			expect(several.total).toBe(5);
			expect(several.titles.every((t) => t.startsWith("S1-"))).toBe(true);
		});
	});
});

test.describe("The workspace home respects a scoped role", () => {
	let projectId: string;
	let sprint1: string;
	let sprint2: string;
	let memberName: string;
	let doneStatusId: string;
	let firstTaskId: string;

	test.beforeEach(async ({ request, playwright, context }) => {
		await authRequest(request);
		await cleanup(request);
		await context.clearCookies();
		projectId = await createProject(request, name("HOME"));
		sprint1 = await createSprint(request, projectId, name("S1"));
		sprint2 = await createSprint(request, projectId, name("S2"));
		const role = await sprintTasksRole(
			request,
			projectId,
			name("S1HOME"),
			sprint1,
		);
		memberName = name("HOMEMEMBER");
		const member = await createMember(
			request,
			playwright,
			projectId,
			memberName,
			[role],
		);

		// Tasks are assigned by project member id. The member is assigned to two
		// tasks of the allowed sprint and to one task of another sprint.
		const assign = async (title: string, sprintId: string) => {
			const response = await request.post(
				`${API_URL}/projects/${projectId}/tasks`,
				{
					data: { title, sprint_id: sprintId, assignee_ids: [member.memberId] },
				},
			);
			expect(response.ok()).toBeTruthy();
			return (await dataOf<{ id: string }>(response)).id;
		};
		firstTaskId = await assign("Mine in S1 A", sprint1);
		await assign("Mine in S1 B", sprint1);
		await assign("Mine in S2", sprint2);
		await createTask(request, projectId, "Unassigned in S1", sprint1);
		await createTask(request, projectId, "Unassigned in S2", sprint2);

		const statuses = await dataOf<{
			items: Array<{ id: string; category: string }>;
		}>(await request.get(`${API_URL}/projects/${projectId}/task-statuses`));
		doneStatusId = statuses.items.find((s) => s.category === "done")?.id ?? "";
		expect(doneStatusId, "the project has a done status").not.toBe("");
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test("The open-task count only counts tasks the member may read", async ({
		request,
		playwright,
	}) => {
		await asUser(playwright, memberName, async (api) => {
			// Three tasks in the allowed sprint; the two in the other one are not counted.
			expect(await workspaceOpenTasks(api)).toBe(3);
		});

		// Finishing a task takes it out of the open count.
		const done = await request.patch(
			`${API_URL}/projects/${projectId}/tasks/${firstTaskId}`,
			{ data: { status_id: doneStatusId } },
		);
		expect(done.ok()).toBeTruthy();
		await asUser(playwright, memberName, async (api) => {
			expect(await workspaceOpenTasks(api)).toBe(2);
		});
	});

	test("'Assigned to me' lists only assigned tasks inside the member's scope", async ({
		playwright,
	}) => {
		await asUser(playwright, memberName, async (api) => {
			expect(await assignedToMe(api)).toEqual(["Mine in S1 A", "Mine in S1 B"]);
		});
	});

	test("The home page shows the scoped open-task count and assigned tasks", async ({
		page,
	}) => {
		await signIn(page, memberName, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/home`);

		const stat = page
			.getByText("Open Tasks", { exact: true })
			.locator("xpath=ancestor::*[@data-slot='card'][1]");
		await expect(stat.getByText("3", { exact: true })).toBeVisible();

		await expect(page.getByText("Mine in S1 A")).toBeVisible();
		await expect(page.getByText("Mine in S1 B")).toBeVisible();
		await expect(page.getByText("Mine in S2")).toHaveCount(0);
	});
});
