// spec: features/projects/subtask-hierarchy.feature
// seed: tests/seed.spec.ts

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_SUB_";
const RUN_ID = newRunId();

// ─── API helpers ─────────────────────────────────────────────────────────────

interface Named {
	id: string;
	name: string;
	category?: string;
	is_system?: boolean;
}

async function listNamed(
	request: APIRequestContext,
	projectId: string,
	path: "task-statuses" | "task-types",
): Promise<Named[]> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/${path}`,
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.items as Named[];
}

interface ProjectKit {
	projectId: string;
	todoId: string;
	doneId: string;
	/** A normal (non-system) task type. */
	taskTypeId: string;
	epicTypeId: string;
}

async function setupProject(
	request: APIRequestContext,
	suffix: string,
): Promise<ProjectKit> {
	const projectId = await createProject(
		request,
		`${PREFIX}${suffix}_${RUN_ID}`,
	);
	const statuses = await listNamed(request, projectId, "task-statuses");
	const types = await listNamed(request, projectId, "task-types");
	const todo = statuses.find((s) => s.category === "todo") ?? statuses[0];
	const done = statuses.find((s) => s.category === "done");
	const normal = types.find((t) => !t.is_system);
	const epic = types.find((t) => t.is_system && t.name === "Epic");
	if (!done || !normal || !epic) {
		throw new Error("project is missing a done status or a task type");
	}
	return {
		projectId,
		todoId: todo.id,
		doneId: done.id,
		taskTypeId: normal.id,
		epicTypeId: epic.id,
	};
}

async function postTask(
	request: APIRequestContext,
	projectId: string,
	data: Record<string, unknown>,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/tasks`,
		{
			data,
		},
	);
	expect(response.ok(), await response.text()).toBeTruthy();
	return (await response.json()).data.id as string;
}

/** "Parent" with subtasks "Child done" (done), "Child todo A", "Child todo B". */
async function seedParent(
	request: APIRequestContext,
	kit: ProjectKit,
	extra: Record<string, unknown> = {},
): Promise<string> {
	const parentId = await postTask(request, kit.projectId, {
		title: "Parent",
		task_type_id: kit.taskTypeId,
		status_id: kit.todoId,
		...extra,
	});
	for (const [title, status] of [
		["Child done", kit.doneId],
		["Child todo A", kit.todoId],
		["Child todo B", kit.todoId],
	] as const) {
		await postTask(request, kit.projectId, {
			title,
			task_type_id: kit.taskTypeId,
			status_id: status,
			parent_task_id: parentId,
		});
	}
	return parentId;
}

interface ListedTask {
	id: string;
	title: string;
	subtask_count?: number;
	subtask_done_count?: number;
}

async function listTasks(
	request: APIRequestContext,
	projectId: string,
	query: string,
): Promise<ListedTask[]> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/tasks?page_size=100&${query}`,
	);
	expect(response.ok(), await response.text()).toBeTruthy();
	return (await response.json()).data.items as ListedTask[];
}

// ─── UI helpers ──────────────────────────────────────────────────────────────

async function openBacklogView(
	page: Page,
	projectId: string,
	view: "Table" | "Board",
): Promise<void> {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
	await expect(
		page.getByRole("heading", { name: "Product Backlog" }),
	).toBeVisible({ timeout: 30_000 });
	await page.getByRole("button", { name: view, exact: true }).click();
}

async function openTimeline(page: Page, projectId: string): Promise<void> {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/timeline`);
	await expect(page.getByRole("heading", { name: "Timeline" })).toBeVisible({
		timeout: 30_000,
	});
}

const expandButton = (page: Page, count: number) =>
	page.getByRole("button", { name: `Expand ${count} subtasks`, exact: true });
const collapseButton = (page: Page, count: number) =>
	page.getByRole("button", { name: `Collapse ${count} subtasks`, exact: true });
const title = (page: Page, text: string) =>
	page.getByText(text, { exact: true });

async function openViewSettings(page: Page): Promise<void> {
	await page
		.getByRole("button", { name: "View settings", exact: true })
		.click();
	await expect(page.getByText("View settings", { exact: true })).toBeVisible();
}

const settingRow = (page: Page, label: string) =>
	page
		.locator("label")
		.filter({ hasText: new RegExp(`^${label}$`) })
		.locator("xpath=following-sibling::button");

/** Picks `option` for the "Subtasks" setting and saves it for the current user. */
async function chooseSubtaskMode(page: Page, option: string): Promise<void> {
	await openViewSettings(page);
	await settingRow(page, "Subtasks").click();
	await page.getByRole("option", { name: option, exact: true }).last().click();
	await page.getByRole("button", { name: "Save only for me" }).click();
	await expect(
		page.getByRole("button", { name: "Save only for me" }),
	).not.toBeVisible({ timeout: 5_000 });
}

test.beforeEach(async ({ request }) => {
	await cleanupProjectsByPrefix(request, PREFIX);
	await authRequest(request);
});

test.afterEach(async ({ request }) => {
	await cleanupProjectsByPrefix(request, PREFIX);
});

// ===========================================================================
// Rule: The task list API reports subtask counts and lists children
// ===========================================================================

test.describe("The task list API reports subtask counts and lists children", () => {
	let kit: ProjectKit;
	let parentId: string;

	test.beforeEach(async ({ request }) => {
		kit = await setupProject(request, "API");
		parentId = await seedParent(request, kit);
		await postTask(request, kit.projectId, {
			title: "Lone",
			task_type_id: kit.taskTypeId,
		});
		const goneId = await postTask(request, kit.projectId, {
			title: "Child gone",
			task_type_id: kit.taskTypeId,
			status_id: kit.doneId,
			parent_task_id: parentId,
		});
		const deleted = await request.delete(
			`${API_URL}/projects/${kit.projectId}/tasks/${goneId}`,
		);
		expect(deleted.ok()).toBeTruthy();
	});

	test("A parent's list entry carries its subtask counts", async ({
		request,
	}) => {
		const tasks = await listTasks(request, kit.projectId, "");
		const parent = tasks.find((t) => t.title === "Parent");
		expect(parent?.subtask_count).toBe(3);
		expect(parent?.subtask_done_count).toBe(1);
		const lone = tasks.find((t) => t.title === "Lone");
		expect(lone?.subtask_count ?? 0).toBe(0);
		expect(lone?.subtask_done_count ?? 0).toBe(0);
	});

	test("Listing by parent returns only that parent's children", async ({
		request,
	}) => {
		const children = await listTasks(
			request,
			kit.projectId,
			`parent_task_id=${parentId}`,
		);
		expect(children.map((t) => t.title).sort()).toEqual([
			"Child done",
			"Child todo A",
			"Child todo B",
		]);
	});

	test("exclude_subtasks hides subtasks but keeps tasks of an Epic", async ({
		request,
	}) => {
		const epicId = await postTask(request, kit.projectId, {
			title: "Epic",
			task_type_id: kit.epicTypeId,
		});
		await postTask(request, kit.projectId, {
			title: "In epic",
			task_type_id: kit.taskTypeId,
			parent_task_id: epicId,
		});
		const titles = (
			await listTasks(request, kit.projectId, "exclude_subtasks=true")
		)
			.map((t) => t.title)
			.sort();
		expect(titles).toEqual(["Epic", "In epic", "Lone", "Parent"]);
	});

	test("Children can be filtered by status", async ({ request }) => {
		const children = await listTasks(
			request,
			kit.projectId,
			`parent_task_id=${parentId}&status_ids=${kit.doneId}`,
		);
		expect(children.map((t) => t.title)).toEqual(["Child done"]);
	});

	test("Listing children ignores the sprint filter", async ({ request }) => {
		const sprintResponse = await request.post(
			`${API_URL}/projects/${kit.projectId}/sprints`,
			{ data: { name: `${PREFIX}SPRINT_${RUN_ID}` } },
		);
		expect(sprintResponse.ok()).toBeTruthy();
		const sprintId = (await sprintResponse.json()).data.id as string;
		const move = await request.patch(
			`${API_URL}/projects/${kit.projectId}/tasks/${parentId}`,
			{ data: { sprint_id: sprintId } },
		);
		expect(move.ok(), await move.text()).toBeTruthy();
		const children = await listTasks(
			request,
			kit.projectId,
			`parent_task_id=${parentId}&sprint_id=${sprintId}`,
		);
		expect(children).toHaveLength(3);
	});
});

// ===========================================================================
// Rule: The product backlog Table nests subtasks under their parents
// ===========================================================================

test.describe("The product backlog Table nests subtasks under their parents", () => {
	let kit: ProjectKit;

	test.beforeEach(async ({ request, page }) => {
		kit = await setupProject(request, "TABLE");
		await seedParent(request, kit);
		await signIn(page);
		await openBacklogView(page, kit.projectId, "Table");
	});

	test("A parent shows its progress and starts collapsed", async ({ page }) => {
		await expect(title(page, "Parent")).toBeVisible();
		await expect(expandButton(page, 3)).toBeVisible();
		await expect(page.getByText("1/3", { exact: true })).toBeVisible();
		await expect(title(page, "Child done")).toHaveCount(0);
	});

	test("Expanding a parent shows its subtasks and collapsing hides them", async ({
		page,
	}) => {
		await expandButton(page, 3).click();
		for (const child of ["Child done", "Child todo A", "Child todo B"]) {
			await expect(title(page, child)).toBeVisible();
		}
		await collapseButton(page, 3).click();
		await expect(title(page, "Child done")).toHaveCount(0);
	});

	test("A task without subtasks has no chevron", async ({ page, request }) => {
		await postTask(request, kit.projectId, {
			title: "Lone",
			task_type_id: kit.taskTypeId,
		});
		await page.reload();
		await expect(title(page, "Lone")).toBeVisible();
		// Only "Parent" is expandable.
		await expect(
			page.getByRole("button", { name: /^Expand \d+ subtasks$/ }),
		).toHaveCount(1);
	});

	test("Subtasks do not appear as top-level rows", async ({ page }) => {
		await expect(title(page, "Parent")).toBeVisible();
		await expect(title(page, "Child todo A")).toHaveCount(0);
	});

	test("A subtask can be added under an expanded parent", async ({ page }) => {
		await expandButton(page, 3).click();
		await page.getByRole("button", { name: "Add subtask" }).click();
		await page.getByPlaceholder("Subtask title").fill("Fourth");
		await page.getByPlaceholder("Subtask title").press("Enter");
		await expect(title(page, "Fourth")).toBeVisible();
		await expect(page.getByText("1/4", { exact: true })).toBeVisible();
	});

	test("Clicking the chevron does not open the task", async ({ page }) => {
		await expandButton(page, 3).click();
		await expect(title(page, "Child done")).toBeVisible();
		await expect(page).not.toHaveURL(/\/tasks\//);
	});

	test("Clicking a subtask opens its details", async ({ page }) => {
		await expandButton(page, 3).click();
		await title(page, "Child done").click();
		await expect(
			page.getByRole("dialog").getByText("Child done").first(),
		).toBeVisible();
	});

	test("Expanded rows are remembered after a reload", async ({ page }) => {
		await expandButton(page, 3).click();
		await expect(title(page, "Child done")).toBeVisible();
		await page.reload();
		await expect(title(page, "Child done")).toBeVisible();
	});
});

// ===========================================================================
// Rule: The Subtasks view setting controls nesting
// ===========================================================================

test.describe("The Subtasks view setting controls nesting", () => {
	let kit: ProjectKit;

	test.beforeEach(async ({ request, page }) => {
		kit = await setupProject(request, "SETTING");
		await seedParent(request, kit);
		await signIn(page);
		await openBacklogView(page, kit.projectId, "Table");
	});

	test('The setting defaults to "Nested, collapsed"', async ({ page }) => {
		await openViewSettings(page);
		await expect(settingRow(page, "Subtasks")).toContainText("Nested, collapsed");
	});

	test('"Nested, expanded" shows subtasks without clicking the chevron', async ({
		page,
	}) => {
		await chooseSubtaskMode(page, "Nested, expanded");
		for (const child of ["Child done", "Child todo A", "Child todo B"]) {
			await expect(title(page, child)).toBeVisible();
		}
		await expect(collapseButton(page, 3)).toBeVisible();
	});

	test("A row can still be collapsed by hand when subtasks start expanded", async ({
		page,
	}) => {
		await chooseSubtaskMode(page, "Nested, expanded");
		await collapseButton(page, 3).click();
		await expect(title(page, "Child done")).toHaveCount(0);
		await expect(expandButton(page, 3)).toBeVisible();
	});

	test('"Hidden (flat list)" removes the chevrons and the chips', async ({
		page,
	}) => {
		await chooseSubtaskMode(page, "Hidden (flat list)");
		await expect(
			page.getByRole("button", { name: /^Expand \d+ subtasks$/ }),
		).toHaveCount(0);
		await expect(page.getByText("1/3", { exact: true })).toHaveCount(0);
		await expect(title(page, "Parent")).toBeVisible();
		// Without nesting the subtasks are plain top-level rows again.
		await expect(title(page, "Child todo A")).toBeVisible();
	});

	test("Switching back to nested restores the chevrons", async ({ page }) => {
		await chooseSubtaskMode(page, "Hidden (flat list)");
		await chooseSubtaskMode(page, "Nested, collapsed");
		await expect(expandButton(page, 3)).toBeVisible();
		await expect(page.getByText("1/3", { exact: true })).toBeVisible();
		await expect(title(page, "Child todo A")).toHaveCount(0);
	});

	test("The setting is remembered after a reload", async ({ page }) => {
		await chooseSubtaskMode(page, "Nested, expanded");
		await page.reload();
		await expect(title(page, "Child done")).toBeVisible();
		await openViewSettings(page);
		await expect(settingRow(page, "Subtasks")).toContainText("Nested, expanded");
	});
});

// ===========================================================================
// Rule: The Timeline nests tasks under their Epics
// ===========================================================================

test.describe("The Timeline nests tasks under their Epics", () => {
	let kit: ProjectKit;

	test.beforeEach(async ({ request, page }) => {
		kit = await setupProject(request, "TIMELINE");
		const epicId = await postTask(request, kit.projectId, {
			title: "Epic",
			task_type_id: kit.epicTypeId,
			status_id: kit.todoId,
		});
		for (const name of ["Epic task A", "Epic task B"]) {
			await postTask(request, kit.projectId, {
				title: name,
				task_type_id: kit.taskTypeId,
				status_id: kit.todoId,
				parent_task_id: epicId,
			});
		}
		await signIn(page);
		await openTimeline(page, kit.projectId);
	});

	test("An Epic shows its child count and expands to its tasks", async ({
		page,
	}) => {
		await expect(title(page, "Epic")).toBeVisible();
		await expect(title(page, "Epic task A")).toHaveCount(0);
		await expandButton(page, 2).click();
		await expect(title(page, "Epic task A")).toBeVisible();
		await expect(title(page, "Epic task B")).toBeVisible();
	});

	test("The Subtasks view setting also applies to the Timeline", async ({
		page,
	}) => {
		await chooseSubtaskMode(page, "Nested, expanded");
		await expect(title(page, "Epic task A")).toBeVisible();
		await expect(title(page, "Epic task B")).toBeVisible();
	});
});

// ===========================================================================
// Rule: Board cards show a subtask progress chip
// ===========================================================================

test.describe("Board cards show a subtask progress chip", () => {
	let kit: ProjectKit;

	test.beforeEach(async ({ request, page }) => {
		kit = await setupProject(request, "BOARD");
		await seedParent(request, kit);
		const view = await request.post(
			`${API_URL}/projects/${kit.projectId}/views?context=backlog`,
			{ data: { name: "Board", view_type: "board" } },
		);
		expect(view.ok()).toBeTruthy();
		await signIn(page);
		await openBacklogView(page, kit.projectId, "Board");
	});

	test('A card with subtasks shows "done/total"', async ({ page }) => {
		await expect(title(page, "Parent")).toBeVisible();
		await expect(page.getByText("1/3", { exact: true })).toBeVisible();
	});

	test("The Board offers no Subtasks view setting", async ({ page }) => {
		await openViewSettings(page);
		await expect(page.getByText("Sort by", { exact: true })).toBeVisible();
		await expect(page.getByText("Subtasks", { exact: true })).toHaveCount(0);
	});

	test("Subtasks stay visible as their own cards", async ({ page }) => {
		await expect(title(page, "Parent")).toBeVisible();
		await expect(title(page, "Child todo A")).toBeVisible();
	});
});
