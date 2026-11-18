// spec: features/projects/board-performance.feature
// seed: tests/seed.spec.ts
//
// Regression test for GitHub issue #781 ("Platform works very slow"): on a
// board with many cards, opening a task or loading a column's next page
// re-rendered every card and blocked the main thread for seconds on slower
// machines. A 4x CPU slowdown stands in for those machines, and the budgets
// below leave generous headroom for CI noise.

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupProjectsByPrefix,
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_PERF_";
const TASKS_PER_STATUS = 40;
const CPU_SLOWDOWN = 4;
// Loose sanity budgets; the render counts are the precise check. Opening a task
// also mounts the detail dialog's editor, which costs ~1 s at this slowdown on
// its own, however many cards the board holds.
const OPEN_TASK_BUDGET_MS = 1800;
const LOAD_MORE_BUDGET_MS = 1200;
// Opening a task or loading a page re-renders a card only when it is the
// card that changed. Before the fix every card re-rendered on each of the
// ~15 commits an open takes (thousands of renders); allow a few per card.
const MAX_RENDERS_PER_EXISTING_CARD = 2;

async function seedBoard(request: APIRequestContext): Promise<string> {
	await authRequest(request);
	await cleanupProjectsByPrefix(request, PREFIX);
	const projectId = await createProject(request, `${PREFIX}${newRunId()}`);
	const statuses = (
		await (
			await request.get(`${API_URL}/projects/${projectId}/task-statuses`)
		).json()
	).data.items as { id: string }[];
	const types = (
		await (
			await request.get(`${API_URL}/projects/${projectId}/task-types`)
		).json()
	).data.items as {
		id: string;
		name: string;
	}[];
	const taskType = types.find((t) => t.name !== "Epic");
	const members = (
		await (
			await request.get(`${API_URL}/projects/${projectId}/members?page_size=1`)
		).json()
	).data.items as { id: string }[];
	for (const status of statuses) {
		await Promise.all(
			Array.from({ length: TASKS_PER_STATUS }, (_, i) =>
				request.post(`${API_URL}/projects/${projectId}/tasks`, {
					data: {
						title: `Perf ${status.id.slice(0, 4)} ${i}`,
						status_id: status.id,
						task_type_id: taskType?.id,
						assignee_ids: [members[0].id],
					},
				}),
			),
		);
	}
	const view = await request.post(
		`${API_URL}/projects/${projectId}/views?context=backlog`,
		{
			data: { name: "Board", view_type: "board" },
		},
	);
	expect(view.ok()).toBeTruthy();
	return projectId;
}

/**
 * Records, in the page, how often each task card re-renders (through React's
 * devtools hook, which production builds still call) and the time the main
 * thread spends in long tasks (> 50 ms). Counting renders pins the cause of
 * the slowness directly; the timings below only back it up.
 */
async function trackBoardWork(page: Page) {
	await page.addInitScript(() => {
		const w = window as unknown as {
			__blockedMs: number;
			__recording: boolean;
			__cardRenders: Record<string, number>;
		};
		w.__blockedMs = 0;
		w.__recording = false;
		w.__cardRenders = {};
		new PerformanceObserver((list) => {
			for (const entry of list.getEntries()) w.__blockedMs += entry.duration;
		}).observe({ entryTypes: ["longtask"] });

		const PERFORMED_WORK = 1;
		// biome-ignore lint/suspicious/noExplicitAny: React fiber internals
		const walk = (fiber: any) => {
			for (let f = fiber; f; f = f.sibling) {
				const props = f.memoizedProps;
				if (
					props?.task?.id &&
					"visibleFields" in props &&
					// A board card, or a table row.
					("onDragStart" in props || "showDragHandle" in props) &&
					f.alternate &&
					f.flags & PERFORMED_WORK
				) {
					w.__cardRenders[props.task.id] =
						(w.__cardRenders[props.task.id] ?? 0) + 1;
				}
				walk(f.child);
			}
		};
		(
			window as unknown as { __REACT_DEVTOOLS_GLOBAL_HOOK__: unknown }
		).__REACT_DEVTOOLS_GLOBAL_HOOK__ = {
			supportsFiber: true,
			renderers: new Map(),
			inject: () => 1,
			checkDCE() {},
			onScheduleFiberRoot() {},
			onCommitFiberUnmount() {},
			// biome-ignore lint/suspicious/noExplicitAny: React fiber internals
			onCommitFiberRoot(_id: number, root: any) {
				if (w.__recording) walk(root.current);
			},
		};
	});
}

const blockedMs = (page: Page) =>
	page.evaluate(
		() => (window as unknown as { __blockedMs: number }).__blockedMs,
	);

const setRecording = (page: Page, on: boolean) =>
	page.evaluate((v) => {
		const w = window as unknown as {
			__recording: boolean;
			__cardRenders: Record<string, number>;
		};
		w.__recording = v;
		if (v) w.__cardRenders = {};
	}, on);

const cardRenders = (page: Page) =>
	page.evaluate(
		() =>
			(window as unknown as { __cardRenders: Record<string, number> })
				.__cardRenders,
	);

/**
 * Gives every assignee an uploaded avatar whose presigned URL is signed afresh
 * on each task response, as the API does, and counts how often the browser
 * fetches an avatar image.
 */
async function serveReSignedAvatars(page: Page) {
	let signature = 0;
	const avatarFetches = { count: 0 };
	await page.route("**/e2e-avatar.png*", (route) => {
		avatarFetches.count++;
		return route.fulfill({
			contentType: "image/png",
			body: Buffer.from(
				"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
				"base64",
			),
		});
	});
	await page.route(/\/projects\/[^/]+\/tasks\?/, async (route) => {
		const response = await route.fetch();
		const body = await response.json();
		const sig = `sig${++signature}`;
		const url = `${new URL(page.url()).origin}/e2e-avatar.png?X-Amz-Date=${sig}&X-Amz-Signature=${sig}`;
		for (const task of body?.data?.items ?? []) {
			for (const person of [...(task.assignees ?? []), task.reporter ?? {}]) {
				person.avatar_url = url;
				person.avatar_thumb_url = url;
			}
		}
		await route.fulfill({ response, json: body });
	});
	return avatarFetches;
}

async function openBoard(page: Page, projectId: string) {
	await trackBoardWork(page);
	await signIn(page);
	await page.goto(`/projects/${projectId}/interactions/backlog`);
	await page.getByRole("button", { name: "Board", exact: true }).click();
	await expect(page.locator("[data-task-id]").first()).toBeVisible({
		timeout: 60_000,
	});
	await page.waitForLoadState("networkidle");
	// Let the first render and the avatar lookups settle before measuring.
	await page.waitForTimeout(1000);
	const cdp = await page.context().newCDPSession(page);
	await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU_SLOWDOWN });
}

test.describe("Board stays responsive with many tasks", () => {
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		test.setTimeout(240_000);
		projectId = await seedBoard(request);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	test("Opening a task does not re-render the whole board", async ({
		page,
	}) => {
		await openBoard(page, projectId);
		const cards = await page.locator("[data-task-id]").count();
		expect(cards).toBeGreaterThanOrEqual(50);

		await setRecording(page, true);
		const before = await blockedMs(page);
		await page.locator("[data-task-id]").first().click();
		await expect(page.getByRole("dialog").first()).toBeVisible({
			timeout: 30_000,
		});
		await page.waitForTimeout(1500);
		await setRecording(page, false);

		const renders = Object.values(await cardRenders(page)).reduce(
			(a, b) => a + b,
			0,
		);
		expect(renders).toBeLessThan(cards * MAX_RENDERS_PER_EXISTING_CARD);
		expect((await blockedMs(page)) - before).toBeLessThan(OPEN_TASK_BUDGET_MS);
	});

	test("Loading the next page of a column does not redraw the other cards", async ({
		page,
	}) => {
		const avatarFetches = await serveReSignedAvatars(page);
		await openBoard(page, projectId);

		const existing = await page
			.locator("[data-task-id]")
			.evaluateAll((els) => els.map((e) => e.getAttribute("data-task-id")));
		expect(existing.length).toBeGreaterThanOrEqual(50);
		// Tag the cards on screen; a redraw that remounts them drops the tag.
		await page.evaluate(() => {
			for (const c of document.querySelectorAll("[data-task-id]"))
				(c as unknown as { __seen: boolean }).__seen = true;
		});

		const column = page
			.locator("[data-task-id]")
			.first()
			.locator('xpath=ancestor::div[contains(@class,"overflow-y-auto")][1]');
		await setRecording(page, true);
		const avatarFetchesBefore = avatarFetches.count;
		const before = await blockedMs(page);
		await column.evaluate((el) => {
			el.scrollTop = el.scrollHeight;
		});
		await expect
			.poll(() => page.locator("[data-task-id]").count(), { timeout: 30_000 })
			.toBeGreaterThan(existing.length);
		await page.waitForTimeout(1500);
		await setRecording(page, false);

		const survivors = await page.evaluate(
			() =>
				[...document.querySelectorAll("[data-task-id]")].filter(
					(c) => (c as unknown as { __seen?: boolean }).__seen,
				).length,
		);
		expect(survivors).toBe(existing.length);
		// The same avatar arriving with a fresh signature is not a new image.
		expect(avatarFetches.count - avatarFetchesBefore).toBe(0);

		const renders = await cardRenders(page);
		const existingRenders = existing.reduce(
			(sum, id) => sum + (renders[id ?? ""] ?? 0),
			0,
		);
		expect(existingRenders).toBeLessThan(
			existing.length * MAX_RENDERS_PER_EXISTING_CARD,
		);
		expect((await blockedMs(page)) - before).toBeLessThan(LOAD_MORE_BUDGET_MS);
	});

	test("Opening a task does not re-render every table row", async ({
		page,
	}) => {
		await trackBoardWork(page);
		await signIn(page);
		// The backlog opens on its default Table view.
		await page.goto(`/projects/${projectId}/interactions/backlog`);
		const rows = page.getByText(/^Perf \w{4} \d+$/);
		await expect(rows.first()).toBeVisible({ timeout: 60_000 });
		for (let i = 0; i < 12; i++) {
			const more = page.getByText("View more").first();
			if (!(await more.isVisible().catch(() => false))) break;
			await more.click();
			await page.waitForTimeout(400);
		}
		await page.waitForLoadState("networkidle");
		await page.waitForTimeout(1000);
		const shown = await rows.count();
		expect(shown).toBeGreaterThanOrEqual(100);
		const cdp = await page.context().newCDPSession(page);
		await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU_SLOWDOWN });

		await setRecording(page, true);
		const before = await blockedMs(page);
		await rows.first().click();
		await expect(page.getByRole("dialog").first()).toBeVisible({
			timeout: 30_000,
		});
		await page.waitForTimeout(1500);
		await setRecording(page, false);

		const renders = Object.values(await cardRenders(page)).reduce(
			(a, b) => a + b,
			0,
		);
		expect(renders).toBeLessThan(shown * MAX_RENDERS_PER_EXISTING_CARD);
		expect((await blockedMs(page)) - before).toBeLessThan(OPEN_TASK_BUDGET_MS);
	});
});
