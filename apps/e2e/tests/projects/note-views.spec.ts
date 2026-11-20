// spec: features/projects/note-views.feature
// seed: tests/seed.spec.ts
//
// A note view (view_type "note") holds one document that belongs to the view:
// it is created when the view is first opened, opens in the docs editor with
// no title and no list of notes, and never appears in the Docs sidebar.

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

test.describe("A note view is created from the Add view popover", () => {
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
		projectId = await createProject(request, `${PREFIX}CREATE_${RUN_ID}`);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	test("Creating a note view opens an editable document", async ({ page }) => {
		await signIn(page);
		await openBacklog(page, projectId);

		await page.getByRole("button", { name: "Add view" }).click();
		await page.getByLabel("View name").fill("Sprint notes");
		await page.getByRole("button", { name: "Note", exact: true }).click();
		await page.getByRole("button", { name: "Create view" }).click();

		await expect(
			page.getByRole("button", { name: "Sprint notes", exact: true }),
		).toBeVisible();
		// No notes list and no "New note": the view is the document.
		await expect(
			page.getByRole("complementary", { name: "Notes" }),
		).toHaveCount(0);
		await expect(page.getByRole("button", { name: "New note" })).toHaveCount(0);

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
		const notes = await listNotes(page.request, projectId, noteView.id);
		expect(notes).toHaveLength(1);
		expect(notes[0].view_id).toBe(noteView.id);
		const stored = await (
			await page.request.get(
				`${API_URL}/projects/${projectId}/docs/${notes[0].id}`,
			)
		).json();
		expect(JSON.stringify(stored.data.content)).toContain("Ship note views");
	});
});

test.describe("The document belongs to its view", () => {
	let projectId: string;
	let viewId: string;

	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
		projectId = await createProject(request, `${PREFIX}LIST_${RUN_ID}`);
		viewId = await createNoteView(request, projectId, "Meeting notes");
		await createDoc(request, projectId, { title: "Retro", view_id: viewId });
		await createDoc(request, projectId, { title: "Plan" });
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	test("The document is not in the Docs sidebar, and opening the view adds no second one", async ({
		page,
	}) => {
		await signIn(page);
		await openBacklog(page, projectId);
		await page
			.getByRole("button", { name: "Meeting notes", exact: true })
			.click();
		await expect(page.locator('[contenteditable="true"]').last()).toBeVisible({
			timeout: 10_000,
		});

		const sidebar = page.locator('[data-sidebar="sidebar"]');
		const viewport = page.viewportSize();
		if (viewport && viewport.width >= 768) {
			// On desktop the sidebar shows the docs tree: the tree document is
			// there, the note is not.
			await expect(
				sidebar.getByRole("button", { name: "Plan", exact: true }),
			).toBeVisible({ timeout: 10_000 });
		}
		await expect(
			sidebar.getByRole("button", { name: "Retro", exact: true }),
		).toHaveCount(0);
		const notes = await listNotes(page.request, projectId, viewId);
		expect(notes.map((n) => n.title)).toEqual(["Retro"]);
	});
});
