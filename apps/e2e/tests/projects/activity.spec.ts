// spec: features/projects/activity.feature
// seed: tests/seed.spec.ts
//
// Entries are seeded over the API (sprints and tasks record activity through
// the events stream), so the list fills in asynchronously: assertions on
// entries rely on the page's realtime refresh plus Playwright's auto-retry.

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

const TEST_PREFIX = "E2E_ACTIVITY_";
const RUN_ID = newRunId();

let counter = 0;
function uniqueName(label: string): string {
	counter += 1;
	return `${TEST_PREFIX}${label}_${RUN_ID}${counter}`;
}

// ─── API helpers ─────────────────────────────────────────────────────────────

async function createSprint(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/sprints`,
		{ data: { name, status: "planned" } },
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.id as string;
}

async function deleteSprint(
	request: APIRequestContext,
	projectId: string,
	sprintId: string,
): Promise<void> {
	const response = await request.delete(
		`${API_URL}/projects/${projectId}/sprints/${sprintId}`,
	);
	expect(response.ok()).toBeTruthy();
}

async function createTask(
	request: APIRequestContext,
	projectId: string,
	title: string,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/tasks`,
		{ data: { title } },
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.id as string;
}

/** Waits until the activity consumer has written an entry for `entityId`. */
async function waitForActivity(
	request: APIRequestContext,
	projectId: string,
	entityId: string,
	activityType: string,
): Promise<void> {
	await expect
		.poll(
			async () => {
				const response = await request.get(
					`${API_URL}/projects/${projectId}/activities?page_size=100`,
				);
				if (!response.ok()) return false;
				const items: Array<{ entity_id?: string; activity_type: string }> =
					(await response.json()).data.items ?? [];
				return items.some(
					(i) => i.entity_id === entityId && i.activity_type === activityType,
				);
			},
			{ timeout: 20_000 },
		)
		.toBe(true);
}

// ─── Page helpers ────────────────────────────────────────────────────────────

async function openActivityPage(page: Page, projectId: string): Promise<void> {
	await page.goto(`${BASE_URL}/projects/${projectId}/activity`);
	await expect(
		page.getByRole("heading", { name: "Activity", level: 1 }),
	).toBeVisible();
}

/** The list row whose text contains both `description` and `entityName`. */
function entry(page: Page, description: string, entityName: string): Locator {
	return page
		.getByRole("listitem")
		.filter({ hasText: description })
		.filter({ hasText: entityName });
}

async function openFilters(page: Page): Promise<void> {
	await page.getByRole("button", { name: "Filters" }).click();
}

// ─── Tests ───────────────────────────────────────────────────────────────────

test.describe("Project activity log", () => {
	let projectId: string;
	let sprintName: string;
	let sprintId: string;
	let taskTitle: string;
	let taskId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
		projectId = await createProject(request, uniqueName("PROJECT"));
		sprintName = uniqueName("SPRINT");
		sprintId = await createSprint(request, projectId, sprintName);
		taskTitle = uniqueName("TASK");
		taskId = await createTask(request, projectId, taskTitle);
		await waitForActivity(request, projectId, sprintId, "sprint.created");
		await waitForActivity(request, projectId, taskId, "task.created");
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
	});

	test.describe("Reading the activity log", () => {
		test("The sidebar links to the activity page", async ({
			page,
			isMobile,
		}) => {
			test.skip(isMobile, "The project sidebar is collapsed on mobile");
			await signIn(page);
			await page.goto(`${BASE_URL}/projects/${projectId}`);

			await page.getByRole("link", { name: "Activity", exact: true }).click();

			await expect(page).toHaveURL(
				new RegExp(`/projects/${projectId}/activity`),
			);
			await expect(
				page.getByRole("heading", { name: "Activity", level: 1 }),
			).toBeVisible();
			await expect(
				page.getByText("Everything that happened in this project"),
			).toBeVisible();
		});

		test("Entries recorded over the API are listed under Today", async ({
			page,
		}) => {
			await signIn(page);
			await openActivityPage(page, projectId);

			await expect(
				page.getByRole("heading", { name: "Today", level: 2 }),
			).toBeVisible();
			await expect(entry(page, "created sprint", sprintName)).toBeVisible();
			await expect(entry(page, "created this task", taskTitle)).toBeVisible();
		});

		test("An entry links to its entity", async ({ page }) => {
			await signIn(page);
			await openActivityPage(page, projectId);

			await entry(page, "created sprint", sprintName)
				.getByRole("link", { name: sprintName })
				.click();

			await expect(page).toHaveURL(
				new RegExp(`/projects/${projectId}/interactions/sprints/${sprintId}$`),
			);
		});

		test("A deleted entity is struck through and not linked", async ({
			page,
			request,
		}) => {
			await deleteSprint(request, projectId, sprintId);
			await waitForActivity(request, projectId, sprintId, "sprint.deleted");

			await signIn(page);
			await openActivityPage(page, projectId);

			const deleted = entry(page, "deleted sprint", sprintName);
			await expect(deleted).toBeVisible();
			await expect(deleted.getByRole("link", { name: sprintName })).toHaveCount(
				0,
			);
			await expect(
				deleted.getByTitle("This item has been deleted"),
			).toBeVisible();
			// The earlier "created" entry loses its link too.
			await expect(
				entry(page, "created sprint", sprintName).getByRole("link", {
					name: sprintName,
				}),
			).toHaveCount(0);
		});

		test("New activity appears without reloading", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openActivityPage(page, projectId);
			await expect(entry(page, "created sprint", sprintName)).toBeVisible();

			const lateSprint = uniqueName("LATE_SPRINT");
			await createSprint(request, projectId, lateSprint);

			await expect(entry(page, "created sprint", lateSprint)).toBeVisible({
				timeout: 20_000,
			});
		});
	});

	test.describe("Filtering", () => {
		test("Filtering by type hides other entity types", async ({ page }) => {
			await signIn(page);
			await openActivityPage(page, projectId);
			await expect(entry(page, "created this task", taskTitle)).toBeVisible();

			await openFilters(page);
			await page.getByRole("checkbox", { name: "Sprints" }).check();
			await page.keyboard.press("Escape");

			await expect(entry(page, "created sprint", sprintName)).toBeVisible();
			await expect(entry(page, "created this task", taskTitle)).toHaveCount(0);
			await expect(
				page.getByRole("button", { name: /^Filters\s*1$/ }),
			).toBeVisible();
		});

		test("Searching narrows the list", async ({ page }) => {
			await signIn(page);
			await openActivityPage(page, projectId);
			await expect(entry(page, "created sprint", sprintName)).toBeVisible();

			await page
				.getByRole("textbox", { name: "Search activity…" })
				.fill(taskTitle);

			await expect(entry(page, "created this task", taskTitle)).toBeVisible();
			await expect(entry(page, "created sprint", sprintName)).toHaveCount(0);
		});

		test("A search with no match shows the filtered empty state", async ({
			page,
		}) => {
			await signIn(page);
			await openActivityPage(page, projectId);
			await expect(entry(page, "created sprint", sprintName)).toBeVisible();

			await page
				.getByRole("textbox", { name: "Search activity…" })
				.fill(`no-such-activity-${RUN_ID}`);

			await expect(page.getByText("No matching activity")).toBeVisible();

			await page.getByRole("button", { name: "Clear all" }).click();

			await expect(
				page.getByRole("textbox", { name: "Search activity…" }),
			).toHaveValue("");
			await expect(entry(page, "created sprint", sprintName)).toBeVisible();
		});
	});

	test.describe("Permission", () => {
		test("A member without project.activities.read cannot see the log", async ({
			page,
			request,
			playwright,
			isMobile,
		}) => {
			const username = uniqueName("NOREAD").toLowerCase();
			await createUserWithProjectPermissions(request, playwright, {
				projectId,
				username,
				roleName: uniqueName("NOREAD_ROLE"),
				permissions: { "tasks.read": true },
			});

			await signIn(page, username, RESTRICTED_PASSWORD);
			if (!isMobile) {
				await page.goto(`${BASE_URL}/projects/${projectId}`);
				await expect(
					page.getByRole("link", { name: "Team", exact: true }),
				).toBeVisible();
				await expect(
					page.getByRole("link", { name: "Activity", exact: true }),
				).toHaveCount(0);
			}

			await page.goto(`${BASE_URL}/projects/${projectId}/activity`);
			await expect(
				page.getByText("You can't view the activity log"),
			).toBeVisible();
			await expect(entry(page, "created sprint", sprintName)).toHaveCount(0);
		});

		test("A member with project.activities.read can see the log", async ({
			page,
			request,
			playwright,
			isMobile,
		}) => {
			const username = uniqueName("READER").toLowerCase();
			await createUserWithProjectPermissions(request, playwright, {
				projectId,
				username,
				roleName: uniqueName("READER_ROLE"),
				permissions: { "tasks.read": true, "project.activities.read": true },
			});

			await signIn(page, username, RESTRICTED_PASSWORD);
			if (!isMobile) {
				await page.goto(`${BASE_URL}/projects/${projectId}`);
				await expect(
					page.getByRole("link", { name: "Activity", exact: true }),
				).toBeVisible();
			}

			await openActivityPage(page, projectId);
			await expect(entry(page, "created sprint", sprintName)).toBeVisible();
		});
	});
});
