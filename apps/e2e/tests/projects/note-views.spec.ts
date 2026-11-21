// spec: features/projects/note-views.feature
// seed: tests/seed.spec.ts
//
// A note view (view_type "note") holds documents that belong to the view:
// they are created, renamed, reordered and deleted in the view, open in the
// docs editor, and never appear in the Docs sidebar.

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

const PREFIX = "E2E_NOTES_";
const RUN_ID = newRunId();

interface StoredDoc {
	id: string;
	title: string;
	view_id?: string | null;
	content: unknown[] | null;
}

async function createNoteView(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/views?context=backlog`,
		{ data: { name, view_type: "note" } },
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.id as string;
}

async function createDoc(
	request: APIRequestContext,
	projectId: string,
	data: { title: string; view_id?: string },
): Promise<StoredDoc> {
	const response = await request.post(`${API_URL}/projects/${projectId}/docs`, {
		data,
	});
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data as StoredDoc;
}

async function listNotes(
	request: APIRequestContext,
	projectId: string,
	viewId: string,
): Promise<StoredDoc[]> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/docs?view_id=${viewId}`,
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.items as StoredDoc[];
}

const openBacklog = async (page: Page, projectId: string) => {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
	await expect(
		page.getByRole("heading", { name: "Product Backlog" }),
	).toBeVisible({ timeout: 30_000 });
};

const noteList = (page: Page) =>
	page.getByRole("complementary", { name: "Notes" });

/** The note titles in list order (each row's own button; its ⋯ menu
 *  trigger is the labelled one). */
const noteTitles = (page: Page) =>
	noteList(page)
		.locator("li > div > button:not([aria-label])")
		.allTextContents();

/** Opens the ⋯ menu of a note. The trigger only shows on hover, so the click
 *  is forced for touch devices. */
const openNoteActions = async (page: Page, title: string) => {
	await noteList(page)
		.getByRole("button", { name: `Actions for ${title}` })
		.click({ force: true });
};

test.describe("A note view is created from the Add view popover", () => {
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
		projectId = await createProject(request, `${PREFIX}CREATE_${RUN_ID}`);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	test("Creating a note view and writing its first note", async ({ page }) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await page.getByRole("button", { name: "Add view" }).click();
		await page.getByLabel("View name").fill("Sprint notes");
		await page.getByRole("button", { name: "Note", exact: true }).click();
		await page.getByRole("button", { name: "Create view" }).click();

		await expect(
			page.getByRole("button", { name: "Sprint notes", exact: true }),
		).toBeVisible();
		await expect(page.getByText("No notes yet")).toBeVisible();

		await noteList(page).getByRole("button", { name: "New note" }).click();
		const title = noteList(page).getByRole("textbox", { name: "Note title" });
		await expect(title).toBeVisible();
		await title.fill("Goals");
		await title.press("Enter");
		await expect(
			noteList(page).getByRole("button", { name: "Goals", exact: true }),
		).toBeVisible();

		const editor = page.locator('[contenteditable="true"]').last();
		await expect(editor).toBeVisible({ timeout: 10_000 });
		await editor.click();
		await page.keyboard.type("Ship note views");
		await page.keyboard.press("ControlOrMeta+s");
		await expect(page.getByText("Saved", { exact: true })).toBeVisible({
			timeout: 10_000,
		});

		const views = await (
			await page.request.get(
				`${API_URL}/projects/${projectId}/views?context=backlog`,
			)
		).json();
		const noteView = (views.data.items as { id: string; name: string }[]).find(
			(v) => v.name === "Sprint notes",
		);
		if (!noteView) throw new Error("the Sprint notes view was not created");
		const [note] = await listNotes(page.request, projectId, noteView.id);
		expect(note.title).toBe("Goals");
		expect(note.view_id).toBe(noteView.id);
		const stored = await (
			await page.request.get(`${API_URL}/projects/${projectId}/docs/${note.id}`)
		).json();
		expect(JSON.stringify(stored.data.content)).toContain("Ship note views");
	});
});

test.describe("Notes belong to their view", () => {
	let projectId: string;
	let viewId: string;
	let retro: StoredDoc;

	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
		projectId = await createProject(request, `${PREFIX}LIST_${RUN_ID}`);
		viewId = await createNoteView(request, projectId, "Meeting notes");
		retro = await createDoc(request, projectId, {
			title: "Retro",
			view_id: viewId,
		});
		await createDoc(request, projectId, { title: "Demo", view_id: viewId });
		await createDoc(request, projectId, { title: "Plan" });
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	const openNoteView = async (page: Page) => {
		await signIn(page);
		await openBacklog(page, projectId);
		await page
			.getByRole("button", { name: "Meeting notes", exact: true })
			.click();
		await expect(noteList(page)).toBeVisible();
	};

	test("Notes are listed in the view and not in the Docs sidebar", async ({
		page,
	}) => {
		await openNoteView(page);
		// Same position: the list falls back to the title.
		await expect.poll(() => noteTitles(page)).toEqual(["Demo", "Retro"]);

		const sidebar = page.locator('[data-sidebar="sidebar"]');
		const viewport = page.viewportSize();
		if (viewport && viewport.width >= 768) {
			// On desktop the sidebar shows the docs tree: the tree document is
			// there, the notes are not.
			await expect(
				sidebar.getByRole("button", { name: "Plan", exact: true }),
			).toBeVisible({ timeout: 10_000 });
		}
		await expect(
			sidebar.getByRole("button", { name: "Retro", exact: true }),
		).toHaveCount(0);
	});

	test("A note is moved down", async ({ page }) => {
		await openNoteView(page);
		await expect.poll(() => noteTitles(page)).toEqual(["Demo", "Retro"]);

		await openNoteActions(page, "Demo");
		await page.getByRole("menuitem", { name: "Move down" }).click();

		await expect.poll(() => noteTitles(page)).toEqual(["Retro", "Demo"]);
		await expect
			.poll(async () =>
				(await listNotes(page.request, projectId, viewId)).map((n) => n.title),
			)
			.toEqual(["Retro", "Demo"]);
	});

	test("Deleting a note asks for confirmation", async ({ page }) => {
		await openNoteView(page);

		await openNoteActions(page, "Retro");
		await page.getByRole("menuitem", { name: "Delete" }).click();
		const dialog = page.getByRole("dialog");
		await expect(dialog).toContainText("“Retro” will be deleted for everyone.");
		await dialog.getByRole("button", { name: "Delete" }).click();

		await expect(
			noteList(page).getByRole("button", { name: "Retro", exact: true }),
		).toHaveCount(0);
		const response = await page.request.get(
			`${API_URL}/projects/${projectId}/docs/${retro.id}`,
		);
		expect(response.status()).toBe(404);
	});
});
