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

/** Types at the start of the editor's first paragraph. */
async function typeAtStart(page: Page, text: string) {
	await editorOf(page).locator(".bn-block-content").first().click();
	await page.keyboard.press("Control+Home");
	await page.keyboard.type(text);
}

/**
 * Holds back this page's saves (the debounced PATCH of the doc or task) so its
 * edits stay "live only" in the room, as they are during the first seconds of
 * typing. Returns a function that lets them through.
 */
async function holdSaves(page: Page) {
	const held: (() => Promise<void>)[] = [];
	let holding = true;
	await page.route(/\/(docs|tasks)\/[0-9a-f-]{36}$/, async (route) => {
		if (!holding || route.request().method() !== "PATCH") {
			return route.continue();
		}
		held.push(() => route.continue());
	});
	return async () => {
		holding = false;
		for (const go of held.splice(0)) await go();
	};
}

/** Types at the end of the editor's n-th block (0-based). */
async function typeInBlock(page: Page, n: number, text: string) {
	await editorOf(page).locator(".bn-block-content").nth(n).click();
	await page.keyboard.press("End");
	await page.keyboard.type(text);
}

/** A doc with the given paragraphs, opened by both members. */
async function openDocAsBoth(
	request: APIRequestContext,
	playwright: Parameters<typeof createUserWithProjectPermissions>[1],
	browser: Browser,
	texts: string[],
) {
	const { projectId, ann, bob } = await arrange(request, playwright);
	const created = await request.post(`${API_URL}/projects/${projectId}/docs`, {
		data: { title: name("doc"), content: texts.map(paragraph) },
	});
	expect(created.ok(), await created.text()).toBeTruthy();
	const docId = (await created.json()).data.id as string;
	const url = `${BASE_URL}/projects/${projectId}/docs/${docId}`;
	const a = await openAs(browser, ann, url);
	const b = await openAs(browser, bob, url);
	for (const { page } of [a, b]) {
		await expect(editorOf(page)).toContainText(texts[0] ?? "", {
			timeout: 20_000,
		});
	}
	return { projectId, docId, a, b };
}

/**
 * Saves a doc the way an agent / MCP does: over REST, from an account that is
 * not editing the doc, with whatever block JSON it likes (no block ids).
 */
async function agentSavesDoc(
	request: APIRequestContext,
	projectId: string,
	docId: string,
	texts: string[],
) {
	const res = await request.patch(
		`${API_URL}/projects/${projectId}/docs/${docId}`,
		{ data: { content: texts.map(paragraph) } },
	);
	expect(res.ok(), await res.text()).toBeTruthy();
}

const storedDoc = async (
	request: APIRequestContext,
	projectId: string,
	docId: string,
) => {
	const res = await request.get(
		`${API_URL}/projects/${projectId}/docs/${docId}`,
	);
	return JSON.stringify((await res.json()).data?.content ?? "");
};

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

	test.describe("writes by an agent", () => {
		test("reach editors who have the doc open, next to a live edit", async ({
			browser,
			request,
			playwright,
		}) => {
			const first = name("first");
			const second = name("second");
			const { projectId, docId, a, b } = await openDocAsBoth(
				request,
				playwright,
				browser,
				[first, second],
			);
			try {
				// Ann types into the first paragraph; her save is held back.
				const releaseSaves = await holdSaves(a.page);
				await typeAtStart(a.page, "LIVE-");
				await expect
					.poll(() => plainText(b.page), { timeout: 15_000 })
					.toContain(`LIVE-${first}`);

				// The agent rewrites the second paragraph over REST.
				await agentSavesDoc(request, projectId, docId, [first, "by-the-agent"]);

				for (const { page } of [a, b]) {
					await expect
						.poll(() => plainText(page), { timeout: 15_000 })
						.toContain("by-the-agent");
					expect(await plainText(page)).toContain(`LIVE-${first}`);
					expect(await plainText(page)).not.toContain(second);
				}

				// Ann's save, once it goes through, stores the merged document and
				// not just her text.
				await releaseSaves();
				await expect
					.poll(() => storedDoc(request, projectId, docId), {
						timeout: 20_000,
					})
					.toMatch(/LIVE-[\s\S]*by-the-agent/);
			} finally {
				await a.context.close();
				await b.context.close();
			}
		});

		test("add a block without ids exactly once", async ({
			browser,
			request,
			playwright,
		}) => {
			const first = name("first");
			const second = name("second");
			const fresh = name("fresh");
			const { projectId, docId, a, b } = await openDocAsBoth(
				request,
				playwright,
				browser,
				[first, second],
			);
			try {
				await agentSavesDoc(request, projectId, docId, [fresh, first, second]);
				for (const { page } of [a, b]) {
					await expect(editorOf(page)).toContainText(fresh, {
						timeout: 15_000,
					});
					expect(await editorOf(page).getByText(fresh).count()).toBe(1);
					expect(await editorOf(page).getByText(first).count()).toBe(1);
					expect(await editorOf(page).getByText(second).count()).toBe(1);
				}
			} finally {
				await a.context.close();
				await b.context.close();
			}
		});

		test("do not replace a block that is being edited", async ({
			browser,
			request,
			playwright,
		}) => {
			const first = name("first");
			const second = name("second");
			const { projectId, docId, a, b } = await openDocAsBoth(
				request,
				playwright,
				browser,
				[first, second],
			);
			try {
				const releaseSaves = await holdSaves(a.page);
				await typeInBlock(a.page, 1, "-LIVE");
				// Bob seeing it proves the realtime service holds it already.
				await expect
					.poll(() => plainText(b.page), { timeout: 15_000 })
					.toContain(`${second}-LIVE`);

				await agentSavesDoc(request, projectId, docId, [first, "agent-wins"]);

				// Give the merge time to run, then check it left the block alone.
				await a.page.waitForTimeout(3_000);
				for (const { page } of [a, b]) {
					expect(await plainText(page)).toContain(`${second}-LIVE`);
					expect(await plainText(page)).not.toContain("agent-wins");
				}
				await releaseSaves();
				await expect
					.poll(() => storedDoc(request, projectId, docId), {
						timeout: 20_000,
					})
					.toContain(`${second}-LIVE`);
			} finally {
				await a.context.close();
				await b.context.close();
			}
		});

		test("reach editors of a task description", async ({
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
					await expect(editorOf(page)).toContainText(seed, {
						timeout: 20_000,
					});
				}
				const patched = await request.patch(
					`${API_URL}/projects/${projectId}/tasks/${taskId}`,
					{ data: { description: [paragraph("task-by-agent")] } },
				);
				expect(patched.ok(), await patched.text()).toBeTruthy();

				for (const { page } of [a, b]) {
					await expect(editorOf(page)).toContainText("task-by-agent", {
						timeout: 15_000,
					});
					await expect(editorOf(page)).not.toContainText(seed);
				}
			} finally {
				await a.context.close();
				await b.context.close();
			}
		});
	});
});
