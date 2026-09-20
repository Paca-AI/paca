// spec: features/projects/sprint-lifecycle.feature
// seed: tests/seed.spec.ts
//
// The admin account is a super_admin, so it implicitly holds every project
// permission (sprints.read / sprints.write). Scenarios about a member who
// LACKS "Manage Sprints" create a second, limited user through the admin API
// (see createUserWithProjectPermissions) and sign in as that user.
//
// A11y gap worked around here (apps/web): the Start sprint / Complete sprint /
// Edit sprint / Delete sprint modals are plain `div`s with no dialog role or
// accessible name, so they are located structurally as the `fixed inset-0`
// overlay that contains the modal's level-2 heading.

import {
	type APIRequestContext,
	expect,
	type Locator,
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

const PROJECT_PREFIX = "E2E_SPRINT_";
const USER_PREFIX = "E2E_SPRINT_MEMBER_";
const RUN_ID = newRunId();
const VIEW_ONLY_PERMISSIONS = {
	"sprints.read": true,
	"tasks.read": true,
	"views.read": true,
};

let counter = 0;

interface SprintDto {
	id: string;
	name: string;
	status: "planned" | "active" | "completed";
	goal?: string | null;
	start_date?: string | null;
	end_date?: string | null;
}

interface TaskDto {
	id: string;
	sprint_id?: string | null;
}

// ─── API helpers ─────────────────────────────────────────────────────────────

function uniqueProjectName(label: string): string {
	counter += 1;
	return `${PROJECT_PREFIX}${label}_${RUN_ID}_${counter}`;
}

async function cleanup(request: APIRequestContext): Promise<void> {
	await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
	await cleanupUsersByPrefix(request, USER_PREFIX);
}

async function createSprint(
	request: APIRequestContext,
	projectId: string,
	name: string,
	status: "planned" | "active" = "planned",
): Promise<SprintDto> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/sprints`,
		{
			data: { name, status },
		},
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data as SprintDto;
}

async function getSprintResponse(
	request: APIRequestContext,
	projectId: string,
	sprintId: string,
) {
	return request.get(`${API_URL}/projects/${projectId}/sprints/${sprintId}`);
}

async function getSprint(
	request: APIRequestContext,
	projectId: string,
	sprintId: string,
): Promise<SprintDto> {
	const response = await getSprintResponse(request, projectId, sprintId);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data as SprintDto;
}

async function listSprints(
	request: APIRequestContext,
	projectId: string,
): Promise<SprintDto[]> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/sprints`,
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.items as SprintDto[];
}

async function completeSprintViaApi(
	request: APIRequestContext,
	projectId: string,
	sprintId: string,
) {
	return request.post(
		`${API_URL}/projects/${projectId}/sprints/${sprintId}/complete`,
		{ data: {} },
	);
}

async function createCompletedSprint(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<SprintDto> {
	const sprint = await createSprint(request, projectId, name, "active");
	const response = await completeSprintViaApi(request, projectId, sprint.id);
	expect(response.ok()).toBeTruthy();
	return sprint;
}

async function statusIdByCategory(
	request: APIRequestContext,
	projectId: string,
	category: "todo" | "done",
): Promise<string> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/task-statuses`,
	);
	expect(response.ok()).toBeTruthy();
	const items: Array<{ id: string; category: string }> = (await response.json())
		.data.items;
	const match = items.find((s) => s.category === category);
	expect(match).toBeTruthy();
	return (match as { id: string }).id;
}

async function createTask(
	request: APIRequestContext,
	projectId: string,
	title: string,
	sprintId: string | null,
	statusId: string,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/tasks`,
		{
			data: { title, sprint_id: sprintId, status_id: statusId },
		},
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.id as string;
}

async function getTask(
	request: APIRequestContext,
	projectId: string,
	taskId: string,
): Promise<TaskDto> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/tasks/${taskId}`,
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data as TaskDto;
}

async function markTaskDone(
	request: APIRequestContext,
	projectId: string,
	taskId: string,
	doneStatusId: string,
): Promise<void> {
	const response = await request.patch(
		`${API_URL}/projects/${projectId}/tasks/${taskId}`,
		{ data: { status_id: doneStatusId } },
	);
	expect(response.ok()).toBeTruthy();
}

// ─── UI helpers ──────────────────────────────────────────────────────────────

async function openBacklog(page: Page, projectId: string): Promise<void> {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
	await expect(
		page.getByRole("heading", { level: 1, name: "Product Backlog" }),
	).toBeVisible({ timeout: 30_000 });
}

async function openSprintPage(
	page: Page,
	projectId: string,
	sprint: SprintDto,
): Promise<void> {
	await page.goto(
		`${BASE_URL}/projects/${projectId}/interactions/sprints/${sprint.id}`,
	);
	await expect(page.getByRole("heading", { level: 1 })).toHaveText(
		sprint.name,
		{ timeout: 30_000 },
	);
}

async function signInAsMember(page: Page, username: string): Promise<void> {
	await page.context().clearCookies();
	await signIn(page, username, RESTRICTED_PASSWORD);
}

function newMemberUsername(): string {
	counter += 1;
	return `${USER_PREFIX}${RUN_ID}_${counter}`;
}

/** The sprint's group header on the backlog Table view (a role="button" row). */
function columnHeader(page: Page, sprintName: string): Locator {
	return page
		.getByRole("button")
		.filter({ has: page.getByText(sprintName, { exact: true }) });
}

/** A modal overlay identified by its level-2 heading. */
function modal(page: Page, title: string): Locator {
	return page
		.locator("div.fixed.inset-0")
		.filter({ has: page.getByRole("heading", { level: 2, name: title }) });
}

function pageHeaderButton(page: Page, name: string): Locator {
	return page.getByRole("button", { name, exact: true });
}

function sprintUrlPattern(sprintId: string): RegExp {
	return new RegExp(`/interactions/sprints/${sprintId}$`);
}

// ─── Rule: Creating a sprint (quick create — no modal) ───────────────────────

test.describe("Creating a sprint (quick create — no modal)", () => {
	test.setTimeout(60_000);
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		await cleanup(request);
		projectId = await createProject(request, uniqueProjectName("CREATE"));
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test('Clicking "New sprint" in the page header creates a draft sprint with a default name', async ({
		page,
		request,
		isMobile,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await page.getByRole("button", { name: "New sprint" }).first().click();

		const header = columnHeader(page, "Sprint 1");
		await expect(header).toBeVisible();
		await expect(header.getByText("Draft", { exact: true })).toBeVisible();
		await expect(page.getByRole("dialog")).toHaveCount(0);
		await expect(
			page.locator("div.fixed.inset-0").getByRole("heading", { level: 2 }),
		).toHaveCount(0);

		if (!isMobile) {
			await expect(
				page.getByRole("button", { name: /Draft Sprints/ }),
			).toBeVisible();
			await expect(page.getByRole("link", { name: "Sprint 1" })).toBeVisible();
		}

		const sprints = await listSprints(request, projectId);
		expect(sprints.map((s) => [s.name, s.status])).toEqual([
			["Sprint 1", "planned"],
		]);
	});

	test("Sequential quick-creates produce incrementally numbered names", async ({
		page,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);
		const newSprint = page.getByRole("button", { name: "New sprint" }).first();

		await newSprint.click();
		await expect(columnHeader(page, "Sprint 1")).toBeVisible();

		// The next name is derived from the loaded sprint list, so wait for
		// the first sprint to render before creating the second.
		await newSprint.click();
		await expect(columnHeader(page, "Sprint 2")).toBeVisible();
		await expect(columnHeader(page, "Sprint 1")).toBeVisible();
	});

	test('The "New sprint" button is not shown without "Manage Sprints" permission', async ({
		page,
		request,
		playwright,
	}) => {
		const username = newMemberUsername();
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `E2E_SPRINT_VIEWER_${RUN_ID}_${counter}`,
			permissions: VIEW_ONLY_PERMISSIONS,
		});

		await signInAsMember(page, username);
		await openBacklog(page, projectId);

		await expect(page.getByRole("button", { name: "New sprint" })).toHaveCount(
			0,
		);
	});
});

// ─── Rule: Starting a sprint ─────────────────────────────────────────────────

test.describe("Starting a sprint", () => {
	test.setTimeout(60_000);
	const SPRINT_NAME = "E2E_SPRINT_START_ME";
	let projectId: string;
	let sprint: SprintDto;

	test.beforeEach(async ({ request }) => {
		await cleanup(request);
		projectId = await createProject(request, uniqueProjectName("START"));
		sprint = await createSprint(request, projectId, SPRINT_NAME, "planned");
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test('A draft sprint column header shows a "Start sprint" button', async ({
		page,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await expect(
			columnHeader(page, SPRINT_NAME).getByRole("button", {
				name: "Start sprint",
			}),
		).toBeVisible();
	});

	test('Clicking "Start sprint" opens the Start sprint modal with the sprint\'s fields', async ({
		page,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await columnHeader(page, SPRINT_NAME)
			.getByRole("button", { name: "Start sprint" })
			.click();

		const startModal = modal(page, "Start sprint");
		await expect(startModal).toBeVisible();
		await expect(startModal.getByLabel("Name", { exact: true })).toHaveValue(
			SPRINT_NAME,
		);
		await expect(startModal.getByLabel("Goal")).toHaveValue("");
		await expect(startModal.getByLabel("Start date")).toHaveValue(
			new Date().toISOString().slice(0, 10),
		);
		await expect(startModal.getByLabel("End date")).toHaveValue("");
	});

	test("Starting with the default values activates the sprint and opens its page", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await columnHeader(page, SPRINT_NAME)
			.getByRole("button", { name: "Start sprint" })
			.click();
		await modal(page, "Start sprint")
			.getByRole("button", { name: "Start sprint", exact: true })
			.click();

		await expect(page).toHaveURL(sprintUrlPattern(sprint.id));
		await expect(page.getByText("Active", { exact: true })).toBeVisible();
		await expect(pageHeaderButton(page, "Complete sprint")).toBeVisible();
		expect((await getSprint(request, projectId, sprint.id)).status).toBe(
			"active",
		);
	});

	test("Goal and dates entered in the modal are saved", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await columnHeader(page, SPRINT_NAME)
			.getByRole("button", { name: "Start sprint" })
			.click();
		const startModal = modal(page, "Start sprint");
		await startModal.getByLabel("Goal").fill("Deliver authentication");
		await startModal.getByLabel("Start date").fill("2026-04-14");
		await startModal.getByLabel("End date").fill("2026-04-27");
		await startModal
			.getByRole("button", { name: "Start sprint", exact: true })
			.click();

		await expect(page).toHaveURL(sprintUrlPattern(sprint.id));
		await expect(page.getByText("Deliver authentication")).toBeVisible();

		const saved = await getSprint(request, projectId, sprint.id);
		expect(saved.status).toBe("active");
		expect(saved.goal).toBe("Deliver authentication");
		expect(saved.start_date?.slice(0, 10)).toBe("2026-04-14");
		expect(saved.end_date?.slice(0, 10)).toBe("2026-04-27");
	});

	test("Renaming the sprint in the Start sprint modal updates its name", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await columnHeader(page, SPRINT_NAME)
			.getByRole("button", { name: "Start sprint" })
			.click();
		const startModal = modal(page, "Start sprint");
		await startModal
			.getByLabel("Name", { exact: true })
			.fill("E2E_SPRINT_RENAMED");
		await startModal
			.getByRole("button", { name: "Start sprint", exact: true })
			.click();

		await expect(page.getByRole("heading", { level: 1 })).toHaveText(
			"E2E_SPRINT_RENAMED",
		);
		const saved = await getSprint(request, projectId, sprint.id);
		expect(saved.name).toBe("E2E_SPRINT_RENAMED");
		expect(saved.status).toBe("active");
	});

	test("An end date before the start date is rejected in the modal", async ({
		page,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await columnHeader(page, SPRINT_NAME)
			.getByRole("button", { name: "Start sprint" })
			.click();
		const startModal = modal(page, "Start sprint");
		await startModal.getByLabel("Start date").fill("2026-04-27");
		await startModal.getByLabel("End date").fill("2026-04-14");

		await expect(
			startModal.getByText("Due date can't be before the start date."),
		).toBeVisible();
		await expect(
			startModal.getByRole("button", { name: "Start sprint", exact: true }),
		).toBeDisabled();
	});

	test("Cancelling the modal leaves the sprint as a draft", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await columnHeader(page, SPRINT_NAME)
			.getByRole("button", { name: "Start sprint" })
			.click();
		const startModal = modal(page, "Start sprint");
		await startModal.getByLabel("Goal").fill("Should not be saved");
		await startModal.getByRole("button", { name: "Cancel" }).click();

		await expect(startModal).toHaveCount(0);
		const saved = await getSprint(request, projectId, sprint.id);
		expect(saved.status).toBe("planned");
		expect(saved.goal ?? "").toBe("");
	});

	test('An active sprint column header has no "Start sprint" button', async ({
		page,
		request,
	}) => {
		await createSprint(request, projectId, "E2E_SPRINT_START_ACTIVE", "active");

		await signIn(page);
		await openBacklog(page, projectId);

		const activeHeader = columnHeader(page, "E2E_SPRINT_START_ACTIVE");
		await expect(activeHeader).toBeVisible();
		await expect(
			activeHeader.getByRole("button", { name: "Start sprint" }),
		).toHaveCount(0);
	});

	test("A draft sprint can also be started from its own page", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await openSprintPage(page, projectId, sprint);

		await pageHeaderButton(page, "Start sprint").click();
		await modal(page, "Start sprint")
			.getByRole("button", { name: "Start sprint", exact: true })
			.click();

		await expect(page).toHaveURL(sprintUrlPattern(sprint.id));
		await expect(page.getByText("Active", { exact: true })).toBeVisible();
		await expect(pageHeaderButton(page, "Complete sprint")).toBeVisible();
		await expect(pageHeaderButton(page, "Start sprint")).toHaveCount(0);
		expect((await getSprint(request, projectId, sprint.id)).status).toBe(
			"active",
		);
	});

	test('"Start sprint" is not offered without "Manage Sprints" permission', async ({
		page,
		request,
		playwright,
	}) => {
		const username = newMemberUsername();
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `E2E_SPRINT_VIEWER_${RUN_ID}_${counter}`,
			permissions: VIEW_ONLY_PERMISSIONS,
		});

		await signInAsMember(page, username);
		await openBacklog(page, projectId);

		const header = columnHeader(page, SPRINT_NAME);
		await expect(header).toBeVisible();
		await expect(
			header.getByRole("button", { name: "Start sprint" }),
		).toHaveCount(0);
	});
});

// ─── Rule: Completing a sprint ───────────────────────────────────────────────

test.describe("Completing a sprint", () => {
	test.setTimeout(60_000);
	const SPRINT_NAME = "E2E_SPRINT_COMPLETE_ME";
	const NEXT_NAME = "E2E_SPRINT_NEXT";
	let projectId: string;
	let sprint: SprintDto;
	let nextSprint: SprintDto;
	let taskIds: { one: string; two: string; done: string };
	let doneStatusId: string;

	test.beforeEach(async ({ request }) => {
		await cleanup(request);
		projectId = await createProject(request, uniqueProjectName("COMPLETE"));
		sprint = await createSprint(request, projectId, SPRINT_NAME, "active");
		nextSprint = await createSprint(request, projectId, NEXT_NAME, "planned");
		const todoStatusId = await statusIdByCategory(request, projectId, "todo");
		doneStatusId = await statusIdByCategory(request, projectId, "done");
		taskIds = {
			one: await createTask(
				request,
				projectId,
				"E2E_SPRINT_TASK_1",
				sprint.id,
				todoStatusId,
			),
			two: await createTask(
				request,
				projectId,
				"E2E_SPRINT_TASK_2",
				sprint.id,
				todoStatusId,
			),
			done: await createTask(
				request,
				projectId,
				"E2E_SPRINT_DONE_TASK",
				sprint.id,
				doneStatusId,
			),
		};
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test('The sprint page header shows the sprint name, status and a "Complete sprint" button', async ({
		page,
	}) => {
		await signIn(page);
		await openSprintPage(page, projectId, sprint);

		await expect(page.getByText("Active", { exact: true })).toBeVisible();
		await expect(pageHeaderButton(page, "Complete sprint")).toBeVisible();
	});

	test('Clicking "Complete sprint" opens a modal counting the incomplete tasks', async ({
		page,
	}) => {
		await signIn(page);
		await openSprintPage(page, projectId, sprint);

		await pageHeaderButton(page, "Complete sprint").click();

		const completeModal = modal(page, "Complete sprint");
		await expect(completeModal).toBeVisible();
		await expect(
			completeModal.getByText("2 incomplete tasks will be moved to:"),
		).toBeVisible();
	});

	test("The modal offers the product backlog and every other unfinished sprint as destinations", async ({
		page,
	}) => {
		await signIn(page);
		await openSprintPage(page, projectId, sprint);

		await pageHeaderButton(page, "Complete sprint").click();

		const completeModal = modal(page, "Complete sprint");
		await expect(
			completeModal.getByRole("radio", { name: /Product Backlog/ }),
		).toBeChecked();
		const nextOption = completeModal.getByRole("radio", {
			name: /E2E_SPRINT_NEXT/,
		});
		await expect(nextOption).toBeVisible();
		await expect(nextOption).not.toBeChecked();
		await expect(
			completeModal.getByText("Draft", { exact: true }),
		).toBeVisible();
		await expect(
			completeModal.getByRole("radio", { name: new RegExp(SPRINT_NAME) }),
		).toHaveCount(0);
	});

	test("Completing with another sprint as destination moves the incomplete tasks there", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await openSprintPage(page, projectId, sprint);

		await pageHeaderButton(page, "Complete sprint").click();
		const completeModal = modal(page, "Complete sprint");
		await completeModal.getByRole("radio", { name: /E2E_SPRINT_NEXT/ }).check();
		await completeModal
			.getByRole("button", { name: "Complete sprint", exact: true })
			.click();

		await expect(page).toHaveURL(/\/interactions\/backlog$/);
		expect((await getSprint(request, projectId, sprint.id)).status).toBe(
			"completed",
		);
		expect((await getTask(request, projectId, taskIds.one)).sprint_id).toBe(
			nextSprint.id,
		);
		expect((await getTask(request, projectId, taskIds.two)).sprint_id).toBe(
			nextSprint.id,
		);
	});

	test("Completing with the product backlog as destination unassigns the incomplete tasks", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await openSprintPage(page, projectId, sprint);

		await pageHeaderButton(page, "Complete sprint").click();
		await modal(page, "Complete sprint")
			.getByRole("button", { name: "Complete sprint", exact: true })
			.click();

		await expect(page).toHaveURL(/\/interactions\/backlog$/);
		expect((await getSprint(request, projectId, sprint.id)).status).toBe(
			"completed",
		);
		expect(
			(await getTask(request, projectId, taskIds.one)).sprint_id ?? null,
		).toBeNull();
		expect(
			(await getTask(request, projectId, taskIds.two)).sprint_id ?? null,
		).toBeNull();
	});

	test("Done tasks stay on the completed sprint", async ({ page, request }) => {
		await signIn(page);
		await openSprintPage(page, projectId, sprint);

		await pageHeaderButton(page, "Complete sprint").click();
		await modal(page, "Complete sprint")
			.getByRole("button", { name: "Complete sprint", exact: true })
			.click();

		await expect(page).toHaveURL(/\/interactions\/backlog$/);
		expect((await getTask(request, projectId, taskIds.done)).sprint_id).toBe(
			sprint.id,
		);
	});

	test('A completed sprint moves to the collapsed "Completed Sprints" sidebar section', async ({
		page,
		isMobile,
	}) => {
		test.skip(isMobile, "The project sidebar is a drawer on mobile viewports");

		await signIn(page);
		await openSprintPage(page, projectId, sprint);
		await pageHeaderButton(page, "Complete sprint").click();
		await modal(page, "Complete sprint")
			.getByRole("button", { name: "Complete sprint", exact: true })
			.click();
		await expect(page).toHaveURL(/\/interactions\/backlog$/);

		const section = page.getByRole("button", { name: /Completed Sprints\s*1/ });
		await expect(section).toBeVisible();
		await expect(page.getByRole("link", { name: SPRINT_NAME })).toHaveCount(0);

		await section.click();
		await expect(page.getByRole("link", { name: SPRINT_NAME })).toBeVisible();
	});

	test("Cancelling the modal leaves the sprint active", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await openSprintPage(page, projectId, sprint);

		await pageHeaderButton(page, "Complete sprint").click();
		const completeModal = modal(page, "Complete sprint");
		await completeModal.getByRole("button", { name: "Cancel" }).click();

		await expect(completeModal).toHaveCount(0);
		expect((await getSprint(request, projectId, sprint.id)).status).toBe(
			"active",
		);
		expect((await getTask(request, projectId, taskIds.one)).sprint_id).toBe(
			sprint.id,
		);
	});

	test("A sprint with no incomplete tasks shows a confirmation message and no destinations", async ({
		page,
		request,
	}) => {
		await markTaskDone(request, projectId, taskIds.one, doneStatusId);
		await markTaskDone(request, projectId, taskIds.two, doneStatusId);

		await signIn(page);
		await openSprintPage(page, projectId, sprint);
		await pageHeaderButton(page, "Complete sprint").click();

		const completeModal = modal(page, "Complete sprint");
		await expect(
			completeModal.getByText("No incomplete tasks remain in this sprint."),
		).toBeVisible();
		await expect(completeModal.getByRole("radio")).toHaveCount(0);
		await expect(
			completeModal.getByRole("button", {
				name: "Complete sprint",
				exact: true,
			}),
		).toBeVisible();
		await expect(
			completeModal.getByRole("button", { name: "Cancel" }),
		).toBeVisible();
	});

	test('"Complete sprint" is not offered without "Manage Sprints" permission', async ({
		page,
		request,
		playwright,
	}) => {
		const username = newMemberUsername();
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `E2E_SPRINT_VIEWER_${RUN_ID}_${counter}`,
			permissions: VIEW_ONLY_PERMISSIONS,
		});

		await signInAsMember(page, username);
		await openSprintPage(page, projectId, sprint);

		await expect(pageHeaderButton(page, "Complete sprint")).toHaveCount(0);
	});
});

// ─── Rule: Sprint lifecycle state constraints ────────────────────────────────

test.describe("Sprint lifecycle state constraints", () => {
	test.setTimeout(60_000);
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		await cleanup(request);
		projectId = await createProject(request, uniqueProjectName("STATE"));
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	async function deleteThroughEditModal(page: Page): Promise<void> {
		await pageHeaderButton(page, "Edit sprint").click();
		await modal(page, "Edit sprint")
			.getByRole("button", { name: "Delete sprint" })
			.click();
	}

	test("Multiple sprints can be active at the same time", async ({
		page,
		request,
	}) => {
		const activeA = await createSprint(
			request,
			projectId,
			"E2E_SPRINT_ACTIVE_A",
			"active",
		);
		const draftB = await createSprint(
			request,
			projectId,
			"E2E_SPRINT_DRAFT_B",
			"planned",
		);

		await signIn(page);
		await openBacklog(page, projectId);
		await columnHeader(page, "E2E_SPRINT_DRAFT_B")
			.getByRole("button", { name: "Start sprint" })
			.click();

		const startModal = modal(page, "Start sprint");
		await expect(
			startModal.getByText(
				'"E2E_SPRINT_ACTIVE_A" is already active in this project. Starting this sprint won\'t stop it.',
			),
		).toBeVisible();
		await startModal
			.getByRole("button", { name: "Start sprint", exact: true })
			.click();

		await expect(page).toHaveURL(sprintUrlPattern(draftB.id));
		expect((await getSprint(request, projectId, activeA.id)).status).toBe(
			"active",
		);
		expect((await getSprint(request, projectId, draftB.id)).status).toBe(
			"active",
		);
	});

	test("A draft sprint can be deleted and its tasks return to the product backlog", async ({
		page,
		request,
	}) => {
		const draft = await createSprint(
			request,
			projectId,
			"E2E_SPRINT_DELETE_DRAFT",
			"planned",
		);
		const taskId = await createTask(
			request,
			projectId,
			"E2E_SPRINT_ORPHAN",
			draft.id,
			await statusIdByCategory(request, projectId, "todo"),
		);

		await signIn(page);
		await openSprintPage(page, projectId, draft);
		await deleteThroughEditModal(page);

		const confirm = modal(page, "Delete sprint?");
		await expect(confirm).toBeVisible();
		await expect(confirm.getByText(/E2E_SPRINT_DELETE_DRAFT/)).toBeVisible();
		await confirm.getByRole("button", { name: "Delete sprint" }).click();

		await expect(page).toHaveURL(/\/interactions\/backlog$/);
		expect(
			(await getSprintResponse(request, projectId, draft.id)).status(),
		).toBe(404);
		expect(
			(await getTask(request, projectId, taskId)).sprint_id ?? null,
		).toBeNull();
	});

	test("Cancelling the delete confirmation keeps the sprint", async ({
		page,
		request,
	}) => {
		const draft = await createSprint(
			request,
			projectId,
			"E2E_SPRINT_KEEP_DRAFT",
			"planned",
		);

		await signIn(page);
		await openSprintPage(page, projectId, draft);
		await deleteThroughEditModal(page);

		const confirm = modal(page, "Delete sprint?");
		await confirm.getByRole("button", { name: "Cancel" }).click();

		await expect(confirm).toHaveCount(0);
		expect((await getSprintResponse(request, projectId, draft.id)).ok()).toBe(
			true,
		);
	});

	test("An active sprint can be deleted too", async ({ page, request }) => {
		const active = await createSprint(
			request,
			projectId,
			"E2E_SPRINT_DELETE_ACTIVE",
			"active",
		);

		await signIn(page);
		await openSprintPage(page, projectId, active);
		await deleteThroughEditModal(page);
		await modal(page, "Delete sprint?")
			.getByRole("button", { name: "Delete sprint" })
			.click();

		await expect(page).toHaveURL(/\/interactions\/backlog$/);
		expect(
			(await getSprintResponse(request, projectId, active.id)).status(),
		).toBe(404);
	});

	test("A completed sprint can be opened but cannot be started or completed again", async ({
		page,
		request,
	}) => {
		const done = await createCompletedSprint(
			request,
			projectId,
			"E2E_SPRINT_DONE",
		);

		await signIn(page);
		await openSprintPage(page, projectId, done);

		await expect(page.getByText("Completed", { exact: true })).toBeVisible();
		await expect(pageHeaderButton(page, "Start sprint")).toHaveCount(0);
		await expect(pageHeaderButton(page, "Complete sprint")).toHaveCount(0);
	});

	test("Completing an already-completed sprint through the API is rejected", async ({
		request,
	}) => {
		const done = await createCompletedSprint(
			request,
			projectId,
			"E2E_SPRINT_DONE_API",
		);

		const again = await completeSprintViaApi(request, projectId, done.id);

		expect(again.ok()).toBe(false);
		expect((await getSprint(request, projectId, done.id)).status).toBe(
			"completed",
		);
	});
});
