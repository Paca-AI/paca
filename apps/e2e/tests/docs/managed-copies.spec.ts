// spec: features/docs/managed-copies.feature
// seed: tests/seed.spec.ts
//
// A managed copy is a document an agent pushes from a file in a code
// repository (PUT /docs/by-path with a source). The page says where it comes
// from; a locked copy is read-only in Paca and cannot be renamed from the
// tree, and locking a copy while it is open stops further edits everywhere.

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
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_MANAGED_";
const RUN_ID = newRunId();

const REPOSITORY = "github.com/acme/app";
const FILE_PATH = "docs/plan.md";
const COMMIT = "0123456789abcdef";
const COPY_OF = `Copy of ${FILE_PATH} in ${REPOSITORY}`;
const LOCKED_TEXT = "Locked: edit the file in the repository and push it.";
const UNLOCKED_TEXT =
	"Edits made here are overwritten by the next push unless they are merged into the file.";
const NOT_SAVED_TEXT = "Your last edit was not saved.";

interface StoredDoc {
	id: string;
	title: string;
	folder_id?: string | null;
	content: unknown;
	source?: { repository: string; path: string; commit?: string };
	source_locked: boolean;
}

const paragraph = (text: string) => ({
	type: "paragraph",
	content: [{ type: "text", text, styles: {} }],
});

/** Seeds a managed copy the way `push` does. */
async function pushCopy(
	request: APIRequestContext,
	projectId: string,
	path: string,
	{ lock, text = "Pushed text" }: { lock: boolean; text?: string },
): Promise<StoredDoc> {
	const res = await request.put(
		`${API_URL}/projects/${projectId}/docs/by-path`,
		{
			data: {
				path,
				content: [paragraph(text)],
				source: { repository: REPOSITORY, path: FILE_PATH, commit: COMMIT },
				lock,
			},
		},
	);
	expect(res.ok(), await res.text()).toBeTruthy();
	return (await res.json()).data as StoredDoc;
}

async function readDoc(
	request: APIRequestContext,
	projectId: string,
	docId: string,
): Promise<StoredDoc> {
	const res = await request.get(
		`${API_URL}/projects/${projectId}/docs/${docId}`,
	);
	expect(res.ok(), await res.text()).toBeTruthy();
	return (await res.json()).data as StoredDoc;
}

async function setLock(
	request: APIRequestContext,
	projectId: string,
	docId: string,
	locked: boolean,
) {
	const res = await request.patch(
		`${API_URL}/projects/${projectId}/docs/${docId}`,
		{ data: { source_locked: locked } },
	);
	expect(res.ok(), await res.text()).toBeTruthy();
}

const editorOf = (page: Page) => page.locator(".bn-editor").first();
const banner = (page: Page) =>
	page.getByRole("region", { name: new RegExp(`^${COPY_OF}`) });

/** The editor accepts typing (and, for a managed copy, is not locked). */
async function expectEditable(page: Page, editable: boolean) {
	await expect(editorOf(page)).toHaveAttribute(
		"contenteditable",
		editable ? "true" : "false",
		{ timeout: 15_000 },
	);
}

/** A second, independent browser session signed in as the admin. */
async function openSecondBrowser(browser: Browser, url: string) {
	const context = await browser.newContext();
	const page = await context.newPage();
	await signIn(page);
	await page.goto(url);
	return { context, page };
}

/** Types at the end of the editor's last paragraph. */
async function typeAtEnd(page: Page, text: string) {
	await editorOf(page).locator(".bn-block-content").last().click();
	await page.keyboard.press(
		process.platform === "darwin" ? "Meta+ArrowDown" : "Control+End",
	);
	await page.keyboard.type(text);
}

/** Opens the Documentation section of the sidebar (a Sheet on phones). */
async function openDocsTree(page: Page, projectId: string) {
	await page.goto(`${BASE_URL}/projects/${projectId}`);
	const viewport = page.viewportSize();
	if (viewport && viewport.width < 768) {
		await page
			.getByRole("main")
			.getByRole("button", { name: "Toggle Sidebar" })
			.click();
	}
	await expect(page.getByText("Documentation", { exact: true })).toBeVisible({
		timeout: 10_000,
	});
}

// Each test deletes only the project it made: copies of one test may run in
// parallel workers (--repeat-each), and a cleanup by prefix would delete the
// project another copy is using.
let ownProjects: string[] = [];

async function newProject(
	request: APIRequestContext,
	label: string,
): Promise<string> {
	const id = await createProject(
		request,
		`${PREFIX}${label}_${RUN_ID}_${test.info().repeatEachIndex}`,
	);
	ownProjects.push(id);
	return id;
}

test.beforeEach(async ({ request, context }) => {
	ownProjects = [];
	await authRequest(request);
	await context.clearCookies();
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await Promise.all(
		ownProjects.map((id) => request.delete(`${API_URL}/projects/${id}`)),
	);
});

test.describe("Managed copies", () => {
	test("a managed copy shows where it comes from", async ({
		page,
		request,
	}) => {
		const projectId = await newProject(request, "SHOW");
		const copy = await pushCopy(request, projectId, "Plan", { lock: false });
		const plainRes = await request.post(
			`${API_URL}/projects/${projectId}/docs`,
			{
				data: { title: "Plain", content: [paragraph("Plain text")] },
			},
		);
		const plain = (await plainRes.json()).data as StoredDoc;

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/docs/${copy.id}`);

		await expect(banner(page)).toContainText(`${COPY_OF} at 0123456`);
		await expect(banner(page)).toContainText(UNLOCKED_TEXT);
		await expect(
			banner(page).getByRole("button", { name: "Lock" }),
		).toBeVisible();
		await expectEditable(page, true);

		// The tree marks the copy; the marker's text is part of the row's name.
		await expect(
			page.getByRole("button", { name: new RegExp(`^Plan.*${COPY_OF}`) }),
		).toBeVisible();

		// A document created in Paca looks as it always did.
		await page.goto(`${BASE_URL}/projects/${projectId}/docs/${plain.id}`);
		await expect(editorOf(page)).toContainText("Plain text");
		await expect(page.getByText(/^Copy of /)).toHaveCount(0);
		await expect(
			page.getByRole("button", { name: "Plain", exact: true }),
		).toBeVisible();
		await expectEditable(page, true);
	});

	test("a locked managed copy is read-only and can be unlocked", async ({
		page,
		request,
	}) => {
		const projectId = await newProject(request, "UNLOCK");
		const copy = await pushCopy(request, projectId, "Plan", { lock: true });

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/docs/${copy.id}`);

		await expect(banner(page)).toContainText(LOCKED_TEXT);
		await expect(editorOf(page)).toContainText("Pushed text");
		await expectEditable(page, false);

		// Unlocking brings editing back without a reload.
		await banner(page).getByRole("button", { name: "Unlock" }).click();
		await expect(banner(page)).toContainText(UNLOCKED_TEXT);
		await expect(
			banner(page).getByRole("button", { name: "Lock" }),
		).toBeVisible();
		await expectEditable(page, true);

		await typeAtEnd(page, " edited-in-paca");
		await page.keyboard.press("Control+s");
		await expect
			.poll(
				async () =>
					JSON.stringify((await readDoc(request, projectId, copy.id)).content),
				{
					timeout: 15_000,
				},
			)
			.toContain("edited-in-paca");
		expect((await readDoc(request, projectId, copy.id)).source_locked).toBe(
			false,
		);
	});

	test("locking a document that is open stops further edits", async ({
		page,
		request,
		browser,
	}) => {
		const projectId = await newProject(request, "LOCK");
		const copy = await pushCopy(request, projectId, "Plan", { lock: false });
		const url = `${BASE_URL}/projects/${projectId}/docs/${copy.id}`;

		await signIn(page);
		await page.goto(url);
		const other = await openSecondBrowser(browser, url);
		try {
			for (const p of [page, other.page]) {
				await expect(editorOf(p)).toContainText("Pushed text", {
					timeout: 20_000,
				});
				await expectEditable(p, true);
			}

			// One browser locks it: both turn read-only, the other one through
			// the realtime event.
			await banner(page).getByRole("button", { name: "Lock" }).click();
			for (const p of [page, other.page]) {
				await expect(banner(p)).toContainText(LOCKED_TEXT, {
					timeout: 15_000,
				});
				await expectEditable(p, false);
			}

			// A push while it is locked reaches both open editors.
			await pushCopy(request, projectId, "Plan", {
				lock: true,
				text: "Second push",
			});
			for (const p of [page, other.page]) {
				await expect(editorOf(p)).toContainText("Second push", {
					timeout: 15_000,
				});
			}

			// Unlocking brings both back into the shared editing session, on
			// the stored content: what one types shows up live in the other.
			await banner(other.page).getByRole("button", { name: "Unlock" }).click();
			for (const p of [page, other.page]) {
				await expectEditable(p, true);
				await expect(editorOf(p)).toContainText("Second push");
				await expect(editorOf(p)).not.toContainText("Pushed text");
			}
			await typeAtEnd(page, " live-after-unlock");
			await expect(editorOf(other.page)).toContainText("live-after-unlock", {
				timeout: 15_000,
			});
		} finally {
			await other.context.close();
		}
	});

	test("a save that races a lock is dropped and not retried", async ({
		page,
		request,
	}) => {
		const projectId = await newProject(request, "RACE");
		const copy = await pushCopy(request, projectId, "Plan", { lock: false });

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/docs/${copy.id}`);
		await expect(editorOf(page)).toContainText("Pushed text", {
			timeout: 20_000,
		});
		await expectEditable(page, true);

		// Hold the content save until the document has been locked, so the
		// server answers it with 409 DOC_LOCKED.
		const contentSaves: string[] = [];
		const held: (() => Promise<void>)[] = [];
		let holding = true;
		await page.route(
			`**/api/v1/projects/${projectId}/docs/${copy.id}`,
			async (route) => {
				const req = route.request();
				if (
					req.method() !== "PATCH" ||
					!req.postData()?.includes('"content"')
				) {
					return route.continue();
				}
				contentSaves.push(req.postData() ?? "");
				if (!holding) return route.continue();
				held.push(() => route.continue());
			},
		);

		await typeAtEnd(page, " lost-edit");
		await page.keyboard.press("Control+s");
		await expect.poll(() => held.length, { timeout: 10_000 }).toBe(1);

		await setLock(request, projectId, copy.id, true);
		holding = false;
		for (const go of held.splice(0)) await go();

		await expect(page.getByText(NOT_SAVED_TEXT)).toHaveCount(1, {
			timeout: 15_000,
		});
		await expect(banner(page)).toContainText(LOCKED_TEXT);
		await expectEditable(page, false);
		// The rejected edit is not left on screen.
		await expect(editorOf(page)).not.toContainText("lost-edit");

		// Past the autosave delay (3 s): no retry and no second save.
		await page.waitForTimeout(4_000);
		expect(contentSaves).toHaveLength(1);
		expect(
			JSON.stringify((await readDoc(request, projectId, copy.id)).content),
		).not.toContain("lost-edit");
	});

	test("a locked document cannot be renamed from the tree but can be moved", async ({
		page,
		request,
	}) => {
		const projectId = await newProject(request, "TREE");
		const copy = await pushCopy(request, projectId, "Guides/Plan", {
			lock: true,
		});
		expect(copy.folder_id).toBeTruthy();

		await signIn(page);
		await openDocsTree(page, projectId);
		await page.getByRole("button", { name: "Guides", exact: true }).click();

		const row = page.getByRole("button", {
			name: new RegExp(
				`^Plan.*${COPY_OF}.*${LOCKED_TEXT.replace(/\./g, "\\.")}`,
			),
		});
		await expect(row).toBeVisible({ timeout: 10_000 });
		await expect(page.getByTestId("doc-locked-marker")).toBeVisible();

		await row.locator("..").getByRole("button").last().click({ force: true });
		const rename = page.getByRole("menuitem", { name: /Rename/ });
		await expect(rename).toBeDisabled();
		await expect(rename).toHaveAttribute("title", LOCKED_TEXT);

		await page.getByRole("menuitem", { name: "Move to top level" }).click();
		await expect
			.poll(
				async () =>
					(await readDoc(request, projectId, copy.id)).folder_id ?? null,
				{
					timeout: 10_000,
				},
			)
			.toBeNull();
		const moved = await readDoc(request, projectId, copy.id);
		expect(moved.title).toBe("Plan");
		expect(moved.source_locked).toBe(true);
	});
});
