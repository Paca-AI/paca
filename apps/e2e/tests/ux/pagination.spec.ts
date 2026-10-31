// spec: features/ux/pagination.feature
// seed: tests/seed.spec.ts
//
// Table views page by number with a rows-per-page menu; card and sidebar
// lists load more on scroll or click. Every scenario creates its own data
// under a prefix no other spec shares and removes it afterwards.

import { expect, type Page, test } from "@playwright/test";
import {
	cleanupServiceAccountsByPrefix,
	createServiceAccount,
} from "../helpers/accounts";
import {
	API_URL,
	addProjectMember,
	allowPolicy,
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createProject,
	createProjectRole,
	firstProjectRoleId,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { createSprint } from "../helpers/iam";
import { createUserWithEmail, loginContext } from "../helpers/profile-users";

const RUN_ID = newRunId();
const PREFIX = "E2E_PAGINATION_";
const USER_PREFIX = `${PREFIX}U_${RUN_ID}_`;
const ROLE_PREFIX = `${PREFIX}R_${RUN_ID}_`;
const PROJECT_PREFIX = `${PREFIX}P_${RUN_ID}_`;
const ACCOUNT_PREFIX = `${PREFIX}SA_${RUN_ID}_`;
const KEY_USER = `${PREFIX}KEYS_${RUN_ID}`;

/** Zero-padded so name order is numeric order. */
const pad = (n: number) => String(n).padStart(2, "0");

/** Runs `fn` for 1..count with a few requests in flight at a time. */
async function inBatches(
	count: number,
	fn: (n: number) => Promise<unknown>,
	size = 5,
): Promise<void> {
	for (let start = 1; start <= count; start += size) {
		const batch = Array.from(
			{ length: Math.min(size, count - start + 1) },
			(_, i) => start + i,
		);
		await Promise.all(batch.map(fn));
	}
}

// ─── Footer helpers ──────────────────────────────────────────────────────────

function footer(page: Page) {
	return page.getByRole("navigation", { name: "Pagination" });
}

async function expectRange(page: Page, text: string | RegExp): Promise<void> {
	await expect(footer(page).getByText(text)).toBeVisible();
}

async function setRowsPerPage(page: Page, rows: number): Promise<void> {
	await footer(page).getByLabel("Rows per page").selectOption(String(rows));
}

/** Waits until the page's table shows exactly `count` body rows. */
async function expectBodyRows(page: Page, count: number): Promise<void> {
	// +1 for the header row.
	await expect(page.getByRole("table").getByRole("row")).toHaveCount(count + 1);
}

/** The "of N" total shown in the footer's range text. */
async function footerTotal(page: Page): Promise<number> {
	const text = await footer(page)
		.getByText(/ of \d+$/)
		.innerText();
	return Number(text.match(/of (\d+)$/)?.[1]);
}

/**
 * The shared table scenario: the first page holds the default 20 rows, Next
 * shows the rest, a smaller page size restarts at page 1, and the choice
 * survives a reload (`reapply` restores a filter the reload clears).
 */
async function expectTablePaging(
	page: Page,
	total: number,
	/** Re-applies a filter the reload clears (e.g. a search box). */
	reapply?: () => Promise<void>,
): Promise<void> {
	await expectRange(page, `1–20 of ${total}`);
	await expectBodyRows(page, 20);

	await footer(page).getByRole("button", { name: "Next page" }).click();
	await expectRange(page, `21–${total} of ${total}`);
	await expectBodyRows(page, total - 20);

	await setRowsPerPage(page, 10);
	await expectRange(page, `1–10 of ${total}`);
	await expectBodyRows(page, 10);

	await page.reload();
	await expect(footer(page).getByLabel("Rows per page")).toHaveValue("10");
	if (reapply) await reapply();
	await expectRange(page, `1–10 of ${total}`);

	await setRowsPerPage(page, 100);
	await expectRange(page, `1–${total} of ${total}`);
	await expectBodyRows(page, total);
}

/**
 * A development build floats the TanStack Query devtools over the page's
 * bottom corner, where the pagination footer sits; hide it so clicks land.
 * Production builds have no such element.
 */
async function hideDevtools(page: Page): Promise<void> {
	await page.addInitScript(() => {
		const style = document.createElement("style");
		style.textContent = ".tsqd-parent-container { display: none !important; }";
		document.addEventListener("DOMContentLoaded", () =>
			document.head.appendChild(style),
		);
	});
}

test.describe("Table pagination", () => {
	// Sign in directly: the session saved in AUTH_FILE can be invalidated by
	// earlier specs, and this file runs late in the suite.
	test.beforeEach(async ({ page }) => {
		await hideDevtools(page);
		await signIn(page);
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
		await cleanupServiceAccountsByPrefix(request, PREFIX);
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	test("the users table pages by number and remembers rows per page", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		await inBatches(25, (n) =>
			request.post(`${API_URL}/admin/users`, {
				data: {
					username: `${USER_PREFIX}${pad(n)}`,
					full_name: `Paged User ${pad(n)}`,
					password: RESTRICTED_PASSWORD,
				},
			}),
		);

		await page.goto("/admin/users");
		const search = page.getByPlaceholder("Search by name, username or email…");
		await search.fill(USER_PREFIX);
		await expectTablePaging(page, 25, () => search.fill(USER_PREFIX));
	});

	test("the global roles table pages by number", async ({ page, request }) => {
		await authRequest(request);
		await inBatches(22, (n) =>
			createGlobalRole(
				request,
				`${ROLE_PREFIX}${pad(n)}`,
				allowPolicy(["tasks:read"]),
			),
		);

		await page.goto("/admin/global-roles");
		await expect(
			page.getByRole("heading", { name: "Global Roles" }),
		).toBeVisible();
		// The three shipped roles are listed too, so the total is 25 or more.
		const total = await footerTotal(page);
		expect(total).toBeGreaterThanOrEqual(25);
		await expectRange(page, `1–20 of ${total}`);

		await footer(page).getByRole("button", { name: "Next page" }).click();
		await expectRange(page, new RegExp(`^21–\\d+ of ${total}$`));
		await setRowsPerPage(page, 10);
		await expectRange(page, `1–10 of ${total}`);
		await expectBodyRows(page, 10);
	});

	test("the service accounts table pages by number and searches on the server", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		await inBatches(25, (n) =>
			createServiceAccount(request, `${ACCOUNT_PREFIX}${pad(n)}`),
		);

		await page.goto("/admin/service-accounts");
		const search = page.getByPlaceholder("Search by name or description…");
		await search.fill(ACCOUNT_PREFIX);
		await expectTablePaging(page, 25, () => search.fill(ACCOUNT_PREFIX));

		// A narrower search shrinks the total and the page count with it.
		await search.fill(`${ACCOUNT_PREFIX}0`);
		await expectBodyRows(page, 9);
		// A short list keeps its footer (range and rows per page) but has no
		// page buttons to press.
		await expectRange(page, "1–9 of 9");
		await expect(
			footer(page).getByRole("button", { name: "Next page" }),
		).toHaveCount(0);
	});

	test("the API keys table pages by number", async ({
		page,
		playwright,
		request,
	}) => {
		await authRequest(request);
		await createUserWithEmail(request, playwright, {
			username: KEY_USER,
			fullName: "Paged Keys",
		});
		const api = await loginContext(playwright, KEY_USER);
		try {
			await inBatches(25, (n) =>
				api.post(`${API_URL}/users/me/api-keys`, {
					data: { name: `key-${pad(n)}` },
				}),
			);
		} finally {
			await api.dispose();
		}

		// The suite's admin session is loaded; sign in as the key owner instead.
		await page.context().clearCookies();
		await signIn(page, KEY_USER, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/account/api-keys`);
		await expectTablePaging(page, 25);
	});

	test("the project roles settings tab pages by number", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, `${PROJECT_PREFIX}ROLES`);
		await inBatches(22, (n) =>
			createProjectRole(
				request,
				projectId,
				`Paged Role ${pad(n)}`,
				allowPolicy(["tasks:read"], { projectId }),
			),
		);

		await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
		await page.getByRole("button", { name: "Roles", exact: true }).click();
		// The shipped project roles come on top of the 22 created ones.
		const total = await footerTotal(page);
		expect(total).toBeGreaterThanOrEqual(25);
		await expectRange(page, `1–20 of ${total}`);
		await footer(page).getByRole("button", { name: "Next page" }).click();
		await expectRange(page, new RegExp(`^21–\\d+ of ${total}$`));
	});

	test("short project settings tables still show the pagination footer", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, `${PROJECT_PREFIX}SHORT`);

		await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
		for (const tab of ["Roles", "Task Statuses", "Task Types"]) {
			await page.getByRole("button", { name: tab, exact: true }).click();
			// A fresh project has only the shipped few rows, yet the footer is there.
			await expect(footer(page).getByLabel("Rows per page")).toBeVisible();
			await expectRange(page, /^1–\d+ of \d+$/);
		}
	});

	test("the task statuses settings tab pages the loaded list", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, `${PROJECT_PREFIX}STATUSES`);
		await inBatches(22, (n) =>
			request.post(`${API_URL}/projects/${projectId}/task-statuses`, {
				data: { name: `Paged Status ${pad(n)}`, category: "todo" },
			}),
		);

		await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
		await page.getByRole("button", { name: "Task Statuses" }).click();
		const total = await footerTotal(page);
		expect(total).toBeGreaterThanOrEqual(22);
		await expectRange(page, `1–20 of ${total}`);
		await expectBodyRows(page, 20);
		await footer(page).getByRole("button", { name: "Next page" }).click();
		await expectRange(page, new RegExp(`^21–${total} of ${total}$`));
		// Rows keep their place in the whole list across pages.
		await expect(
			page.getByRole("table").getByRole("row").nth(1).getByRole("cell").nth(1),
		).toHaveText("21");
	});
});

test.describe("Load-more lists", () => {
	// Sign in directly: the session saved in AUTH_FILE can be invalidated by
	// earlier specs, and this file runs late in the suite.
	test.beforeEach(async ({ page }) => {
		await hideDevtools(page);
		await signIn(page);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
		// Only the team scenario creates users; sweeping them is a no-op otherwise.
		await cleanupUsersByPrefix(request, USER_PREFIX);
	});

	test("the sidebar counts every draft sprint and loads the rest on demand", async ({
		page,
		request,
		isMobile,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, `${PROJECT_PREFIX}SPRINTS`);
		await inBatches(35, (n) =>
			createSprint(request, projectId, `Draft ${pad(n)}`),
		);

		await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
		// On a phone the sidebar is an off-canvas sheet until it is opened.
		if (isMobile) {
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}
		// The badge comes from the server's total, not from what is loaded.
		await expect(
			page.getByRole("button", { name: "Draft Sprints" }).getByText("35"),
		).toBeVisible();

		// 30 sprints load first; the last ones arrive on scroll or click.
		const sidebar = page.locator('[data-sidebar="sidebar"]');
		const loadMore = sidebar.getByRole("button", { name: "Load more" });
		// The list may also load on scroll, which removes the button mid-click.
		await expect(async () => {
			if (await loadMore.isVisible())
				await loadMore.click({ timeout: 2_000 }).catch(() => {});
			await expect(sidebar.getByRole("link", { name: "Draft 35" })).toBeVisible(
				{ timeout: 2_000 },
			);
		}).toPass({ timeout: 10_000 });
		await expect(sidebar.getByRole("link", { name: "Draft 01" })).toBeVisible();
	});

	test("the team list loads more members past the first page", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, `${PROJECT_PREFIX}TEAM`);
		const roleId = await firstProjectRoleId(request, projectId);
		await inBatches(32, async (n) => {
			const created = await request.post(`${API_URL}/admin/users`, {
				data: {
					username: `${USER_PREFIX}M${pad(n)}`,
					full_name: `Paged Member ${pad(n)}`,
					password: RESTRICTED_PASSWORD,
				},
			});
			expect(created.ok()).toBeTruthy();
			const userId = (await created.json()).data.id as string;
			await addProjectMember(request, projectId, userId, [roleId]);
		});

		await page.goto(`${BASE_URL}/projects/${projectId}/team`);
		// 32 members plus the project's creator.
		await expect(page.getByText("33 members")).toBeVisible();

		const loadMore = page.getByRole("button", { name: "Load more" });
		await expect(async () => {
			if (await loadMore.isVisible())
				await loadMore.click({ timeout: 2_000 }).catch(() => {});
			await expect(page.getByText("Paged Member 32")).toBeVisible({
				timeout: 2_000,
			});
		}).toPass({ timeout: 10_000 });
	});
});
