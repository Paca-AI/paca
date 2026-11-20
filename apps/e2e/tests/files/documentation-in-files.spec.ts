// spec: docs/product/unified-documentation-files.md
// seed: tests/seed.spec.ts
//
// Documentation and Files are one folder tree: the Documentation folder and its
// documents (Markdown files) show up in Files, documents behave like files
// (rename, move, star, recent, search, trash), download as Markdown, and an
// uploaded Markdown file becomes a document.

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

const TEST_PREFIX = "E2E_DOCFILES_";
const RUN_ID = newRunId();

let counter = 0;
function uniqueName(label: string): string {
	counter += 1;
	return `${TEST_PREFIX}${label}_${RUN_ID}${counter}`;
}

function filesUrl(projectId: string, section = ""): string {
	return `${BASE_URL}/projects/${projectId}/files${section}`;
}

/** A row or card of the file list, found by its visible name. */
function item(page: Page, name: string) {
	return page.locator("[data-file-id]").filter({ hasText: name }).first();
}

/** The location shown above the list (the shared path breadcrumb). */
const breadcrumb = (page: Page) =>
	page.getByRole("navigation", { name: "breadcrumb" });

const EDITOR_URL = (projectId: string) =>
	new RegExp(`/projects/${projectId}/docs/[0-9a-f-]{36}`);

// ── API setup ────────────────────────────────────────────────────────────────

async function createFolder(
	request: APIRequestContext,
	projectId: string,
	name: string,
	parentId?: string,
): Promise<string> {
	const res = await request.post(
		`${API_URL}/projects/${projectId}/docs/folders`,
		{
			data: { name, ...(parentId ? { parent_id: parentId } : {}) },
		},
	);
	expect(res.ok()).toBeTruthy();
	return (await res.json()).data.id as string;
}

function blocks(text: string) {
	return [
		{
			id: crypto.randomUUID(),
			type: "paragraph",
			props: {},
			content: [{ type: "text", text, styles: {} }],
			children: [],
		},
	];
}

async function createDoc(
	request: APIRequestContext,
	projectId: string,
	title: string,
	opts: { folderId?: string; text?: string } = {},
): Promise<string> {
	const res = await request.post(`${API_URL}/projects/${projectId}/docs`, {
		data: {
			title,
			content: blocks(opts.text ?? `Body of ${title}`),
			...(opts.folderId ? { folder_id: opts.folderId } : {}),
		},
	});
	expect(res.ok()).toBeTruthy();
	return (await res.json()).data.id as string;
}

async function docStatus(
	request: APIRequestContext,
	projectId: string,
	docId: string,
): Promise<number> {
	return (
		await request.get(`${API_URL}/projects/${projectId}/docs/${docId}`)
	).status();
}

/** Opens a folder of the file list by double click. */
async function openFolder(page: Page, name: string) {
	await item(page, name).dblclick();
	await expect(breadcrumb(page).getByText(name)).toBeVisible();
}

async function setUploadFiles(
	page: Page,
	files: { name: string; mimeType: string; body: string }[],
) {
	// The New menu clicks a hidden input; set the files on it directly.
	await page.locator('input[type="file"]:not([webkitdirectory])').setInputFiles(
		files.map((f) => ({
			name: f.name,
			mimeType: f.mimeType,
			buffer: Buffer.from(f.body),
		})),
	);
}

test.describe("Documentation in Files", () => {
	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
	});
	test.afterAll(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
	});

	test("Files lists the Documentation folder with its folders and Markdown documents", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("tree"));
		const governance = await createFolder(request, projectId, "Governance");
		await createDoc(request, projectId, "Charter", { folderId: governance });
		await createDoc(request, projectId, "Loose note");

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await expect(item(page, "Documentation")).toBeVisible();

		await openFolder(page, "Documentation");
		await expect(item(page, "Governance")).toBeVisible();
		// A document is the file "<title>.md".
		await expect(item(page, "Loose note.md")).toBeVisible();

		await openFolder(page, "Governance");
		await expect(item(page, "Charter.md")).toBeVisible();
		await expect(breadcrumb(page).getByText("Documentation")).toBeVisible();

		// A document opens in the docs editor.
		await item(page, "Charter.md").dblclick();
		await expect(page).toHaveURL(EDITOR_URL(projectId));
	});

	test("New document in a documentation folder creates it and opens the editor", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("newdoc"));
		const governance = await createFolder(request, projectId, "Governance");

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Governance");

		await page.getByRole("button", { name: "New", exact: true }).click();
		await page.getByRole("menuitem", { name: "New document" }).click();
		await expect(page).toHaveURL(EDITOR_URL(projectId));

		// It was created in that folder.
		const list = await request.get(
			`${API_URL}/projects/${projectId}/docs?folder_id=${governance}`,
		);
		expect((await list.json()).data.items).toHaveLength(1);
	});

	test("New document is only offered inside Documentation", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("newdocscope"));
		await signIn(page);
		await page.goto(filesUrl(projectId));

		// At the project root, New has no Document entry.
		await page.getByRole("button", { name: "New", exact: true }).click();
		await expect(
			page.getByRole("menuitem", { name: "New folder" }),
		).toBeVisible();
		await expect(
			page.getByRole("menuitem", { name: "New document" }),
		).toHaveCount(0);
		await page.keyboard.press("Escape");

		await openFolder(page, "Documentation");
		await page.getByRole("button", { name: "New", exact: true }).click();
		await expect(
			page.getByRole("menuitem", { name: "New document" }),
		).toBeVisible();
	});

	test("an uploaded Markdown file becomes a document; other files stay files", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("import"));
		await createFolder(request, projectId, "Methodology");

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Methodology");

		await setUploadFiles(page, [
			{
				name: "process-guide.md",
				mimeType: "text/markdown",
				body: "# Evaluation criteria\n\nScore every proposal on cost and risk.\n",
			},
			{
				name: "plain-notes.md",
				mimeType: "text/markdown",
				body: "No heading here, just text.\n",
			},
			{
				name: "evidence.txt",
				mimeType: "text/plain",
				body: "stored as a file",
			},
		]);

		// A leading level-1 heading is the title; without one the file name is.
		await expect(item(page, "Evaluation criteria.md")).toBeVisible({
			timeout: 30_000,
		});
		await expect(item(page, "plain-notes.md")).toBeVisible();
		await expect(item(page, "evidence.txt")).toBeVisible();
		// Never stored as a Markdown file.
		await expect(item(page, "process-guide.md")).toHaveCount(0);

		// The documents are documents (open in the editor); the text file is a file.
		await item(page, "Evaluation criteria.md").click({ button: "right" });
		await expect(
			page.getByRole("menuitem", { name: "Open document" }),
		).toBeVisible();
		await page.keyboard.press("Escape");
		await item(page, "evidence.txt").click({ button: "right" });
		await expect(page.getByRole("menuitem", { name: "Preview" })).toBeVisible();
		await page.keyboard.press("Escape");

		// The body went into the document.
		await item(page, "Evaluation criteria.md").dblclick();
		await expect(page).toHaveURL(EDITOR_URL(projectId));
		await expect(
			page.getByText("Score every proposal on cost and risk."),
		).toBeVisible({ timeout: 30_000 });
	});

	test("a Markdown file uploaded outside Documentation stays an ordinary file", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("importscope"));
		await signIn(page);
		await page.goto(filesUrl(projectId));

		await page.getByRole("button", { name: "New", exact: true }).click();
		await page.getByRole("menuitem", { name: "New folder" }).click();
		await page.getByLabel("Folder name").fill("Specs");
		await page.getByRole("button", { name: "Create" }).click();
		await openFolder(page, "Specs");

		await setUploadFiles(page, [
			{
				name: "readme.md",
				mimeType: "text/markdown",
				body: "# Readme\n\nText.\n",
			},
		]);
		await expect(item(page, "readme.md")).toBeVisible({ timeout: 30_000 });
		await item(page, "readme.md").click({ button: "right" });
		await expect(page.getByRole("menuitem", { name: "Preview" })).toBeVisible();
		await expect(
			page.getByRole("menuitem", { name: "Open document" }),
		).toHaveCount(0);
	});

	test("a document downloads as a Markdown file", async ({ page, request }) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("download"));
		const governance = await createFolder(request, projectId, "Governance");
		await createDoc(request, projectId, "Charter", {
			folderId: governance,
			text: "Downloaded body text",
		});

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Governance");

		await item(page, "Charter.md").click({ button: "right" });
		const [download] = await Promise.all([
			page.waitForEvent("download"),
			page.getByRole("menuitem", { name: "Download" }).click(),
		]);
		expect(download.suggestedFilename()).toBe("Charter.md");
		const path = await download.path();
		const { readFile } = await import("node:fs/promises");
		const text = await readFile(path, "utf8");
		expect(text.startsWith("# Charter")).toBe(true);
		expect(text).toContain("Downloaded body text");
	});

	test("the docs editor has a Download as Markdown button", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("editordl"));
		const docId = await createDoc(request, projectId, "Plan", {
			text: "Editor download body",
		});

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/docs/${docId}`);
		const [download] = await Promise.all([
			page.waitForEvent("download"),
			page.getByRole("button", { name: "Download as Markdown" }).click(),
		]);
		expect(download.suggestedFilename()).toBe("Plan.md");
	});

	test("rename and move a document from Files", async ({ page, request }) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("renamemove"));
		const from = await createFolder(request, projectId, "Drafts");
		await createFolder(request, projectId, "Final");
		const docId = await createDoc(request, projectId, "Proposal", {
			folderId: from,
		});

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Drafts");

		// Rename: the .md is how it shows up, the title is what is stored.
		await item(page, "Proposal.md").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Rename" }).click();
		await page
			.getByRole("dialog")
			.getByRole("textbox", { name: "Name" })
			.fill("Proposal v2.md");
		await page.getByRole("button", { name: "Save" }).click();
		await expect(item(page, "Proposal v2.md")).toBeVisible();
		const renamed = await request.get(
			`${API_URL}/projects/${projectId}/docs/${docId}`,
		);
		expect((await renamed.json()).data.title).toBe("Proposal v2");

		// Move it to another documentation folder.
		await item(page, "Proposal v2.md").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Move to…" }).click();
		const dialog = page.getByRole("dialog");
		await dialog.getByRole("button", { name: "Documentation" }).first().click();
		await dialog.getByRole("button", { name: "Final" }).click();
		await dialog.getByRole("button", { name: "Move here" }).click();
		await expect(item(page, "Proposal v2.md")).toHaveCount(0);

		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Final");
		await expect(item(page, "Proposal v2.md")).toBeVisible();
	});

	test("star, open and search find a document in Files", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("starsearch"));
		const governance = await createFolder(request, projectId, "Governance");
		await createDoc(request, projectId, "Quarterly review", {
			folderId: governance,
		});

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Governance");

		// Star it.
		await item(page, "Quarterly review.md").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Add to starred" }).click();
		await page.goto(filesUrl(projectId, "/starred"));
		await expect(item(page, "Quarterly review.md")).toBeVisible();

		// Open it from the file list: it shows up in Recent.
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Governance");
		await item(page, "Quarterly review.md").dblclick();
		await expect(page).toHaveURL(EDITOR_URL(projectId));
		await page.goto(filesUrl(projectId, "/recent"));
		await expect(item(page, "Quarterly review.md")).toBeVisible();

		// Search finds it by name.
		await page.goto(filesUrl(projectId));
		await page.getByPlaceholder(/^Search in/).fill("quarterly");
		await expect(item(page, "Quarterly review.md")).toBeVisible();
	});

	test("a document goes to the trash and comes back", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("doctrash"));
		const governance = await createFolder(request, projectId, "Governance");
		const docId = await createDoc(request, projectId, "Minutes", {
			folderId: governance,
		});

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Governance");

		await item(page, "Minutes.md").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Move to trash" }).click();
		await expect(item(page, "Minutes.md")).toHaveCount(0);
		// Gone for the docs side too.
		expect(await docStatus(request, projectId, docId)).toBe(404);

		await page.goto(filesUrl(projectId, "/trash"));
		await expect(item(page, "Minutes.md")).toBeVisible();
		await item(page, "Minutes.md").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Restore" }).click();
		await expect(item(page, "Minutes.md")).toHaveCount(0);
		await expect.poll(() => docStatus(request, projectId, docId)).toBe(200);

		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Governance");
		await expect(item(page, "Minutes.md")).toBeVisible();
	});

	test("trashing a documentation folder takes its documents and files, and restoring brings them back", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("foldertrash"));
		const research = await createFolder(request, projectId, "Research");
		const docId = await createDoc(request, projectId, "Findings", {
			folderId: research,
		});

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Research");
		await setUploadFiles(page, [
			{ name: "survey.txt", mimeType: "text/plain", body: "raw survey data" },
		]);
		await expect(item(page, "survey.txt")).toBeVisible({ timeout: 30_000 });

		// Trash the folder from its parent.
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await item(page, "Research").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Move to trash" }).click();
		await expect(item(page, "Research")).toHaveCount(0);
		expect(await docStatus(request, projectId, docId)).toBe(404);

		// Only the folder is a trash entry.
		await page.goto(filesUrl(projectId, "/trash"));
		await expect(item(page, "Research")).toBeVisible();
		await expect(item(page, "Findings.md")).toHaveCount(0);

		await item(page, "Research").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Restore" }).click();
		await expect.poll(() => docStatus(request, projectId, docId)).toBe(200);

		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Research");
		await expect(item(page, "Findings.md")).toBeVisible();
		await expect(item(page, "survey.txt")).toBeVisible();
	});

	test("the Documentation folder itself cannot be renamed, moved or deleted", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("locked"));
		await signIn(page);
		await page.goto(filesUrl(projectId));

		await item(page, "Documentation").click({ button: "right" });
		await expect(page.getByRole("menuitem", { name: "Rename" })).toHaveCount(0);
		await expect(page.getByRole("menuitem", { name: "Move to…" })).toHaveCount(
			0,
		);
		await expect(
			page.getByRole("menuitem", { name: "Move to trash" }),
		).toHaveCount(0);
	});

	test("the editor breadcrumb shows the location and opens folders in Files", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectName = uniqueName("crumbs");
		const projectId = await createProject(request, projectName);
		const governance = await createFolder(request, projectId, "Governance");
		const docId = await createDoc(request, projectId, "Charter", {
			folderId: governance,
		});

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/docs/${docId}`);
		const crumbs = breadcrumb(page);
		await expect(crumbs.getByText(projectName)).toBeVisible();
		await expect(crumbs.getByText("Documentation")).toBeVisible();
		await expect(crumbs.getByText("Governance")).toBeVisible();
		await expect(crumbs.getByText("Charter")).toBeVisible();

		// An ancestor opens that folder in Files, where the same breadcrumb shows it.
		await crumbs.getByRole("button", { name: "Governance" }).click();
		await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/files`));
		await expect(item(page, "Charter.md")).toBeVisible();
		await expect(breadcrumb(page).getByText("Governance")).toBeVisible();
	});

	test("files in a documentation folder show in the docs sidebar", async ({
		page,
		request,
		isMobile,
	}) => {
		test.skip(
			isMobile,
			"the docs tree sits behind the sidebar toggle on mobile",
		);
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("sidebar"));
		await createFolder(request, projectId, "Suppliers");

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await openFolder(page, "Documentation");
		await openFolder(page, "Suppliers");
		await setUploadFiles(page, [
			{ name: "quote.txt", mimeType: "text/plain", body: "supplier quote" },
		]);
		await expect(item(page, "quote.txt")).toBeVisible({ timeout: 30_000 });

		// The docs tree lists the same file inside its folder.
		await page.goto(`${BASE_URL}/projects/${projectId}/docs`);
		await page.getByRole("button", { name: "Suppliers" }).click();
		await expect(page.getByRole("button", { name: "quote.txt" })).toBeVisible();
	});
});
