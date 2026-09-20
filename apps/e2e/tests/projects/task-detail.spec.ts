// spec: features/projects/task-detail.feature
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';
const TEST_PROJECT_PREFIX = 'E2E_TD_';
const RUN_ID = Date.now().toString(36).slice(-5).toUpperCase();

// ─── Types ────────────────────────────────────────────────────────────────────

interface TaskStatus {
  id: string;
  name: string;
  category: string;
  position: number;
}

interface Task {
  id: string;
  title: string;
  status_id: string | null;
  sprint_id: string | null;
}

interface InteractionView {
  id: string;
  name: string;
  view_type: string;
}

interface TaskType {
  id: string;
  name: string;
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
  const resp = await request.post(`${BASE_URL}/api/v1/projects`, {
    data: { name },
  });
  const body = await resp.json();
  return body.data.id as string;
}

async function getTaskStatuses(request: APIRequestContext, projectId: string): Promise<TaskStatus[]> {
  const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/task-statuses`);
  const body = await resp.json();
  return (body?.data?.items ?? []) as TaskStatus[];
}

async function createTask(
  request: APIRequestContext,
  projectId: string,
  payload: { title: string; status_id?: string; sprint_id?: string; task_type_id?: string },
): Promise<Task> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/tasks`, {
    data: payload,
  });
  const body = await resp.json();
  return body.data as Task;
}

async function createBacklogView(
  request: APIRequestContext,
  projectId: string,
  name: string,
  view_type: 'board' | 'table',
): Promise<InteractionView> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/views?context=backlog`, {
    data: { name, view_type },
  });
  const body = await resp.json();
  return body.data as InteractionView;
}

async function listBacklogViews(request: APIRequestContext, projectId: string): Promise<InteractionView[]> {
  const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/views?context=backlog`);
  const body = await resp.json();
  return (body?.data?.items ?? []) as InteractionView[];
}

async function deleteBacklogView(request: APIRequestContext, projectId: string, viewId: string): Promise<void> {
  await request.delete(`${BASE_URL}/api/v1/projects/${projectId}/views/${viewId}?context=backlog`);
}

async function getTaskTypes(request: APIRequestContext, projectId: string): Promise<TaskType[]> {
  const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/task-types`);
  const body = await resp.json();
  return (body?.data?.items ?? []) as TaskType[];
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

const navigateToBacklog = async (page: Page, projectId: string) => {
  await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
  await expect(page.getByRole('heading', { name: 'Product Backlog' })).toBeVisible({ timeout: 10_000 });
};

const openBoardView = async (page: Page) => {
  await page.getByRole('button', { name: 'Board', exact: true }).click();
};

const openTableView = async (page: Page) => {
  await page.getByRole('button', { name: 'Table', exact: true }).click();
};

// ─── Test Suites ──────────────────────────────────────────────────────────────

// ===========================================================================
// Rule: Opening — modal from board view
// ===========================================================================

test.describe('Opening task detail from board view', () => {
  let projectId: string;
  let task: Task;
  let statuses: TaskStatus[];

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}BOARD_${RUN_ID}`);
    statuses = await getTaskStatuses(request, projectId);
    const taskTypes = await getTaskTypes(request, projectId);
    const nonEpicType = taskTypes.find((t) => t.name !== 'Epic');
    // Delete the auto-created default views so we control exactly which views exist
    const existingViews = await listBacklogViews(request, projectId);
    // Smart-delete: create new views before deleting old ones to avoid server auto-recreation
    await createBacklogView(request, projectId, 'Board', 'board');
    await createBacklogView(request, projectId, 'Table', 'table');
    for (const v of existingViews) {
      await deleteBacklogView(request, projectId, v.id);
    }
    const todoStatus = statuses.find((s) => s.category === 'todo');
    task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}MODAL_TASK_${RUN_ID}`,
      status_id: todoStatus?.id,
      task_type_id: nonEpicType?.id,
    });
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Clicking a task card in Board view opens the task detail modal', async ({ page }) => {
    await signIn(page);
    await navigateToBacklog(page, projectId);
    await openBoardView(page);

    const card = page.locator('[data-task-id]').filter({ hasText: task.title });
    await expect(card).toBeVisible({ timeout: 10_000 });
    await card.click();

    // The task detail dialog should be open and accessible by its title
    await expect(page.getByRole('dialog', { name: task.title })).toBeVisible({ timeout: 10_000 });
  });

  test('Clicking a task row in Table view opens the task detail modal', async ({ page }) => {
    await signIn(page);
    await navigateToBacklog(page, projectId);
    await openTableView(page);

    await expect(page.getByText(task.title)).toBeVisible({ timeout: 10_000 });
    await page.getByText(task.title).click();

    // The task detail dialog should be open and accessible by its title
    await expect(page.getByRole('dialog', { name: task.title })).toBeVisible({ timeout: 10_000 });
  });

  test('URL includes the task identifier when the modal opens', async ({ page }) => {
    await signIn(page);
    await navigateToBacklog(page, projectId);
    await openBoardView(page);

    const card = page.locator('[data-task-id]').filter({ hasText: task.title });
    await expect(card).toBeVisible({ timeout: 10_000 });
    await card.click();

    // URL should contain the task id
    await expect(page).toHaveURL(new RegExp(task.id), { timeout: 10_000 });
  });

  test('Board view remains visible in the background after modal opens', async ({ page }) => {
    await signIn(page);
    await navigateToBacklog(page, projectId);
    await openBoardView(page);

    const card = page.locator('[data-task-id]').filter({ hasText: task.title });
    await expect(card).toBeVisible({ timeout: 10_000 });
    await card.click();

    // The task detail dialog should be open and accessible by its title
    await expect(page.getByRole('dialog', { name: task.title })).toBeVisible({ timeout: 10_000 });

    // Board columns should still be visible in the background
    const todoStatus = statuses.find((s) => s.category === 'todo');
    if (todoStatus) {
      await expect(page.getByText(todoStatus.name, { exact: true }).first()).toBeVisible();
    }
  });
});

// ===========================================================================
// Rule: Opening — task detail page from a direct URL
// ===========================================================================

test.describe('Task detail page via direct URL', () => {
  let projectId: string;
  let task: Task;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}PAGE_${RUN_ID}`);
    const statuses = await getTaskStatuses(request, projectId);
    const todoStatus = statuses.find((s) => s.category === 'todo');
    task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}PAGE_TASK_${RUN_ID}`,
      status_id: todoStatus?.id,
    });
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Navigating directly to a task URL renders the task title', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${task.id}`);

    // Use filter({ visible: true }) because on mobile the breadcrumb span is CSS-hidden;
    // the title should still be visible in the main content heading.
    await expect(page.getByText(task.title).filter({ visible: true }).first()).toBeVisible({ timeout: 10_000 });
  });

  test('Task detail page shows project breadcrumb context', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${task.id}`);

    // A breadcrumb with a back link should be visible
    await expect(page.locator('[class*="breadcrumb"], a[href*="projects"]').first()).toBeVisible({ timeout: 10_000 });
  });

  test('Navigating to a non-existent task URL shows a not-found state', async ({ page }) => {
    await signIn(page);
    const fakeTaskId = '00000000-0000-0000-0000-000000000000';
    await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${fakeTaskId}`);

    await expect(page.getByText(/task not found/i)).toBeVisible({ timeout: 10_000 });
  });
});

// ===========================================================================
// Rule: Two-pane layout
// ===========================================================================

test.describe('Task detail two-pane layout', () => {
  let projectId: string;
  let task: Task;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}LAYOUT_${RUN_ID}`);
    const statuses = await getTaskStatuses(request, projectId);
    await createBacklogView(request, projectId, 'Board', 'board');
    const todoStatus = statuses.find((s) => s.category === 'todo');
    task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}LAYOUT_TASK_${RUN_ID}`,
      status_id: todoStatus?.id,
    });
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Task detail shows a description/content area', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${task.id}`);

    // Use filter({ visible: true }) because on mobile the breadcrumb span is CSS-hidden
    await expect(page.getByText(task.title).filter({ visible: true }).first()).toBeVisible({ timeout: 10_000 });
    // A description or add-description affordance should exist
    await expect(
      page.getByText(/description|add a description/i).first(),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('Task detail shows a comment input', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${task.id}`);

    // Use filter({ visible: true }) because on mobile the breadcrumb span is CSS-hidden
    await expect(page.getByText(task.title).filter({ visible: true }).first()).toBeVisible({ timeout: 10_000 });
    // The comment composer is a BlockNote editor in a <fieldset>; its contenteditable
    // has no accessible name or placeholder text, so it is located by structure.
    await expect(page.locator('fieldset [contenteditable="true"]').first()).toBeVisible({ timeout: 10_000 });
  });

  test('Task detail properties section shows a Status field', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${task.id}`);

    // Use filter({ visible: true }) because on mobile the breadcrumb span is CSS-hidden
    await expect(page.getByText(task.title).filter({ visible: true }).first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Status', { exact: true }).first()).toBeVisible({ timeout: 10_000 });
  });

  test('Task detail header shows task title prominently', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${task.id}`);

    // Use filter({ visible: true }) because on mobile the breadcrumb span is CSS-hidden
    await expect(page.getByText(task.title).filter({ visible: true }).first()).toBeVisible({ timeout: 10_000 });
  });
});

// ===========================================================================
// Rule: Sprint task detail preserves sprint association (regression guard)
// ===========================================================================

test.describe('Sprint task detail — sprint_id preservation', () => {
  let projectId: string;
  let sprintId: string;
  let task: Task;
  let statuses: TaskStatus[];

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}SPRINT_${RUN_ID}`);
    statuses = await getTaskStatuses(request, projectId);

    // Create an active sprint
    const sprintResp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/sprints`, {
      data: { name: `${TEST_PROJECT_PREFIX}SPRINT_${RUN_ID}`, status: 'active' },
    });
    sprintId = (await sprintResp.json()).data.id as string;

    const todoStatus = statuses.find((s) => s.category === 'todo');
    task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}SPRINT_TASK_${RUN_ID}`,
      status_id: todoStatus?.id,
      sprint_id: sprintId,
    });
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Task detail page opens for a sprint task and shows its title', async ({ page }) => {
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${task.id}`);

    // Use filter({ visible: true }) because on mobile the breadcrumb span is CSS-hidden
    await expect(page.getByText(task.title).filter({ visible: true }).first()).toBeVisible({ timeout: 10_000 });
  });

  test('Patching only the status from the task detail does not move task to backlog', async ({ request }) => {
    // This is the regression test for the PATCH partial-update bug.
    // PATCH with only status_id must not clear sprint_id.
    const newStatus = statuses.find((s) => s.category === 'inprogress') ?? statuses[1];

    const patchResp = await request.patch(
      `${BASE_URL}/api/v1/projects/${projectId}/tasks/${task.id}`,
      { data: { status_id: newStatus.id } },
    );
    expect(patchResp.ok()).toBeTruthy();

    // Re-fetch the task and verify sprint_id is still set
    const getResp = await request.get(
      `${BASE_URL}/api/v1/projects/${projectId}/tasks/${task.id}`,
    );
    expect(getResp.ok()).toBeTruthy();
    const updated = (await getResp.json()).data as Task;

    expect(updated.sprint_id).toBe(sprintId);
    expect(updated.status_id).toBe(newStatus.id);
  });
});
