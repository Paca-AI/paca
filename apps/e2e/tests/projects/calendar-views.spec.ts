// spec: features/projects/calendar-views.feature
// seed: tests/seed.spec.ts
//
// A Calendar view (view_type "calendar") shows the tasks of an interaction on
// a month, week or day grid. Day cells carry their day number (days since
// 1970-01-01, UTC) in data-calendar-day, and each bar of a task carries its
// first and last day in data-start / data-end.

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
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_CALENDAR_";
const RUN_ID = newRunId();
const MS_PER_DAY = 86_400_000;

interface StoredTask {
	id: string;
	start_date?: string | null;
	due_date?: string | null;
}

/** Day number and stored form of a day of the current (local) month. */
function dayOfThisMonth(date: number): { day: number; iso: string } {
	const now = new Date();
	const ms = Date.UTC(now.getFullYear(), now.getMonth(), date);
	return {
		day: Math.floor(ms / MS_PER_DAY),
		iso: `${new Date(ms).toISOString().slice(0, 10)}T00:00:00Z`,
	};
}

async function createCalendarView(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/views?context=backlog`,
		{ data: { name, view_type: "calendar" } },
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.id as string;
}

/** Creates a task of the project's default (non-system) type: the views'
 *  default filter lists those types. */
async function createDatedTask(
	request: APIRequestContext,
	projectId: string,
	title: string,
	startDate: string,
	dueDate: string,
): Promise<StoredTask> {
	const types = await (
		await request.get(`${API_URL}/projects/${projectId}/task-types`)
	).json();
	const items = (types?.data?.items ?? []) as {
		id: string;
		is_system?: boolean;
		is_default?: boolean;
	}[];
	const normal = items.filter((tt) => !tt.is_system);
	const type = normal.find((tt) => tt.is_default) ?? normal[0];
	const response = await request.post(
		`${API_URL}/projects/${projectId}/tasks`,
		{
			data: {
				title,
				task_type_id: type?.id,
				start_date: startDate,
				due_date: dueDate,
			},
		},
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data as StoredTask;
}

const openBacklog = async (page: Page, projectId: string) => {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
	await expect(
		page.getByRole("heading", { name: "Product Backlog" }),
	).toBeVisible({ timeout: 30_000 });
};

const openCalendar = async (page: Page, projectId: string, name: string) => {
	await openBacklog(page, projectId);
	await page.getByRole("button", { name, exact: true }).click();
	await expect(page.getByRole("grid")).toBeVisible({ timeout: 10_000 });
};

const taskBars = (page: Page, taskId: string) =>
	page.locator(`[data-calendar-task="${taskId}"]`);

const currentMonthTitle = () =>
	new Intl.DateTimeFormat("en-US", { month: "long", year: "numeric" }).format(
		new Date(),
	);

test.describe("A calendar view is created from the Add view popover", () => {
	let projectId: string;
	let task: StoredTask;
	const start = dayOfThisMonth(10);
	const due = dayOfThisMonth(12);

	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
		projectId = await createProject(request, `${PREFIX}CREATE_${RUN_ID}`);
		task = await createDatedTask(
			request,
			projectId,
			`${PREFIX}TASK`,
			start.iso,
			due.iso,
		);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	test("Creating a calendar view shows this month with the task on its days", async ({
		page,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await page.getByRole("button", { name: "Add view" }).click();
		await page.getByLabel("View name").fill("Release calendar");
		await page.getByRole("button", { name: "Calendar", exact: true }).click();
		await page.getByRole("button", { name: "Create view" }).click();

		await expect(
			page.getByRole("button", { name: "Release calendar", exact: true }),
		).toBeVisible();
		await expect(
			page.getByRole("grid", { name: currentMonthTitle() }),
		).toBeVisible({ timeout: 10_000 });

		const bars = taskBars(page, task.id);
		await expect(bars.first()).toBeVisible({ timeout: 10_000 });
		await expect(bars.first()).toHaveAttribute("data-start", String(start.day));
		await expect(bars.last()).toHaveAttribute("data-end", String(due.day));
	});
});

test.describe("Dragging a task reschedules it", () => {
	let projectId: string;
	let task: StoredTask;
	const start = dayOfThisMonth(3);
	const due = dayOfThisMonth(4);

	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
		projectId = await createProject(request, `${PREFIX}DRAG_${RUN_ID}`);
		await createCalendarView(request, projectId, "Calendar");
		task = await createDatedTask(
			request,
			projectId,
			`${PREFIX}DRAG_TASK`,
			start.iso,
			due.iso,
		);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	test("Dropping a task on another day moves both of its dates", async ({
		page,
		isMobile,
	}) => {
		test.skip(isMobile, "HTML5 drag and drop needs a mouse");
		await signIn(page);
		await openCalendar(page, projectId, "Calendar");

		const bar = taskBars(page, task.id).first().getByRole("button");
		await expect(bar).toBeVisible({ timeout: 10_000 });
		const target = dayOfThisMonth(17);
		// Grab the task by its first day, so the drop day becomes its start
		// (clear of the resize handle on the bar's left edge).
		const box = await bar.boundingBox();
		if (!box) throw new Error("the task bar has no box");
		await bar.dragTo(page.locator(`[data-calendar-day="${target.day}"]`), {
			sourcePosition: { x: 12, y: box.height / 2 },
		});

		const moved = dayOfThisMonth(18);
		await expect
			.poll(
				async () => {
					const response = await page.request.get(
						`${API_URL}/projects/${projectId}/tasks/${task.id}`,
					);
					const stored = (await response.json()).data as StoredTask;
					return [stored.start_date, stored.due_date];
				},
				{ timeout: 10_000 },
			)
			.toEqual([target.iso, moved.iso]);
		await expect(taskBars(page, task.id).first()).toHaveAttribute(
			"data-start",
			String(target.day),
		);
	});
});

test.describe("The mode is remembered", () => {
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
		projectId = await createProject(request, `${PREFIX}MODE_${RUN_ID}`);
		await createCalendarView(request, projectId, "Calendar");
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	test("Switching to the week mode survives a reload", async ({ page }) => {
		await signIn(page);
		await openCalendar(page, projectId, "Calendar");

		const week = page.getByRole("button", { name: "Week", exact: true });
		await week.click();
		await expect(week).toHaveAttribute("aria-pressed", "true");
		await expect(page.getByRole("gridcell")).toHaveCount(7);
		// The choice is saved to the view before the reload.
		await expect
			.poll(
				async () => {
					const response = await page.request.get(
						`${API_URL}/projects/${projectId}/views?context=backlog`,
					);
					const items = (await response.json()).data.items as {
						name: string;
						config?: { calendar_mode?: string };
					}[];
					return items.find((v) => v.name === "Calendar")?.config
						?.calendar_mode;
				},
				{ timeout: 10_000 },
			)
			.toBe("week");

		await page.reload();
		await expect(
			page.getByRole("button", { name: "Week", exact: true }),
		).toHaveAttribute("aria-pressed", "true", { timeout: 15_000 });
		await expect(page.getByRole("gridcell")).toHaveCount(7);
	});
});
