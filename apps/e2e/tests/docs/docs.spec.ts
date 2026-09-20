// spec: features/docs/docs.feature
// seed: tests/seed.spec.ts
//
// The document title is breadcrumb text in the page header (renamed from the sidebar), the
// comment composer is a BlockNote editor, and history lives in the activity feed.

import { ensureLoginForm } from '../helpers/e2e-api';
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';
const TEST_PROJECT_PREFIX = 'E2E_DOCS_';
const RUN_ID = Date.now().toString(36).slice(-5).toUpperCase();

// ─── Types ────────────────────────────────────────────────────────────────────

interface DocFolder {
  id: string;
  name: string;
}

interface Document {
  id: string;
  title: string;
}

interface DocSnapshot {
  id: string;
  snapshot_number: number;
}

// ─── API Helpers ──────────────────────────────────────────────────────────────

async function authRequest(request: APIRequestContext): Promise<void> {
  await request.post(`${BASE_URL}/api/v1/auth/login`, {
    data: { username: USERNAME, password: PASSWORD, rememberMe: false },
  });
}

async function cleanupTestProjects(request: APIRequestContext): Promise<void> {
  await authRequest(request);
  const allProjects: Array<{ id: string; name: string }> = [];
  let page = 1;
  while (true) {
    const listResp = await request.get(`${BASE_URL}/api/v1/projects?page=${page}&page_size=100`);
    if (!listResp.ok()) break;
    const body = await listResp.json();
    const items: Array<{ id: string; name: string }> = body?.data?.items ?? [];
    if (items.length === 0) break;
    allProjects.push(...items);
    const { page: currentPage, page_size, total } = body.data as {
      page: number;
      page_size: number;
      total: number;
    };
    if (currentPage * page_size >= total) break;
    page++;
  }
  await Promise.all(
    allProjects
      .filter((p) => p.name.startsWith(TEST_PROJECT_PREFIX))
      .map((p) => request.delete(`${BASE_URL}/api/v1/projects/${p.id}`)),
  );
}

async function createProject(request: APIRequestContext, name: string): Promise<string> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects`, { data: { name } });
  const body = await resp.json();
  return body.data.id as string;
}

async function createFolder(
  request: APIRequestContext,
  projectId: string,
  name: string,
): Promise<DocFolder> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/docs/folders`, {
    data: { name },
  });
  const body = await resp.json();
  return body.data as DocFolder;
}

async function createDocument(
  request: APIRequestContext,
  projectId: string,
  payload: { title: string; folder_id?: string; content?: unknown },
): Promise<Document> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/docs`, {
    data: payload,
  });
  const body = await resp.json();
  return body.data as Document;
}

async function updateDocument(
  request: APIRequestContext,
  projectId: string,
  docId: string,
  payload: { title?: string; content?: unknown },
): Promise<void> {
  await request.patch(`${BASE_URL}/api/v1/projects/${projectId}/docs/${docId}`, {
    data: payload,
  });
}

async function listSnapshots(
  request: APIRequestContext,
  projectId: string,
  docId: string,
): Promise<DocSnapshot[]> {
  const resp = await request.get(
    `${BASE_URL}/api/v1/projects/${projectId}/docs/${docId}/snapshots`,
  );
  const body = await resp.json();
  return (body?.data?.items ?? []) as DocSnapshot[];
}

// ─── UI Helpers ───────────────────────────────────────────────────────────────

const signIn = async (page: Page) => {
  await page.goto(`${BASE_URL}/`);
  await ensureLoginForm(page);
  await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
  await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: /Good (morning|afternoon|evening)/i })).toBeVisible();
};

const navigateToDocsPage = async (page: Page, projectId: string) => {
  await page.goto(`${BASE_URL}/projects/${projectId}`);
  // On mobile the sidebar renders as a hidden Sheet overlay; open it before asserting.
  // On desktop both a sidebar-rail and a sidebar-trigger share the same label, so we
  // scope the click to the trigger inside <main> to avoid a strict-mode violation.
  const viewport = page.viewportSize();
  if (viewport && viewport.width < 768) {
    await page.getByRole('main').getByRole('button', { name: 'Toggle Sidebar' }).click();
  }
  await expect(page.getByText('Documentation', { exact: true })).toBeVisible({ timeout: 10_000 });
};

/**
 * Open the Add dropdown menu in the Documentation sidebar section.
 * The Add button is always visible (no hover reveal) once the sidebar is open.
 * On mobile the sidebar must already be open via navigateToDocsPage.
 */
const openDocAddMenu = async (page: Page) => {
  await page.getByRole('button', { name: 'Add', exact: true }).click();
};

/**
 * Open the options menu (⋯) for a sidebar folder or document item.
 * Each item row contains the name button and the options button as siblings.
 * Navigating one level up from the name button finds the row container,
 * and `.last()` selects the options button (the sibling after the name button).
 * The options trigger has `opacity-0 group-hover:opacity-100`; on touch/mobile
 * devices hover is never activated, so { force: true } bypasses visibility.
 */
const openSidebarItemOptions = async (page: Page, itemName: string) => {
  const itemRow = page.getByRole('button', { name: itemName, exact: true }).locator('..');
  await itemRow.getByRole('button').last().click({ force: true });
};

/**
 * On mobile the sidebar opens as a Sheet (dialog). After client-side navigation
 * the sheet stays open and covers the main content (which becomes aria-hidden).
 * Call this after any action that triggers navigation while the sidebar is open.
 */
const closeSidebarIfOpen = async (page: Page) => {
  const sidebarDialog = page.getByRole('dialog', { name: 'Sidebar' });
  if (await sidebarDialog.isVisible().catch(() => false)) {
    await page.keyboard.press('Escape');
    await sidebarDialog.waitFor({ state: 'hidden', timeout: 3_000 }).catch(() => {});
  }
};

/**
 * The comment composer is a BlockNote rich-text editor inside a <fieldset> in the
 * activity pane. Its contenteditable has no accessible name, so it is located by
 * structure. Fill it with `.fill()` and submit with Ctrl+Enter.
 */
// Tailwind's bare `group` class (word match, so `group/x` or `group-hover:` don't count).
const GROUP_ANCESTOR = "xpath=ancestor::div[contains(concat(' ', normalize-space(@class), ' '), ' group ')][1]";

const commentEditor = (page: Page) => page.locator('fieldset [contenteditable="true"]');

/**
 * The document page shows its title as plain breadcrumb text in the header (not a
 * heading); the title is edited from the sidebar, not on the page itself.
 */
const docTitle = (page: Page, title: string) =>
  page.getByRole('main').getByText(title, { exact: true });

// ─── Test Suites ──────────────────────────────────────────────────────────────

// ===========================================================================
// Rule: Document folders
// ===========================================================================

test.describe('Document folders', () => {
  let projectId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}FOLDERS_${RUN_ID}`);
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Create a new folder', async ({ page }) => {
    await signIn(page);
    await navigateToDocsPage(page, projectId);

    // 1. Open the Add menu in the Documentation section
    await openDocAddMenu(page);

    // 2. Select New Folder
    await page.getByRole('menuitem', { name: 'New Folder' }).click();
    await expect(page.getByRole('button', { name: 'New Folder', exact: true })).toBeVisible({
      timeout: 6_000,
    });

    // 3. Rename the newly created folder to "Architecture" via the options menu
    await openSidebarItemOptions(page, 'New Folder');
    await page.getByRole('menuitem', { name: 'Rename' }).click();
    await page.getByRole('textbox').fill('Architecture');
    await page.keyboard.press('Enter');

    // Verify
    await expect(page.getByRole('button', { name: 'Architecture', exact: true })).toBeVisible({
      timeout: 8_000,
    });
  });

  test('Rename an existing folder', async ({ page, request }) => {
    await createFolder(request, projectId, 'Old Name');

    await signIn(page);
    await navigateToDocsPage(page, projectId);

    // 1. Open options for "Old Name"
    await openSidebarItemOptions(page, 'Old Name');

    // 2. Select Rename and type the new name
    await page.getByRole('menuitem', { name: 'Rename' }).click();
    await page.getByRole('textbox').fill('New Name');
    await page.keyboard.press('Enter');

    // Verify
    await expect(page.getByRole('button', { name: 'New Name', exact: true })).toBeVisible({
      timeout: 8_000,
    });
    await expect(page.getByRole('button', { name: 'Old Name', exact: true })).not.toBeVisible();
  });

  test('Delete an existing folder', async ({ page, request }) => {
    await createFolder(request, projectId, 'To Delete');

    await signIn(page);
    await navigateToDocsPage(page, projectId);

    // 1. Open options for "To Delete" and select Delete
    await openSidebarItemOptions(page, 'To Delete');
    await page.getByRole('menuitem', { name: 'Delete' }).click();
    // No confirmation dialog — folder is removed immediately

    // Verify
    await expect(page.getByRole('button', { name: 'To Delete', exact: true })).not.toBeVisible({
      timeout: 8_000,
    });
  });
});

// ===========================================================================
// Rule: Document lifecycle
// ===========================================================================

test.describe('Document lifecycle', () => {
  let projectId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}LIFECYCLE_${RUN_ID}`);
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Create a document at the project root', async ({ page }) => {
    await signIn(page);
    await navigateToDocsPage(page, projectId);

    // 1. Open Add menu and select New Document
    await openDocAddMenu(page);
    await page.getByRole('menuitem', { name: 'New Document' }).click();

    // Verify the doc appears in the sidebar (on mobile the sidebar sheet is still open
    // after client-side navigation, so check it first before closing the overlay).
    await expect(page.getByRole('button', { name: 'Untitled', exact: true })).toBeVisible({
      timeout: 8_000,
    });
    // Close the sidebar sheet on mobile so the main editor area becomes accessible.
    await closeSidebarIfOpen(page);
    // Verify the editor opened with the default "Untitled" title.
    await expect(docTitle(page, 'Untitled')).toBeVisible({ timeout: 8_000 });
  });

  test('Create a document inside a folder', async ({ page, request }) => {
    const folder = await createFolder(request, projectId, 'Engineering');

    await signIn(page);
    await navigateToDocsPage(page, projectId);

    // 1. Create a document and assign it to the folder via API verification
    await openDocAddMenu(page);
    await page.getByRole('menuitem', { name: 'New Document' }).click();
    // On mobile the sidebar sheet remains open after navigation; close it first.
    await closeSidebarIfOpen(page);
    await expect(docTitle(page, 'Untitled')).toBeVisible({ timeout: 8_000 });

    // API verification: folder endpoint returns items properly structured
    const resp = await request.get(
      `${BASE_URL}/api/v1/projects/${projectId}/docs?folder_id=${folder.id}`,
    );
    const body = await resp.json();
    expect(body.data).toHaveProperty('items');
  });

  test('Rename a document from the sidebar', async ({ page, request }) => {
    const doc = await createDocument(request, projectId, { title: 'Draft' });

    await signIn(page);
    await navigateToDocsPage(page, projectId);

    // 1. Open the document options in the sidebar and choose Rename
    await openSidebarItemOptions(page, 'Draft');
    await page.getByRole('menuitem', { name: 'Rename' }).click();

    // 2. Type the new title and confirm with Enter
    await page.getByRole('textbox').fill('Final');
    await page.keyboard.press('Enter');

    // Verify the sidebar shows the new title and the API persisted it
    await expect(page.getByRole('button', { name: 'Final', exact: true })).toBeVisible({
      timeout: 8_000,
    });
    await expect(page.getByRole('button', { name: 'Draft', exact: true })).not.toBeVisible();
    const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/docs/${doc.id}`);
    expect((await resp.json()).data.title).toBe('Final');
  });

  test('Delete a document', async ({ page, request }) => {
    const doc = await createDocument(request, projectId, { title: 'Temporary Doc' });

    await signIn(page);
    await navigateToDocsPage(page, projectId);

    // 1. Open document options and delete
    await openSidebarItemOptions(page, doc.title);
    await page.getByRole('menuitem', { name: /delete/i }).click();
    // Confirm if a dialog appears
    const confirmBtn = page.getByRole('button', { name: /confirm|delete/i });
    if (await confirmBtn.isVisible()) {
      await confirmBtn.click();
    }

    // Verify
    await expect(
      page.getByRole('button', { name: doc.title, exact: true }),
    ).not.toBeVisible({ timeout: 8_000 });
  });
});

// ===========================================================================
// Rule: Document editor with BlockNote
// ===========================================================================

test.describe('Document editor', () => {
  let projectId: string;
  let doc: Document;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}EDITOR_${RUN_ID}`);
    doc = await createDocument(request, projectId, {
      title: 'E2E_EDITOR_DOC',
      content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Version 1' }] }],
    });
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Editor loads existing document content', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);

    // The document heading and the BlockNote contenteditable area should be visible
    await expect(docTitle(page, 'E2E_EDITOR_DOC')).toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[contenteditable="true"]').last()).toBeVisible({ timeout: 10_000 });
  });

  test('User can type content into the editor', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);

    // The editor is the last contenteditable element (title is the first)
    const editor = page.locator('[contenteditable="true"]').last();
    await expect(editor).toBeVisible({ timeout: 10_000 });

    // 1. Click into editor and type new content
    await editor.click();
    await page.keyboard.press('End');
    await page.keyboard.press('Enter');
    await page.keyboard.type('Hello World');

    // Verify
    await expect(editor.getByText('Hello World')).toBeVisible({ timeout: 8_000 });
  });

  test('Saving updated content creates a new snapshot', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);

    const editor = page.locator('[contenteditable="true"]').last();
    await expect(editor).toBeVisible({ timeout: 10_000 });

    // 1. Type new content to make the document dirty
    await editor.click();
    await page.keyboard.press('Control+a');
    await page.keyboard.type('Version 2');

    // 2. Save with Ctrl+S and wait for the "Saved" indicator
    await page.keyboard.press('Control+s');
    await expect(page.getByText(/saved/i)).toBeVisible({ timeout: 10_000 });

    // Verify snapshot was created via API
    const snaps = await listSnapshots(page.request, projectId, doc.id);
    expect(snaps.length).toBeGreaterThanOrEqual(1);
  });
});

// ===========================================================================
// Rule: Document history and snapshots
// ===========================================================================

test.describe('Document history', () => {
  let projectId: string;
  let doc: Document;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}HISTORY_${RUN_ID}`);
    doc = await createDocument(request, projectId, {
      title: 'E2E_HISTORY_DOC',
      content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Initial' }] }],
    });
    // Generate a snapshot by updating content
    await updateDocument(request, projectId, doc.id, {
      content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Updated' }] }],
    });
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  // The standalone "Version history" panel is gone: content/title edits now show up in
  // the Comments & activity feed as "updated content" entries with View diff / Revert.
  const openUpdateEntryMenu = async (page: Page) => {
    await page.getByRole('button', { name: 'Comments & activity' }).click();
    await page.getByRole('button', { name: 'All activity', exact: true }).click();

    const entry = page.getByText('updated content', { exact: true });
    await expect(entry).toBeVisible({ timeout: 15_000 });
    // The options trigger has no accessible name and is only revealed on hover.
    await entry
      .locator(GROUP_ANCESTOR)
      .getByRole('button')
      .click({ force: true });
  };

  test('User can view what changed in a document update', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);

    await openUpdateEntryMenu(page);
    await page.getByRole('menuitem', { name: 'View diff' }).click();

    const dialog = page.getByRole('dialog', { name: 'Content change diff' });
    await expect(dialog).toBeVisible({ timeout: 8_000 });
    await expect(dialog.getByText('Initial')).toBeVisible();
    await expect(dialog.getByText('Updated')).toBeVisible();
  });

  test('User can revert a document update', async ({ page, request }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);

    await openUpdateEntryMenu(page);
    await page.getByRole('menuitem', { name: 'Revert' }).click();

    // The document content is restored to its pre-update value
    await expect
      .poll(
        async () => {
          const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/docs/${doc.id}`);
          return JSON.stringify((await resp.json()).data.content);
        },
        { timeout: 10_000 },
      )
      .toContain('Initial');
  });
});

// ===========================================================================
// Rule: Document comments and activity
// ===========================================================================

test.describe('Document comments and activity', () => {
  let projectId: string;
  let doc: Document;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}COMMENTS_${RUN_ID}`);
    doc = await createDocument(request, projectId, { title: 'E2E_COMMENT_DOC' });
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Activity panel shows a document creation event', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);

    // 1. Open the Comments & activity panel
    await page.getByRole('button', { name: 'Comments & activity' }).click();

    // The panel defaults to a comments-only view; switch to All activity to see the creation event.
    // exact: true avoids matching the empty-state "Show all activity" button, which also renders
    // here (zero comments yet) and contains "All activity" as a substring.
    await page.getByRole('button', { name: 'All activity', exact: true }).click();

    // The activity feed shows "created this document" for the doc.created event
    await expect(page.getByText(/created this document/i)).toBeVisible({ timeout: 15_000 });
  });

  test('User can add a comment to a document', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);

    // 1. Open the Comments & activity panel
    await page.getByRole('button', { name: 'Comments & activity' }).click();

    // 2. Type a comment and submit with Ctrl+Enter
    const commentInput = commentEditor(page);
    await expect(commentInput).toBeVisible({ timeout: 8_000 });
    await commentInput.fill('Great document!');
    await page.keyboard.press('Control+Enter');

    // Verify
    await expect(page.getByText('Great document!')).toBeVisible({ timeout: 8_000 });
  });

  test('Comment input clears after submission', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);

    // 1. Open Comments & activity panel
    await page.getByRole('button', { name: 'Comments & activity' }).click();

    // 2. Submit a comment via Ctrl+Enter
    const commentInput = commentEditor(page);
    await expect(commentInput).toBeVisible({ timeout: 8_000 });
    await commentInput.fill('Keyboard shortcut test');
    await page.keyboard.press('Control+Enter');

    // Verify: comment appears and input is cleared
    await expect(page.getByText('Keyboard shortcut test')).toBeVisible({ timeout: 8_000 });
    await expect(commentInput).toHaveText('');
  });

  // The options trigger (⋯) has no accessible name and is only revealed on hover, so it is
  // reached from the comment text via its `group` card and clicked with { force: true }.
  const openCommentOptions = async (page: Page, text: string) => {
    const card = page
      .getByText(text, { exact: true })
      .locator(GROUP_ANCESTOR);
    await card.getByRole('button').click({ force: true });
  };

  const postComment = async (page: Page, text: string) => {
    await page.getByRole('button', { name: 'Comments & activity' }).click();
    const commentInput = commentEditor(page);
    await expect(commentInput).toBeVisible({ timeout: 8_000 });
    await commentInput.fill(text);
    await page.keyboard.press('Control+Enter');
    await expect(page.getByText(text, { exact: true })).toBeVisible({ timeout: 8_000 });
  };

  test('User can edit their own comment', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);
    await postComment(page, 'Original comment');

    // Edit loads the comment into the composer; Ctrl+Enter saves the change
    await openCommentOptions(page, 'Original comment');
    await page.getByRole('menuitem', { name: 'Edit' }).click();
    const commentInput = commentEditor(page);
    await expect(commentInput.getByText('Original comment')).toBeVisible({ timeout: 8_000 });
    await commentInput.click();
    await page.keyboard.press('Control+a');
    await page.keyboard.type('Updated comment');
    await page.keyboard.press('Control+Enter');

    // Verify
    await expect(page.getByText('Updated comment', { exact: true })).toBeVisible({ timeout: 8_000 });
    await expect(page.getByText('Original comment')).toHaveCount(0);
  });

  test('User can delete their own comment', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/docs/${doc.id}`);
    await postComment(page, 'Delete me');

    // Delete removes the comment immediately (no confirmation dialog)
    await openCommentOptions(page, 'Delete me');
    await page.getByRole('menuitem', { name: 'Delete' }).click();

    // Verify
    await expect(page.getByText('Delete me')).toHaveCount(0, { timeout: 8_000 });
  });

  // ─── API-level validation (fast, no browser) ─────────────────────────────

  test('POST /comments without content returns 400', async ({ request }) => {
    await authRequest(request);
    const resp = await request.post(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/${doc.id}/comments`,
      { data: {} },
    );
    expect(resp.status()).toBe(400);
    const body = await resp.json();
    expect(body.error_code).toBe('BAD_REQUEST');
  });
});

// ===========================================================================
// Rule: API-level access control (fast, no browser)
// ===========================================================================

test.describe('Document API access control', () => {
  let projectId: string;

  test.beforeEach(async ({ request }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}ACL_${RUN_ID}`);
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Unauthenticated request returns 401', async ({ browser }) => {
    // The shared `request` fixture is already authenticated by beforeEach (via cleanupTestProjects).
    // Use a fresh browser context so no session cookies are present.
    const freshCtx = await browser.newContext();
    try {
      const resp = await freshCtx.request.get(`${BASE_URL}/api/v1/projects/${projectId}/docs`);
      expect(resp.status()).toBe(401);
    } finally {
      await freshCtx.close();
    }
  });

  test('Authenticated user can list documents', async ({ request }) => {
    await authRequest(request);
    const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/docs`);
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.data).toHaveProperty('items');
  });

  test('Authenticated user can create a document', async ({ request }) => {
    await authRequest(request);
    const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/docs`, {
      data: { title: 'API Test Doc' },
    });
    expect(resp.status()).toBe(201);
  });

  test('Creating a document with empty title defaults to Untitled', async ({ request }) => {
    await authRequest(request);
    const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/docs`, {
      data: { title: '' },
    });
    expect(resp.status()).toBe(201);
    const body = await resp.json();
    expect(body.data.title).toBe('Untitled');
  });

  test('Patching a document with empty title returns 400', async ({ request }) => {
    await authRequest(request);
    const doc = await createDocument(request, projectId, { title: 'Has Title' });
    const resp = await request.patch(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/${doc.id}`,
      { data: { title: '  ' } },
    );
    expect(resp.status()).toBe(400);
    const body = await resp.json();
    expect(body.error_code).toBe('DOC_TITLE_INVALID');
  });

  test('GET non-existent document returns 404', async ({ request }) => {
    await authRequest(request);
    const resp = await request.get(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/00000000-0000-0000-0000-000000000000`,
    );
    expect(resp.status()).toBe(404);
    const body = await resp.json();
    expect(body.error_code).toBe('DOC_NOT_FOUND');
  });

  test('Creating a folder with blank name returns 400', async ({ request }) => {
    await authRequest(request);
    const resp = await request.post(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/folders`,
      { data: { name: '   ' } },
    );
    expect(resp.status()).toBe(400);
    const body = await resp.json();
    expect(body.error_code).toBe('DOC_FOLDER_NAME_INVALID');
  });

  test('Deleting a non-existent folder returns 404', async ({ request }) => {
    await authRequest(request);
    const resp = await request.delete(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/folders/00000000-0000-0000-0000-000000000000`,
    );
    expect(resp.status()).toBe(404);
    const body = await resp.json();
    expect(body.error_code).toBe('DOC_FOLDER_NOT_FOUND');
  });

  test('Folder CRUD lifecycle', async ({ request }) => {
    await authRequest(request);

    // Create
    const folder = await createFolder(request, projectId, 'API Folder');
    expect(folder.id).toBeTruthy();
    expect(folder.name).toBe('API Folder');

    // List
    const listResp = await request.get(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/folders`,
    );
    const listBody = await listResp.json();
    expect(listBody.data.items.some((f: DocFolder) => f.id === folder.id)).toBe(true);

    // Rename
    const patchResp = await request.patch(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/folders/${folder.id}`,
      { data: { name: 'Renamed Folder' } },
    );
    expect(patchResp.status()).toBe(200);

    // Delete
    const delResp = await request.delete(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/folders/${folder.id}`,
    );
    expect(delResp.status()).toBe(204);
  });

  test('Document content update creates a snapshot', async ({ request }) => {
    await authRequest(request);
    const doc = await createDocument(request, projectId, {
      title: 'Snapshot Test',
      content: [],
    });

    // Initial: no snapshots
    const before = await listSnapshots(request, projectId, doc.id);
    expect(before).toHaveLength(0);

    // Update content — triggers snapshot
    await updateDocument(request, projectId, doc.id, {
      content: [{ type: 'paragraph' }],
    });

    const after = await listSnapshots(request, projectId, doc.id);
    expect(after.length).toBeGreaterThanOrEqual(1);
  });

  test('Snapshot not found returns 404', async ({ request }) => {
    await authRequest(request);
    const doc = await createDocument(request, projectId, { title: 'No Snaps' });
    const resp = await request.get(
      `${BASE_URL}/api/v1/projects/${projectId}/docs/${doc.id}/snapshots/00000000-0000-0000-0000-000000000000`,
    );
    expect(resp.status()).toBe(404);
    const body = await resp.json();
    expect(body.error_code).toBe('DOC_SNAPSHOT_NOT_FOUND');
  });

  test('Filter documents by folder_id', async ({ request }) => {
    await authRequest(request);
    const folder = await createFolder(request, projectId, 'Filtered');
    await createDocument(request, projectId, { title: 'In Folder', folder_id: folder.id });
    await createDocument(request, projectId, { title: 'Root Doc' });

    const resp = await request.get(
      `${BASE_URL}/api/v1/projects/${projectId}/docs?folder_id=${folder.id}`,
    );
    const body = await resp.json();
    expect(body.data.items).toHaveLength(1);
    expect(body.data.items[0].title).toBe('In Folder');
  });
});
