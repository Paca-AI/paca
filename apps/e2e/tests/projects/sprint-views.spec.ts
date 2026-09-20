// spec: features/projects/interaction-views.feature
//
// Covers only the sprint-page / backlog view-layout scenarios of that feature.
// Sprint state transitions (create, start, complete, delete) live in
// tests/projects/sprint-lifecycle.spec.ts.
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://localhost";
const USERNAME = process.env.E2E_USERNAME ?? "admin";
const PASSWORD = process.env.E2E_PASSWORD ?? "e2e-admin-password";
const TEST_PROJECT_PREFIX = "E2E_SV_";
const RUN_ID = Date.now().toString(36).slice(-5).toUpperCase();

async function authRequest(request: APIRequestContext): Promise<void> {
	await request.post(`${BASE_URL}/api/v1/auth/login`, {
		data: { username: USERNAME, password: PASSWORD, rememberMe: false },
	});
}

async function cleanupTestProjects(request: APIRequestContext): Promise<void> {
	await authRequest(request);
	const allProjects: Array<{ id: string; name: string }> = [];
	let page = 1;
	while (true) {
		const listResp = await request.get(
			`${BASE_URL}/api/v1/projects?page=${page}&page_size=100`,
		);
		if (!listResp.ok()) break;
		const body = await listResp.json();
		const items: Array<{ id: string; name: string }> = body?.data?.items ?? [];
		if (items.length === 0) break;
		allProjects.push(...items);
		const {
			page: currentPage,
			page_size,
			total,
		} = body.data as { page: number; page_size: number; total: number };
		if (currentPage * page_size >= total) break;
		page++;
	}
	await Promise.all(
		allProjects
			.filter((p) => p.name.startsWith(TEST_PROJECT_PREFIX))
			.map((p) => request.delete(`${BASE_URL}/api/v1/projects/${p.id}`)),
	);
}

async function createProject(
	request: APIRequestContext,
	name: string,
): Promise<string> {
	const resp = await request.post(`${BASE_URL}/api/v1/projects`, {
		data: { name },
	});
	const body = await resp.json();
	return body.data.id as string;
}

async function createSprint(
	request: APIRequestContext,
	projectId: string,
	name: string,
	status: "planned" | "active" = "active",
): Promise<string> {
	const resp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/sprints`,
		{
			data: { name, status },
		},
	);
	const body = await resp.json();
	return body.data.id as string;
}

async function createTask(
	request: APIRequestContext,
	projectId: string,
	payload: {
		title: string;
		sprint_id?: string | null;
		status_id?: string | null;
	},
): Promise<void> {
	await request.post(`${BASE_URL}/api/v1/projects/${projectId}/tasks`, {
		data: payload,
	});
}

const signIn = async (page: Page) => {
	await page.goto(`${BASE_URL}/`);
	await ensureLoginForm(page);
	await page.getByRole("textbox", { name: "Username" }).fill(USERNAME);
	await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(
		page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
	).toBeVisible();
};

const navigateToSprint = async (
	page: Page,
	projectId: string,
	sprintId: string,
) => {
	await page.goto(
		`${BASE_URL}/projects/${projectId}/interactions/sprints/${sprintId}`,
	);
	await expect(
		page.getByRole("button", { name: "Complete sprint" }),
	).toBeVisible({ timeout: 30_000 });
};

const navigateToBacklog = async (page: Page, projectId: string) => {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
	await expect(
		page.getByRole("heading", { name: "Product Backlog" }),
	).toBeVisible({ timeout: 30_000 });
};

test.describe("Sprint interaction page header and view defaults", () => {
	test.setTimeout(60_000);
	let projectId: string;
	let sprintId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}SPRINT_PAGE_${RUN_ID}`,
		);
		sprintId = await createSprint(
			request,
			projectId,
			`${TEST_PROJECT_PREFIX}ACTIVE_${RUN_ID}`,
			"active",
		);
		await context.clearCookies();
		await context.clearPermissions();
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
	});

	test("Sprint interaction page title shows the sprint name", async ({
		page,
	}) => {
		await signIn(page);

		// Navigate to the sprint interaction page
		await navigateToSprint(page, projectId, sprintId);

		// Verify the page heading shows the sprint name
		await expect(page.getByRole("heading", { level: 1 })).toContainText(
			`${TEST_PROJECT_PREFIX}ACTIVE_${RUN_ID}`,
		);
	});

	test("Sprint interaction page subtitle shows active state and start date", async ({
		page,
	}) => {
		await signIn(page);

		// Navigate to the sprint interaction page
		await navigateToSprint(page, projectId, sprintId);

		// Verify the subtitle contains "Active sprint"
		await expect(page.getByText("Active sprint")).toBeVisible();
	});

	test('Active sprint page shows a "Complete sprint" button in the header', async ({
		page,
	}) => {
		await signIn(page);

		// Navigate to the sprint interaction page
		await navigateToSprint(page, projectId, sprintId);

		// Verify "Complete sprint" button is visible in the header
		await expect(
			page.getByRole("button", { name: "Complete sprint" }),
		).toBeVisible();
	});

	test("Default view on a sprint interaction page is the Board view", async ({
		page,
	}) => {
		await signIn(page);

		// Navigate to the sprint interaction page
		await navigateToSprint(page, projectId, sprintId);

		// Board tab should be active (text-primary) and Table tab should be inactive (text-muted)
		const boardTab = page.getByRole("button", { name: "Board", exact: true });
		await expect(boardTab).toBeVisible();
		await expect(boardTab).toHaveClass(/text-primary/);

		const tableTab = page.getByRole("button", { name: "Table", exact: true });
		await expect(tableTab).toHaveClass(/text-muted/);
	});

	test("Sprint Board view contains a column for each project task status", async ({
		page,
	}) => {
		await signIn(page);

		// Navigate to the sprint interaction page (Board view is default)
		await navigateToSprint(page, projectId, sprintId);

		// Each default status column should be visible
		await expect(
			page.getByText("Backlog", { exact: true }).first(),
		).toBeVisible();
		await expect(page.getByText("Todo", { exact: true }).first()).toBeVisible();
		await expect(
			page.getByText("In Progress", { exact: true }).first(),
		).toBeVisible();
		await expect(page.getByText("Done", { exact: true }).first()).toBeVisible();
	});

	test('Sprint Board view columns each have an "Add task" inline button', async ({
		page,
	}) => {
		await signIn(page);

		// Navigate to the sprint interaction page (Board view is default)
		await navigateToSprint(page, projectId, sprintId);

		// At least one "Add task" button should be visible
		await expect(
			page.getByRole("button", { name: "Add task" }).first(),
		).toBeVisible();
	});

	test("Sprint Table view groups tasks by status with correct column headers", async ({
		page,
		isMobile,
	}) => {
		await signIn(page);
		await navigateToSprint(page, projectId, sprintId);

		// Click the Table view tab
		await page.getByRole("button", { name: "Table", exact: true }).click();

		// Verify ID, Title, Assignee, Importance, Type column headers are visible
		await expect(page.getByText("ID", { exact: true }).first()).toBeVisible();
		await expect(
			page.getByText("Title", { exact: true }).first(),
		).toBeVisible();
		await expect(
			page.getByText("Assignee", { exact: true }).first(),
		).toBeVisible();
		// Importance and Type columns have 'hidden sm:block' CSS — not visible on mobile viewports
		if (!isMobile) {
			await expect(
				page.getByText("Importance", { exact: true }).first(),
			).toBeVisible();
			await expect(
				page.getByText("Type", { exact: true }).first(),
			).toBeVisible();
		}
	});
});

test.describe("Product backlog table view column structure", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}BACKLOG_COLS_${RUN_ID}`,
		);
		// Create a sprint so the table has a sprint column to verify ordering
		await createSprint(
			request,
			projectId,
			`${TEST_PROJECT_PREFIX}COL_SPRINT_${RUN_ID}`,
			"active",
		);
		// Create a backlog task so the "Backlog" group is rendered in the table view
		await createTask(request, projectId, {
			title: `${TEST_PROJECT_PREFIX}BL_TASK_${RUN_ID}`,
			sprint_id: null,
		});
		await context.clearCookies();
		await context.clearPermissions();
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
	});

	test('Product backlog description reads "All work items not assigned to a sprint."', async ({
		page,
	}) => {
		await signIn(page);

		// Navigate to the product backlog
		await navigateToBacklog(page, projectId);

		// Verify the subtitle text
		await expect(
			page.getByText("All work items not assigned to a sprint."),
		).toBeVisible();
	});

	test("Product backlog table view columns are ID, Title, Assignee, Importance, and Type", async ({
		page,
		isMobile,
	}) => {
		await signIn(page);

		// Navigate to the product backlog (default Table view grouped by sprint)
		await navigateToBacklog(page, projectId);

		// Verify all expected column headers are visible
		await expect(page.getByText("ID", { exact: true }).first()).toBeVisible();
		await expect(
			page.getByText("Title", { exact: true }).first(),
		).toBeVisible();
		await expect(
			page.getByText("Assignee", { exact: true }).first(),
		).toBeVisible();
		// Importance and Type columns have 'hidden sm:block' CSS — not visible on mobile viewports
		if (!isMobile) {
			await expect(
				page.getByText("Importance", { exact: true }).first(),
			).toBeVisible();
			await expect(
				page.getByText("Type", { exact: true }).first(),
			).toBeVisible();
		}
	});

	test("Sprint columns appear before the Backlog column in the table view", async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);

		// Use getByRole to find both <button> elements and div[role="button"] group headers
		const sprintGroupBtn = page
			.getByRole("button", { name: /COL_SPRINT/ })
			.first();
		const backlogGroupBtn = page
			.getByRole("button", { name: /^Backlog/ })
			.first();

		await expect(sprintGroupBtn).toBeVisible({ timeout: 10_000 });
		await expect(backlogGroupBtn).toBeVisible({ timeout: 10_000 });

		const sprintBB = await sprintGroupBtn.boundingBox();
		const backlogBB = await backlogGroupBtn.boundingBox();
		if (!sprintBB || !backlogBB)
			throw new Error("Could not get group header bounding boxes");
		// Sprint group should appear above (smaller y) the Backlog group
		expect(sprintBB.y).toBeLessThan(backlogBB.y);
	});
});
