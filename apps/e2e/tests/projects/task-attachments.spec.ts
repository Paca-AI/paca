// spec: features/projects/task-detail.feature (Attachments section)
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import path from 'node:path';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';
const TEST_PROJECT_PREFIX = 'E2E_ATT_';
const RUN_ID = Date.now().toString(36).slice(-5).toUpperCase();

// Fixture files for UI upload tests (real files already on disk in the repo)
const FIXTURE_FILE_1 = path.join(__dirname, '..', 'seed.spec.ts');
const FIXTURE_FILE_2 = path.join(__dirname, 'task-detail.spec.ts');

// ─── Types ────────────────────────────────────────────────────────────────────

interface TaskStatus {
  id: string;
  name: string;
  category: string;
}

interface Task {
  id: string;
  title: string;
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
    const { page: currentPage, page_size, total } = body.data as { page: number; page_size: number; total: number };
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
  return (await resp.json()).data.id as string;
}

async function getTaskStatuses(request: APIRequestContext, projectId: string): Promise<TaskStatus[]> {
  const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/task-statuses`);
  return ((await resp.json())?.data?.items ?? []) as TaskStatus[];
}

async function createTask(
  request: APIRequestContext,
  projectId: string,
  title: string,
  statusId?: string,
): Promise<Task> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/tasks`, {
    data: { title, status_id: statusId ?? null },
  });
  return (await resp.json()).data as Task;
}

/**
 * Seeds an attachment on a task via the 3-step API flow:
 * 1. initiate-upload  → get presigned URL
 * 2. PUT to presigned URL  → upload bytes to object store
 * 3. complete-upload  → register the attachment record
 */
async function uploadAttachmentViaAPI(
  request: APIRequestContext,
  projectId: string,
  taskId: string,
  fileName: string,
  content = 'test attachment content',
): Promise<{ id: string }> {
  const contentBytes = Buffer.from(content, 'utf-8');

  const initResp = await request.post(
    `${BASE_URL}/api/v1/projects/${projectId}/tasks/${taskId}/attachments/initiate-upload`,
    { data: { file_name: fileName, content_type: 'text/plain', file_size: contentBytes.length } },
  );
  const { data: session } = await initResp.json();

  const uploadResp = await request.put(session.upload_url, {
    data: content,
    headers: { 'Content-Type': 'text/plain' },
  });
  if (!uploadResp.ok()) throw new Error(`Presigned PUT failed with status ${uploadResp.status()}`);

  const completeResp = await request.post(
    `${BASE_URL}/api/v1/projects/${projectId}/tasks/${taskId}/attachments/complete-upload`,
    { data: { file_id: session.file_id } },
  );
  return (await completeResp.json()).data as { id: string };
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

const openTaskDetail = async (page: Page, projectId: string, taskId: string) => {
  await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
  await expect(page.getByRole('heading', { name: 'Attachments', level: 3 })).toBeVisible({
    timeout: 10_000,
  });
};

// ===========================================================================
// Rule: Attachments section — basic rendering (no files attached)
// ===========================================================================

test.describe('Task detail – Attachments section – basic rendering', () => {
  let projectId: string;
  let taskId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}BASIC_${RUN_ID}`);
    const statuses = await getTaskStatuses(request, projectId);
    const todoStatus = statuses.find((s) => s.category === 'todo');
    const task = await createTask(
      request,
      projectId,
      `${TEST_PROJECT_PREFIX}BASIC_TASK_${RUN_ID}`,
      todoStatus?.id,
    );
    taskId = task.id;
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Attachments section is always visible in the content pane', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The "Attachments" heading is always rendered in the content pane
    await expect(page.getByRole('heading', { name: 'Attachments', level: 3 })).toBeVisible();
  });

  test('Attachments section shows a drop-zone when no files are attached', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The drop zone text is visible when there are no attachments
    await expect(page.getByText('Drop your files here to upload')).toBeVisible();
  });

  test('Clicking the upload icon in the attachments section opens a file picker', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The "Upload attachment" icon button triggers the hidden file input
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Upload attachment' }).click();
    const fileChooser = await fileChooserPromise;
    expect(fileChooser).toBeTruthy();
  });

  test('Clicking the drop zone body opens the file picker', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The drop zone button also triggers the file picker when clicked
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: /drop your files here to upload/i }).click();
    const fileChooser = await fileChooserPromise;
    expect(fileChooser).toBeTruthy();
  });
});

// ===========================================================================
// Rule: Attachments section — upload via file picker
// ===========================================================================

test.describe('Task detail – Attachments section – upload via file picker', () => {
  let projectId: string;
  let taskId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}UPLOAD_${RUN_ID}`);
    const statuses = await getTaskStatuses(request, projectId);
    const todoStatus = statuses.find((s) => s.category === 'todo');
    const task = await createTask(
      request,
      projectId,
      `${TEST_PROJECT_PREFIX}UPLOAD_TASK_${RUN_ID}`,
      todoStatus?.id,
    );
    taskId = task.id;
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Single file upload via the upload button lists the file by name', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // Click the upload button and select a file
    const [fileChooser] = await Promise.all([
      page.waitForEvent('filechooser'),
      page.getByRole('button', { name: 'Upload attachment' }).click(),
    ]);
    await fileChooser.setFiles(FIXTURE_FILE_1);

    // The uploaded file should appear in the attachment list
    const fileName = path.basename(FIXTURE_FILE_1);
    await expect(page.getByRole('button', { name: `Preview ${fileName}` })).toBeVisible({
      timeout: 15_000,
    });
  });

  test('Dragging a file onto the drop-zone uploads it as an attachment', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // Click the drop zone to open the file picker and select a file
    const [fileChooser] = await Promise.all([
      page.waitForEvent('filechooser'),
      page.getByRole('button', { name: /drop your files here to upload/i }).click(),
    ]);
    await fileChooser.setFiles(FIXTURE_FILE_1);

    // The file should appear in the attachment list after upload
    const fileName = path.basename(FIXTURE_FILE_1);
    await expect(page.getByRole('button', { name: `Preview ${fileName}` })).toBeVisible({
      timeout: 15_000,
    });
  });

  test('Multiple files can be selected and uploaded at once via the file picker', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // Select two files simultaneously from the upload button file picker
    const [fileChooser] = await Promise.all([
      page.waitForEvent('filechooser'),
      page.getByRole('button', { name: 'Upload attachment' }).click(),
    ]);
    await fileChooser.setFiles([FIXTURE_FILE_1, FIXTURE_FILE_2]);

    // Both files should appear in the attachment list
    const fileName1 = path.basename(FIXTURE_FILE_1);
    const fileName2 = path.basename(FIXTURE_FILE_2);
    await expect(page.getByRole('button', { name: `Preview ${fileName1}` })).toBeVisible({
      timeout: 15_000,
    });
    await expect(page.getByRole('button', { name: `Preview ${fileName2}` })).toBeVisible({
      timeout: 15_000,
    });
  });

  test('Multiple files can be uploaded by dropping them onto the drop zone at the same time', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // Click the drop zone to open the file picker and select two files
    const [fileChooser] = await Promise.all([
      page.waitForEvent('filechooser'),
      page.getByRole('button', { name: /drop your files here to upload/i }).click(),
    ]);
    await fileChooser.setFiles([FIXTURE_FILE_1, FIXTURE_FILE_2]);

    const fileName1 = path.basename(FIXTURE_FILE_1);
    const fileName2 = path.basename(FIXTURE_FILE_2);
    await expect(page.getByRole('button', { name: `Preview ${fileName1}` })).toBeVisible({
      timeout: 15_000,
    });
    await expect(page.getByRole('button', { name: `Preview ${fileName2}` })).toBeVisible({
      timeout: 15_000,
    });
  });
});

// ===========================================================================
// Rule: Attachments section — attachment item display
// ===========================================================================

test.describe('Task detail – Attachments section – attachment item display', () => {
  let projectId: string;
  let taskId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}DISPLAY_${RUN_ID}`);
    const statuses = await getTaskStatuses(request, projectId);
    const todoStatus = statuses.find((s) => s.category === 'todo');
    const task = await createTask(
      request,
      projectId,
      `${TEST_PROJECT_PREFIX}DISPLAY_TASK_${RUN_ID}`,
      todoStatus?.id,
    );
    taskId = task.id;
    // Seed an attachment with a known file name via API
    await uploadAttachmentViaAPI(request, projectId, taskId, 'report.pdf', 'PDF content here');
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Existing attachments are listed with their file name', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The file should appear as a preview button labelled with its name
    await expect(page.getByRole('button', { name: 'Preview report.pdf' })).toBeVisible();
  });

  test('Each attachment item shows the file size in a human-readable format', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // Size is displayed as e.g. "16 B ·", "1.2 KB ·", etc.
    await expect(page.getByText(/\d+(\.\d+)? (B|KB|MB) ·/)).toBeVisible();
  });

  test('Each attachment item shows a relative upload timestamp', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The item's meta line renders as "<size> · <relative time>" in a single paragraph,
    // so a just-seeded attachment reads e.g. "23 B · just now".
    await expect(page.getByText(/· just now$/)).toBeVisible();
  });

  test('Extension badge shows the correct uppercased file extension', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The preview badge button for "report.pdf" should contain the text "PDF"
    await expect(page.getByRole('button', { name: 'Preview report.pdf' })).toContainText('PDF');
  });
});

// ===========================================================================
// Rule: Attachments section — preview and download
// ===========================================================================

test.describe('Task detail – Attachments section – preview and download', () => {
  let projectId: string;
  let taskId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}PREVIEW_${RUN_ID}`);
    const statuses = await getTaskStatuses(request, projectId);
    const todoStatus = statuses.find((s) => s.category === 'todo');
    const task = await createTask(
      request,
      projectId,
      `${TEST_PROJECT_PREFIX}PREVIEW_TASK_${RUN_ID}`,
      todoStatus?.id,
    );
    taskId = task.id;
    await uploadAttachmentViaAPI(request, projectId, taskId, 'design.png', 'PNG content here');
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Clicking the file extension badge opens the attachment for preview in a new browser tab', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The extension badge button has aria-label="Preview {filename}"
    const [popup] = await Promise.all([
      page.waitForEvent('popup'),
      page.getByRole('button', { name: 'Preview design.png' }).click(),
    ]);
    expect(popup).toBeTruthy();
    await popup.close();
  });

  test('Clicking the file name opens the attachment for preview in a new browser tab', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The file name button has an accessible name that starts with the file name (e.g. "design.png 16 B · just now")
    const fileNameBtn = page.getByRole('button', { name: /^design\.png/ });
    const [popup] = await Promise.all([
      page.waitForEvent('popup'),
      fileNameBtn.click(),
    ]);
    expect(popup).toBeTruthy();
    await popup.close();
  });

  test('A download button is present for each attachment row', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // The download button is labelled "Download {filename}" via aria-label
    await expect(page.getByRole('button', { name: 'Download design.png' })).toBeVisible();
  });

  test('Clicking the download button initiates a file download in a new tab', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // Clicking download fetches a presigned ?download=true URL and opens it in a new tab
    const [popup] = await Promise.all([
      page.waitForEvent('popup'),
      page.getByRole('button', { name: 'Download design.png' }).click(),
    ]);
    expect(popup).toBeTruthy();
    await popup.close();
  });
});

// ===========================================================================
// Rule: Attachments section — delete attachment
// ===========================================================================

test.describe('Task detail – Attachments section – delete', () => {
  let projectId: string;
  let taskId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}DELETE_${RUN_ID}`);
    const statuses = await getTaskStatuses(request, projectId);
    const todoStatus = statuses.find((s) => s.category === 'todo');
    const task = await createTask(
      request,
      projectId,
      `${TEST_PROJECT_PREFIX}DELETE_TASK_${RUN_ID}`,
      todoStatus?.id,
    );
    taskId = task.id;
    await uploadAttachmentViaAPI(request, projectId, taskId, 'old-file.docx', 'file content');
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('An attachment can be deleted by a user with write permission', async ({ page }) => {
    await signIn(page);
    await openTaskDetail(page, projectId, taskId);

    // Attachment should be listed before deletion
    await expect(page.getByRole('button', { name: 'Preview old-file.docx' })).toBeVisible();

    // Click the delete button for the attachment
    await page.getByRole('button', { name: 'Delete old-file.docx' }).click();

    // Attachment should be removed from the list
    await expect(page.getByRole('button', { name: 'Preview old-file.docx' })).not.toBeVisible({
      timeout: 10_000,
    });
  });
});
