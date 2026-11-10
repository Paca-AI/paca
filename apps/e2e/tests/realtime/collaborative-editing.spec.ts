// spec: features/realtime/collaborative-editing.feature
// seed: tests/seed.spec.ts
//
// Docs and task descriptions are edited together through Yjs, relayed by the
// realtime service. Two members open the same resource in separate browsers:
// the stored content must appear once (only one client seeds the room), what
// one types must show up for the other, and the REST copy that MCP, search and
// exports read must end up holding both people's text.

import {
	type APIRequestContext,
	type Browser,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { createTask } from "../helpers/iam";

const PREFIX = "E2E_COLLAB_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

const EDITOR = {
	"docs:read": true,
	"docs:write": true,
	"tasks:read": true,
	"tasks:write": true,
	"views:read": true,
} as const;

const paragraph = (text: string) => ({
	type: "paragraph",
	content: [{ type: "text", text, styles: {} }],
});

test.beforeEach(async ({ request, context }) => {
	await authRequest(request);
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
	await context.clearCookies();
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
});

/** A project with two members who can edit docs and tasks. */
async function arrange(
	request: APIRequestContext,
	playwright: Parameters<typeof createUserWithProjectPermissions>[1],
) {
	await authRequest(request);
	const projectId = await createProject(request, name("p"));
	for (const who of ["ann", "bob"]) {
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username: name(who),
			roleName: name(`role_${who}`),
			permissions: EDITOR,
		});
	}
	return { projectId, ann: name("ann"), bob: name("bob") };
}

/** A second, independent browser session signed in as `username`. */
async function openAs(browser: Browser, username: string, url: string) {
	const context = await browser.newContext();
	const page = await context.newPage();
	await signIn(page, username, RESTRICTED_PASSWORD);
	await page.goto(url);
	return { context, page };
}

const editorOf = (page: Page) => page.locator(".bn-editor").first();

/** The editor's text without the remote-cursor name labels. */
const plainText = (page: Page) =>
	editorOf(page).evaluate((el) => {
		const copy = el.cloneNode(true) as HTMLElement;
		for (const c of copy.querySelectorAll('[class*="cursor"]')) {
			c.remove();
		}
		return (copy.textContent ?? "").replace(/\s+/g, " ").trim();
	});

/** Types at the end of the editor's last paragraph. */
async function typeAtEnd(page: Page, text: string) {
	const editor = editorOf(page);
	await editor.locator(".bn-block-content").last().click();
	await page.keyboard.press("Control+End");
	await page.keyboard.type(text);
}

test.describe("Collaborative editing", () => {
	test("two members share a doc live", async ({
		browser,
		request,
		playwright,
	}) => {
		const { projectId, ann, bob } = await arrange(request, playwright);
		const seed = name("seed");
		const created = await request.post(
			`${API_URL}/projects/${projectId}/docs`,
			{
				data: { title: name("doc"), content: [paragraph(seed)] },
			},
		);
		expect(created.ok(), await created.text()).toBeTruthy();
		const docId = (await created.json()).data.id as string;
		const url = `${BASE_URL}/projects/${projectId}/docs/${docId}`;

		const a = await openAs(browser, ann, url);
		const b = await openAs(browser, bob, url);
		try {
			// Whoever joined first seeded the room; the second must not add it again.
			for (const { page } of [a, b]) {
				await expect(editorOf(page)).toContainText(seed, { timeout: 20_000 });
				expect(
					await editorOf(page).getByText(seed, { exact: false }).count(),
				).toBe(1);
			}

			await typeAtEnd(a.page, " from-ann");
			await expect(editorOf(b.page)).toContainText("from-ann", {
				timeout: 15_000,
			});

			await typeAtEnd(b.page, " from-bob");
			await expect(editorOf(a.page)).toContainText("from-bob", {
				timeout: 15_000,
			});

			// Both pages converge on identical text.
			await expect
				.poll(async () => plainText(a.page), { timeout: 10_000 })
				.toBe(await plainText(b.page));

			// The REST copy (read by MCP, search, exports) holds both edits.
			await expect
				.poll(
					async () => {
						const res = await request.get(
							`${API_URL}/projects/${projectId}/docs/${docId}`,
						);
						return JSON.stringify((await res.json()).data?.content ?? "");
					},
					{ timeout: 20_000 },
				)
				.toMatch(/from-ann[\s\S]*from-bob|from-bob[\s\S]*from-ann/);
		} finally {
			await a.context.close();
			await b.context.close();
		}
	});

	test("two members share a task description live", async ({
		browser,
		request,
		playwright,
	}) => {
		const { projectId, ann, bob } = await arrange(request, playwright);
		const taskId = await createTask(request, projectId, name("task"));
		const seed = name("desc");
		const set = await request.patch(
			`${API_URL}/projects/${projectId}/tasks/${taskId}`,
			{ data: { description: [paragraph(seed)] } },
		);
		expect(set.ok(), await set.text()).toBeTruthy();
		const url = `${BASE_URL}/projects/${projectId}/tasks/${taskId}`;

		const a = await openAs(browser, ann, url);
		const b = await openAs(browser, bob, url);
		try {
			for (const { page } of [a, b]) {
				await expect(editorOf(page)).toContainText(seed, { timeout: 20_000 });
				expect(
					await editorOf(page).getByText(seed, { exact: false }).count(),
				).toBe(1);
			}

			await typeAtEnd(a.page, " from-ann");
			await expect(editorOf(b.page)).toContainText("from-ann", {
				timeout: 15_000,
			});
		} finally {
			await a.context.close();
			await b.context.close();
		}
	});
});
