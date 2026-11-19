// The "+" button of an empty block opens a menu of block types. In the
// collaborative editors (documents, task descriptions) that menu used to close
// by itself a moment after opening, so only the keyboard could reach it in
// time; and the description once rendered two menus on top of each other.

import { expect, type Locator, type Page, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";

const TEST_PREFIX = "E2E_ADDBLOCK_";
const RUN_ID = newRunId();

type Editor = "document" | "description" | "comment";

/** Opens a page with the editor and returns that editor. */
async function openEditor(
	page: Page,
	request: Parameters<typeof authRequest>[0],
	editor: Editor,
	projectId: string,
): Promise<Locator> {
	let url: string;
	if (editor === "document") {
		const doc = await request.post(`${API_URL}/projects/${projectId}/docs`, {
			data: { title: "Add block" },
		});
		url = `${BASE_URL}/projects/${projectId}/docs/${(await doc.json()).data.id}`;
	} else {
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: "Add block" },
		});
		url = `${BASE_URL}/projects/${projectId}/tasks/${(await task.json()).data.id}`;
	}
	await signIn(page);
	await page.setViewportSize({ width: 1280, height: 1000 });
	await page.goto(url);
	const editors = page.locator(".bn-editor");
	await editors.first().waitFor();
	// Let the collaboration provider connect before using the editor.
	await page.waitForTimeout(1500);

	// A task page has the description on the left and the comment composer on
	// the right; tell them apart by position.
	let picked = editors.first();
	for (let i = 0; i < (await editors.count()); i++) {
		const box = await editors.nth(i).boundingBox();
		if (!box) continue;
		if (editor === "comment" && box.x > 900) picked = editors.nth(i);
		if (editor === "description" && box.x < 900) picked = editors.nth(i);
	}
	return picked;
}

async function openAddBlockMenu(page: Page, editor: Locator) {
	await editor.click();
	await editor.locator(".bn-block-content").first().hover();
	await page.locator(".bn-side-menu button").first().click();
	// Exactly one menu: the description once rendered two.
	await expect(page.locator(".bn-suggestion-menu")).toHaveCount(1);
}

const headings = (page: Page) =>
	page.locator('[data-content-type="heading"]').count();

test.describe("Add block (+) menu", () => {
	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
	});
	test.afterAll(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
	});

	for (const editorKind of ["document", "description", "comment"] as const) {
		test(`${editorKind}: an option can be chosen with the mouse, held down like a person does`, async ({
			page,
			request,
		}) => {
			await authRequest(request);
			const projectId = await createProject(
				request,
				`${TEST_PREFIX}${RUN_ID}_${editorKind}_m`,
			);
			const editor = await openEditor(page, request, editorKind, projectId);
			await openAddBlockMenu(page, editor);
			// The menu must still be there a moment later, not closed by itself.
			await page.waitForTimeout(800);
			const item = page
				.locator(".bn-suggestion-menu-item", { hasText: "Heading 1" })
				.first();
			await expect(item).toBeVisible();
			await item.hover();
			await page.mouse.down();
			await page.waitForTimeout(200);
			await page.mouse.up();
			await expect.poll(() => headings(page)).toBe(1);
		});

		test(`${editorKind}: an option can be chosen with the keyboard`, async ({
			page,
			request,
		}) => {
			await authRequest(request);
			const projectId = await createProject(
				request,
				`${TEST_PREFIX}${RUN_ID}_${editorKind}_k`,
			);
			const editor = await openEditor(page, request, editorKind, projectId);
			await openAddBlockMenu(page, editor);
			await page.keyboard.press("Enter");
			await expect.poll(() => headings(page)).toBe(1);
		});

		test(`${editorKind}: dismissing the menu leaves the block empty`, async ({
			page,
			request,
		}) => {
			await authRequest(request);
			const projectId = await createProject(
				request,
				`${TEST_PREFIX}${RUN_ID}_${editorKind}_e`,
			);
			const editor = await openEditor(page, request, editorKind, projectId);
			await openAddBlockMenu(page, editor);
			await page.keyboard.press("Escape");
			await expect(page.locator(".bn-suggestion-menu")).toHaveCount(0);
			// No stray "/" is left in the block.
			await expect(
				editor.locator(".bn-block-content").first(),
			).not.toContainText("/");
		});
	}
});
