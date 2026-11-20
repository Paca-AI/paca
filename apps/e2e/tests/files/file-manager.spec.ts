// spec: docs/product/file-management.md
// seed: tests/seed.spec.ts
//
// Drive-style file manager: default folders, uploading, folders, starring,
// trash and restore, and task attachments showing up in their default folder.

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

const TEST_PREFIX = "E2E_FILES_";
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

async function uploadViaPicker(page: Page, name: string, body: string) {
	// The New menu clicks a hidden input; set the files on it directly.
	await page
		.locator('input[type="file"]:not([webkitdirectory])')
		.setInputFiles({
			name,
			mimeType: "text/plain",
			buffer: Buffer.from(body),
		});
}

/** Uploads a file to a task through the attachment API (presigned PUT, then complete). */
async function attachToTask(
	request: APIRequestContext,
	projectId: string,
	taskId: string,
	fileName: string,
	body: string | Buffer,
	contentType = "text/plain",
): Promise<string> {
	const base = `${API_URL}/projects/${projectId}/tasks/${taskId}/attachments`;
	const init = await request.post(`${base}/initiate-upload`, {
		data: {
			file_name: fileName,
			content_type: contentType,
			file_size: Buffer.byteLength(body),
		},
	});
	expect(init.ok()).toBeTruthy();
	const session = (await init.json()).data as {
		file_id: string;
		upload_url: string;
	};
	const put = await request.put(session.upload_url, {
		data: body,
		headers: { "Content-Type": contentType },
	});
	expect(put.ok()).toBeTruthy();
	const done = await request.post(`${base}/complete-upload`, {
		data: { file_id: session.file_id },
	});
	expect(done.ok()).toBeTruthy();
	return (await done.json()).data.id as string;
}

test.describe("File manager", () => {
	test.beforeEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
	});
	test.afterAll(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
	});

	test("a project starts with its default folders, listed in the sidebar section", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("defaults"));
		await signIn(page);
		await page.goto(filesUrl(projectId));

		for (const name of [
			"Task attachments",
			"Document files",
			"Annotation screenshots",
			"Documentation",
		]) {
			await expect(item(page, name)).toBeVisible();
		}

		// The Files section sits beside Documentation with its four entries.
		// On mobile the sidebar is collapsed behind a toggle.
		const viewport = page.viewportSize();
		const isMobile = !!viewport && viewport.width <= 768;
		if (isMobile) {
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}
		for (const label of ["Files", "Recent", "Starred", "Trash"]) {
			await expect(
				page.getByRole("link", { name: label, exact: true }),
			).toBeVisible();
		}
		if (isMobile) {
			// The sidebar is a modal Sheet; close it so <main> is interactive again.
			await page.keyboard.press("Escape");
			await expect(page.getByRole("dialog", { name: "Sidebar" })).toBeHidden();
		}

		// Default folders cannot be renamed, moved or deleted.
		await item(page, "Task attachments").click({ button: "right" });
		await expect(page.getByRole("menuitem", { name: "Rename" })).toHaveCount(0);
		await expect(
			page.getByRole("menuitem", { name: "Move to trash" }),
		).toHaveCount(0);
	});

	test("create a folder, upload into it, preview, star, trash and restore", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("flow"));
		await signIn(page);
		await page.goto(filesUrl(projectId));

		// New folder (the New button is at the top right of the page).
		await page.getByRole("button", { name: "New" }).click();
		await page.getByRole("menuitem", { name: "New folder" }).click();
		await page.getByLabel("Folder name").fill("Specs");
		await page.getByRole("button", { name: "Create" }).click();
		await expect(item(page, "Specs")).toBeVisible();

		// Open it (double click) and upload a file there.
		await item(page, "Specs").dblclick();
		await expect(breadcrumb(page).getByText("Specs")).toBeVisible(); // breadcrumb: the current folder
		await uploadViaPicker(page, "notes.txt", "hello from the file manager");
		await expect(item(page, "notes.txt")).toBeVisible({ timeout: 30_000 });

		// Preview opens the viewer with the file's content.
		await item(page, "notes.txt").dblclick();
		await expect(page.getByText("hello from the file manager")).toBeVisible({
			timeout: 30_000,
		});
		await page.keyboard.press("Escape");

		// Star it: it appears under Starred.
		await item(page, "notes.txt").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Add to starred" }).click();
		await page.goto(filesUrl(projectId, "/starred"));
		await expect(item(page, "notes.txt")).toBeVisible();

		// Trash it from the folder, find it in Trash, restore it.
		await page.goBack();
		await page.goto(filesUrl(projectId));
		await item(page, "Specs").dblclick();
		await item(page, "notes.txt").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Move to trash" }).click();
		await expect(item(page, "notes.txt")).toHaveCount(0);

		await page.goto(filesUrl(projectId, "/trash"));
		await expect(item(page, "notes.txt")).toBeVisible();
		await item(page, "notes.txt").click({ button: "right" });
		await page.getByRole("menuitem", { name: "Restore" }).click();
		await expect(item(page, "notes.txt")).toHaveCount(0);

		await page.goto(filesUrl(projectId));
		await item(page, "Specs").dblclick();
		await expect(item(page, "notes.txt")).toBeVisible();
	});

	test("a task attachment appears in the task's default folder and is deleted with it", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("attach"));
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: "Attach here" },
		});
		expect(task.ok()).toBeTruthy();
		const taskId = (await task.json()).data.id as string;
		await attachToTask(
			request,
			projectId,
			taskId,
			"spec.txt",
			"attached to a task",
		);

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await item(page, "Task attachments").dblclick();
		await expect(
			breadcrumb(page).getByText("Task attachments"),
		).toBeVisible(); // breadcrumb: the folder opened
		// One subfolder per task, named after the task key.
		const taskFolder = page.locator("[data-file-id]").first();
		await expect(taskFolder).toBeVisible();
		await taskFolder.dblclick();
		await expect(item(page, "spec.txt")).toBeVisible();

		// The file is managed: it cannot be renamed, but deleting it is permanent.
		await item(page, "spec.txt").click({ button: "right" });
		await expect(page.getByRole("menuitem", { name: "Rename" })).toHaveCount(0);
		await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
		await page.getByRole("button", { name: "Delete", exact: true }).click();
		await expect(item(page, "spec.txt")).toHaveCount(0);

		const attachments = await request.get(
			`${API_URL}/projects/${projectId}/tasks/${taskId}/attachments`,
		);
		expect((await attachments.json()).data.items).toHaveLength(0);
	});

	test("clicking a task attachment opens the in-app preview instead of downloading", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("taskpreview"));
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: "Preview attachment" },
		});
		expect(task.ok()).toBeTruthy();
		const taskId = (await task.json()).data.id as string;
		await attachToTask(
			request,
			projectId,
			taskId,
			"readme.txt",
			"shown in the preview",
		);

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
		const downloads: string[] = [];
		page.on("download", (d) => downloads.push(d.suggestedFilename()));
		const popups: string[] = [];
		page.context().on("page", (p) => popups.push(p.url()));

		await page.getByRole("button", { name: /Preview readme\.txt/i }).click();
		await expect(page.getByText("shown in the preview")).toBeVisible({
			timeout: 30_000,
		});
		expect(downloads).toHaveLength(0);
		expect(popups).toHaveLength(0); // no new browser tab either
	});

	test("clicking an inline image in the task description opens the preview", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("inline"));
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: "Inline image" },
		});
		expect(task.ok()).toBeTruthy();
		const taskId = (await task.json()).data.id as string;
		const png = Buffer.from(
			"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
			"base64",
		);
		const attachmentId = await attachToTask(
			request,
			projectId,
			taskId,
			"pixel.png",
			png,
			"image/png",
		);
		const update = await request.patch(
			`${API_URL}/projects/${projectId}/tasks/${taskId}`,
			{
				data: {
					description: [
						{
							type: "image",
							props: {
								url: `attachment://${projectId}/${taskId}/${attachmentId}`,
								name: "pixel.png",
								showPreview: true,
							},
						},
					],
				},
			},
		);
		expect(update.ok()).toBeTruthy();

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
		const popups: string[] = [];
		page.context().on("page", (p) => popups.push(p.url()));
		await page.locator(".bn-visual-media-wrapper").first().click();
		// The viewer overlay shows the file name with a "1/1" position counter.
		await expect(page.getByText("1/1")).toBeVisible({ timeout: 30_000 });
		await expect(page.getByText("pixel.png").first()).toBeVisible();
		expect(popups).toHaveLength(0);
	});

	/** Uploads a file into a project's Files through the file manager API. */
	async function uploadToFiles(
		request: APIRequestContext,
		projectId: string,
		name: string,
		body: Buffer,
		contentType: string,
	): Promise<string> {
		const base = `${API_URL}/projects/${projectId}/files/uploads`;
		const init = await request.post(`${base}/initiate`, {
			data: {
				file_name: name,
				content_type: contentType,
				file_size: body.length,
			},
		});
		expect(init.ok()).toBeTruthy();
		const session = (await init.json()).data as {
			file_id: string;
			upload_url: string;
		};
		const put = await request.put(session.upload_url, {
			data: body,
			headers: { "Content-Type": contentType },
		});
		expect(put.ok()).toBeTruthy();
		const done = await request.post(`${base}/complete`, {
			data: { file_id: session.file_id },
		});
		expect(done.ok()).toBeTruthy();
		return session.file_id;
	}

	const PIXEL = Buffer.from(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
		"base64",
	);

	test("clicking a file block in the task description opens the preview", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("fileblock"));
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: "File block" },
		});
		const taskId = (await task.json()).data.id as string;
		const attachmentId = await attachToTask(
			request,
			projectId,
			taskId,
			"spec.txt",
			"the file block content",
		);
		await request.patch(`${API_URL}/projects/${projectId}/tasks/${taskId}`, {
			data: {
				description: [
					{
						type: "file",
						props: {
							url: `attachment://${projectId}/${taskId}/${attachmentId}`,
							name: "spec.txt",
						},
					},
				],
			},
		});

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
		const popups: string[] = [];
		page.context().on("page", (p) => popups.push(p.url()));
		await page.locator(".bn-file-name-with-icon").first().click();
		await expect(page.getByText("the file block content")).toBeVisible({
			timeout: 30_000,
		});
		expect(popups).toHaveLength(0);
	});

	test("choose an image that is already in Files from the description's file panel", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("picker"));
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: "Pick from files" },
		});
		const taskId = (await task.json()).data.id as string;
		await uploadToFiles(
			request,
			projectId,
			"from-files.png",
			PIXEL,
			"image/png",
		);
		await uploadToFiles(
			request,
			projectId,
			"notes.txt",
			Buffer.from("not an image"),
			"text/plain",
		);
		// An image block with no file yet shows the "add image" placeholder.
		await request.patch(`${API_URL}/projects/${projectId}/tasks/${taskId}`, {
			data: { description: [{ type: "image", props: { url: "" } }] },
		});

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
		await page.locator(".bn-add-file-button").first().click();
		const panel = page.getByTestId("file-block-panel");
		await expect(panel).toBeVisible();
		await panel.getByRole("tab", { name: "From files" }).click();
		// Only images are offered for an image block.
		await expect(
			panel.getByRole("button", { name: /from-files\.png/ }),
		).toBeVisible();
		await expect(panel.getByText("notes.txt")).toHaveCount(0);
		await panel.getByRole("button", { name: /from-files\.png/ }).click();

		// The block now shows the image, and clicking it opens the preview.
		await expect(page.locator(".bn-visual-media").first()).toBeVisible({
			timeout: 30_000,
		});
		await page.locator(".bn-visual-media-wrapper").first().click();
		await expect(page.getByText("1/1")).toBeVisible({ timeout: 30_000 });
		await expect(page.getByText("from-files.png").first()).toBeVisible();
	});

	test("file blocks keep readable colours when hovered in dark mode", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("dark"));
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title: "Dark file block" },
		});
		const taskId = (await task.json()).data.id as string;
		const attachmentId = await attachToTask(
			request,
			projectId,
			taskId,
			"dark.txt",
			"x",
		);
		await request.patch(`${API_URL}/projects/${projectId}/tasks/${taskId}`, {
			data: {
				description: [
					{
						type: "file",
						props: {
							url: `attachment://${projectId}/${taskId}/${attachmentId}`,
							name: "dark.txt",
						},
					},
				],
			},
		});

		await signIn(page);
		await page.evaluate(() => localStorage.setItem("theme", "dark"));
		await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
		const block = page.locator(".bn-file-name-with-icon").first();
		await expect(block).toBeVisible();
		await block.hover();
		// Not the library's hard-coded light grey (#e1e1e1 = rgb(225, 225, 225)).
		const [r, g, b] = await block.evaluate((el) =>
			getComputedStyle(el)
				.backgroundColor.match(/\d+(\.\d+)?/g)!
				.slice(0, 3)
				.map(Number),
		);
		expect(Math.max(r, g, b)).toBeLessThan(100);
	});

	test("deleting a document file asks first and removes it for good instead of trashing it", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, uniqueName("docfile"));
		const doc = await request.post(`${API_URL}/projects/${projectId}/docs`, {
			data: { title: "With a file" },
		});
		const docId = (await doc.json()).data.id as string;
		const base = `${API_URL}/projects/${projectId}/docs/${docId}/files`;
		const body = Buffer.from("document file body");
		const init = await request.post(`${base}/initiate-upload`, {
			data: {
				file_name: "diagram.txt",
				content_type: "text/plain",
				file_size: body.length,
			},
		});
		expect(init.ok()).toBeTruthy();
		const session = (await init.json()).data as {
			file_id: string;
			upload_url: string;
		};
		expect(
			(
				await request.put(session.upload_url, {
					data: body,
					headers: { "Content-Type": "text/plain" },
				})
			).ok(),
		).toBeTruthy();
		expect(
			(
				await request.post(`${base}/complete-upload`, {
					data: { file_id: session.file_id },
				})
			).ok(),
		).toBeTruthy();

		await signIn(page);
		await page.goto(filesUrl(projectId));
		await item(page, "Document files").dblclick();
		await expect(
			breadcrumb(page).getByText("Document files"),
		).toBeVisible();
		await item(page, "diagram.txt").click({ button: "right" });
		// Offered as Delete, not "Move to trash": it is used by a document.
		await expect(
			page.getByRole("menuitem", { name: "Move to trash" }),
		).toHaveCount(0);
		await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
		await expect(page.getByText(/is used in a document/)).toBeVisible();
		await page.getByRole("button", { name: "Delete", exact: true }).click();
		await expect(item(page, "diagram.txt")).toHaveCount(0);

		// Gone for good: nothing to restore from the trash.
		await page.goto(filesUrl(projectId, "/trash"));
		await expect(page.getByText("Trash is empty")).toBeVisible();
	});
});
