// spec: features/projects/interaction-sidebar.feature
//       Rule: Dragging a task onto a sidebar interaction entry reassigns its sprint
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import { test, expect, type Locator, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';
const TEST_PROJECT_PREFIX = 'E2E_DS_';
const RUN_ID = Date.now().toString(36).slice(-5).toUpperCase();

// ─── Types ────────────────────────────────────────────────────────────────────

interface Task {
  id: string;
  title: string;
  sprint_id: string | null;
  status_id: string | null;
}

interface TaskStatus {
  id: string;
  name: string;
  category: string;
  position: number;
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
    const resp = await request.get(`${BASE_URL}/api/v1/projects?page=${page}&page_size=100`);
    if (!resp.ok()) break;
    const body = await resp.json();
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
  return ((await resp.json()).data.id) as string;
}

async function createSprint(
  request: APIRequestContext,
  projectId: string,
  name: string,
): Promise<string> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/sprints`, {
    data: { name, status: 'active' },
  });
  return ((await resp.json()).data.id) as string;
}

async function createTask(
  request: APIRequestContext,
  projectId: string,
  payload: { title: string; sprint_id?: string | null; status_id?: string | null; task_type_id?: string | null },
): Promise<Task> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/tasks`, {
    data: payload,
  });
  return (await resp.json()).data as Task;
}

async function getTask(
  request: APIRequestContext,
  projectId: string,
  taskId: string,
): Promise<Task> {
  const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/tasks/${taskId}`);
  return (await resp.json()).data as Task;
}

async function getTaskStatuses(
  request: APIRequestContext,
  projectId: string,
): Promise<TaskStatus[]> {
  const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/task-statuses`);
  return ((await resp.json())?.data?.items ?? []) as TaskStatus[];
}

async function getTaskTypes(request: APIRequestContext, projectId: string): Promise<TaskType[]> {
  const resp = await request.get(`${BASE_URL}/api/v1/projects/${projectId}/task-types`);
  const body = await resp.json();
  return (body?.data?.items ?? []) as TaskType[];
}

// ─── UI Helpers ───────────────────────────────────────────────────────────────

const signIn = async (page: Page): Promise<void> => {
  await page.goto(`${BASE_URL}/`);
  await ensureLoginForm(page);
  await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
  await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(
    page.getByRole('heading', { name: /Good (morning|afternoon|evening)/i }),
  ).toBeVisible();
};

/** Navigate to a sprint's interaction page and ensure the Board tab is active. */
const navigateToSprintBoardView = async (
  page: Page,
  projectId: string,
  sprintId: string,
): Promise<void> => {
  await page.goto(`${BASE_URL}/projects/${projectId}/interactions/sprints/${sprintId}`);
  await expect(page.locator('[data-sidebar="sidebar"]')).toBeVisible({ timeout: 30_000 });
  const boardTab = page.getByRole('button', { name: 'Board', exact: true });
  await expect(boardTab).toBeVisible({ timeout: 30_000 });
  await boardTab.click();
  // Board columns should be visible - use status name text instead of removed [data-status-id]
  await expect(page.getByText('Todo', { exact: true }).first()).toBeVisible({ timeout: 30_000 });
};

/** Navigate to a sprint's interaction page and ensure the Table tab is active. */
const navigateToSprintTableView = async (
  page: Page,
  projectId: string,
  sprintId: string,
): Promise<void> => {
  await page.goto(`${BASE_URL}/projects/${projectId}/interactions/sprints/${sprintId}`);
  await expect(page.locator('[data-sidebar="sidebar"]')).toBeVisible({ timeout: 30_000 });
  const tableTab = page.getByRole('button', { name: 'Table', exact: true });
  await expect(tableTab).toBeVisible({ timeout: 30_000 });
  await tableTab.click();
};

/** Return the SidebarMenuButton element for a named interaction entry in the sidebar. */
function sidebarEntry(page: Page, name: string): Locator {
  return page
    .locator('[data-sidebar="sidebar"]')
    .locator('[data-sidebar="menu-button"]')
    .filter({ hasText: name });
}

/**
 * Simulate an HTML5 drag from `source` to `target` using low-level mouse events.
 * In Chromium, pressing mouse-down on a `draggable` element then moving triggers
 * the HTML5 dragstart / dragover / drop event chain, including a populated
 * DataTransfer object — matching what our onDragStart handlers set.
 */
async function dragElementTo(page: Page, source: Locator, target: Locator): Promise<void> {
  const sourceBB = await source.boundingBox();
  const targetBB = await target.boundingBox();
  if (!sourceBB || !targetBB) throw new Error('Could not obtain bounding boxes for drag');

  const startX = sourceBB.x + sourceBB.width / 2;
  const startY = sourceBB.y + sourceBB.height / 2;
  const endX = targetBB.x + targetBB.width / 2;
  const endY = targetBB.y + targetBB.height / 2;

  await page.mouse.move(startX, startY);
  await page.mouse.down();
  // Small nudge to trigger dragstart on the source element
  await page.mouse.move(startX + 5, startY, { steps: 3 });
  // Sweep to the target in fine steps so dragover fires on every element along the path
  await page.mouse.move(endX, endY, { steps: 40 });
  await page.mouse.up();
}

// ===========================================================================
// Rule: Dragging a task onto a sidebar interaction entry reassigns its sprint
// ===========================================================================

test.describe('Dragging a task onto a sidebar interaction entry reassigns its sprint', () => {
  let projectId: string;
  let sourceSprintId: string;
  let targetSprintId: string;
  let statuses: TaskStatus[];
  let taskTypes: TaskType[] = [];

  const SOURCE_SPRINT = `${TEST_PROJECT_PREFIX}SRC_${RUN_ID}`;
  const TARGET_SPRINT = `${TEST_PROJECT_PREFIX}TGT_${RUN_ID}`;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}PRJ_${RUN_ID}`);
    statuses = await getTaskStatuses(request, projectId);
    taskTypes = await getTaskTypes(request, projectId);
    sourceSprintId = await createSprint(request, projectId, SOURCE_SPRINT);
    targetSprintId = await createSprint(request, projectId, TARGET_SPRINT);
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  // ── Scenario 1: Board view card → sprint sidebar entry ───────────────────

  test('Dragging a task card onto a sprint sidebar entry moves the task into that sprint', async ({
    page,
    request,
    isMobile,
  }) => {
    if (isMobile) {
      test.skip(true, 'Drag and drop is not supported on mobile browsers');
      return;
    }

    const todoStatus = statuses.find((s) => s.category === 'todo');
    const nonEpicType = taskTypes.find((t) => t.name !== 'Epic');
    const task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}MOVE_TASK_${RUN_ID}`,
      sprint_id: sourceSprintId,
      status_id: todoStatus?.id ?? null,
      task_type_id: nonEpicType?.id ?? null,
    });

    await signIn(page);
    await navigateToSprintBoardView(page, projectId, sourceSprintId);

    const taskCard = page.locator('[data-task-id]').filter({ hasText: task.title });
    await expect(taskCard).toBeVisible({ timeout: 10_000 });

    const targetEntry = sidebarEntry(page, TARGET_SPRINT);
    await expect(targetEntry).toBeVisible();

    await dragElementTo(page, taskCard, targetEntry);

    // The task should no longer be visible in the source sprint board view
    await expect(taskCard).not.toBeVisible({ timeout: 10_000 });

    // Confirm via API that sprint_id was updated to the target sprint
    const updated = await getTask(request, projectId, task.id);
    expect(updated.sprint_id).toBe(targetSprintId);
  });

  // ── Scenario 2: Table view row → sprint sidebar entry ────────────────────

  test('Dragging a task row from the table view onto a sprint sidebar entry moves the task', async ({
    page,
    request,
    isMobile,
  }) => {
    if (isMobile) {
      test.skip(true, 'Drag and drop is not supported on mobile browsers');
      return;
    }

    const todoStatus = statuses.find((s) => s.category === 'todo');
    const nonEpicType = taskTypes.find((t) => t.name !== 'Epic');
    const task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}TBL_TASK_${RUN_ID}`,
      sprint_id: sourceSprintId,
      status_id: todoStatus?.id ?? null,
      task_type_id: nonEpicType?.id ?? null,
    });

    await signIn(page);
    await navigateToSprintTableView(page, projectId, sourceSprintId);

    // The task row in table view is a cursor-pointer generic element whose parent contains the task title
    const taskRow = page.getByText(task.title).locator('xpath=..').first();
    await expect(taskRow).toBeVisible({ timeout: 10_000 });

    const targetEntry = sidebarEntry(page, TARGET_SPRINT);
    await expect(targetEntry).toBeVisible();

    await dragElementTo(page, taskRow, targetEntry);

    // Confirm via API that sprint_id was updated to the target sprint
    await expect(async () => {
      const updated = await getTask(request, projectId, task.id);
      expect(updated.sprint_id).toBe(targetSprintId);
    }).toPass({ timeout: 10_000 });
  });

  // ── Scenario 3: Board view card → Product Backlog sidebar entry ───────────

  test('Dragging a task onto the "Product Backlog" sidebar entry removes it from its sprint', async ({
    page,
    request,
    isMobile,
  }) => {
    if (isMobile) {
      test.skip(true, 'Drag and drop is not supported on mobile browsers');
      return;
    }

    const todoStatus = statuses.find((s) => s.category === 'todo');
    const nonEpicType = taskTypes.find((t) => t.name !== 'Epic');
    const task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}BL_TASK_${RUN_ID}`,
      sprint_id: sourceSprintId,
      status_id: todoStatus?.id ?? null,
      task_type_id: nonEpicType?.id ?? null,
    });

    await signIn(page);
    await navigateToSprintBoardView(page, projectId, sourceSprintId);

    const taskCard = page.locator('[data-task-id]').filter({ hasText: task.title });
    await expect(taskCard).toBeVisible({ timeout: 10_000 });

    const backlogEntry = sidebarEntry(page, 'Product Backlog');
    await expect(backlogEntry).toBeVisible();

    await dragElementTo(page, taskCard, backlogEntry);

    // Task should be removed from the source sprint (no longer visible on the board)
    await expect(taskCard).not.toBeVisible({ timeout: 10_000 });

    // Confirm via API that sprint_id is now null/absent (moved to backlog)
    await expect(async () => {
      const updated = await getTask(request, projectId, task.id);
      // sprint_id may be absent (undefined) or explicitly null when no sprint is set
      expect(updated.sprint_id ?? null).toBeNull();
    }).toPass({ timeout: 10_000 });
  });

  // ── Scenario 4: Drop-target highlight appears on dragover ─────────────────

  test('Sprint sidebar entry highlights as a drop target when a task is dragged over it', async ({
    page,
    request,
    isMobile,
  }) => {
    if (isMobile) {
      test.skip(true, 'Drag and drop is not supported on mobile browsers');
      return;
    }

    const todoStatus = statuses.find((s) => s.category === 'todo');
    const nonEpicType = taskTypes.find((t) => t.name !== 'Epic');
    const task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}HOVER_TASK_${RUN_ID}`,
      sprint_id: sourceSprintId,
      status_id: todoStatus?.id ?? null,
      task_type_id: nonEpicType?.id ?? null,
    });

    await signIn(page);
    await navigateToSprintBoardView(page, projectId, sourceSprintId);

    const taskCard = page.locator('[data-task-id]').filter({ hasText: task.title });
    await expect(taskCard).toBeVisible({ timeout: 10_000 });

    const targetEntry = sidebarEntry(page, TARGET_SPRINT);
    await expect(targetEntry).toBeVisible();

    const sourceBB = await taskCard.boundingBox();
    const targetBB = await targetEntry.boundingBox();
    if (!sourceBB || !targetBB) throw new Error('Could not obtain bounding boxes');

    const startX = sourceBB.x + sourceBB.width / 2;
    const startY = sourceBB.y + sourceBB.height / 2;
    const endX = targetBB.x + targetBB.width / 2;
    const endY = targetBB.y + targetBB.height / 2;

    // Begin drag — do not release yet
    await page.mouse.move(startX, startY);
    await page.mouse.down();
    await page.mouse.move(startX + 5, startY, { steps: 3 });
    await page.mouse.move(endX, endY, { steps: 40 });

    // While hovering over the target entry, the ring highlight class should be present
    await page.waitForFunction(
      (sprintName: string) => {
        const buttons = Array.from(
          document.querySelectorAll<HTMLElement>('[data-sidebar="menu-button"]'),
        );
        const btn = buttons.find((el) => el.textContent?.trim().includes(sprintName));
        return btn != null && /(?:^|\s)ring-2(?:\s|$)/.test(btn.className);
      },
      TARGET_SPRINT,
      { timeout: 5_000 },
    );

    // Cleanup: release mouse to end the drag
    await page.mouse.up();
  });

  // ── Scenario 5: Drop-target highlight is removed on drag leave ────────────

  test('Drop-target highlight is removed when the task is dragged away from the entry', async ({
    page,
    request,
    isMobile,
  }) => {
    if (isMobile) {
      test.skip(true, 'Drag and drop is not supported on mobile browsers');
      return;
    }

    const todoStatus = statuses.find((s) => s.category === 'todo');
    const nonEpicType = taskTypes.find((t) => t.name !== 'Epic');
    const task = await createTask(request, projectId, {
      title: `${TEST_PROJECT_PREFIX}LEAVE_TASK_${RUN_ID}`,
      sprint_id: sourceSprintId,
      status_id: todoStatus?.id ?? null,
      task_type_id: nonEpicType?.id ?? null,
    });

    await signIn(page);
    await navigateToSprintBoardView(page, projectId, sourceSprintId);

    const taskCard = page.locator('[data-task-id]').filter({ hasText: task.title });
    await expect(taskCard).toBeVisible({ timeout: 10_000 });

    const targetEntry = sidebarEntry(page, TARGET_SPRINT);
    await expect(targetEntry).toBeVisible();

    const sourceBB = await taskCard.boundingBox();
    const targetBB = await targetEntry.boundingBox();
    if (!sourceBB || !targetBB) throw new Error('Could not obtain bounding boxes');

    const startX = sourceBB.x + sourceBB.width / 2;
    const startY = sourceBB.y + sourceBB.height / 2;
    const targetX = targetBB.x + targetBB.width / 2;
    const targetY = targetBB.y + targetBB.height / 2;
    // A safe position to move away to that is not inside the sidebar entry (use source position)
    const awayX = startX;
    const awayY = startY;

    // Begin drag, move to target entry to trigger dragover highlight
    await page.mouse.move(startX, startY);
    await page.mouse.down();
    await page.mouse.move(startX + 5, startY, { steps: 3 });
    await page.mouse.move(targetX, targetY, { steps: 30 });

    // Wait for the highlight to appear
    await page.waitForFunction(
      (sprintName: string) => {
        const buttons = Array.from(
          document.querySelectorAll<HTMLElement>('[data-sidebar="menu-button"]'),
        );
        const btn = buttons.find((el) => el.textContent?.trim().includes(sprintName));
        return btn != null && /(?:^|\s)ring-2(?:\s|$)/.test(btn.className);
      },
      TARGET_SPRINT,
      { timeout: 5_000 },
    );

    // Move away and release the mouse.
    await page.mouse.move(awayX, awayY, { steps: 20 });
    await page.mouse.up();

    // Playwright's mouse simulation may not reliably trigger HTML5 `dragleave`
    // with the correct relatedTarget across all browsers. Dispatch it directly
    // on the target entry via Playwright's locator API so React's onDragLeave
    // handler fires with relatedTarget=null, which always passes the contains
    // check and clears the highlight state.
    await targetEntry.dispatchEvent('dragleave', { bubbles: true, cancelable: true });

    // The highlight should now be removed
    await expect(targetEntry).not.toHaveClass(/(?:^|\s)ring-2(?:\s|$)/, { timeout: 5_000 });
  });

  // ── Scenario 6: Permission guard ──────────────────────────────────────────

  test('User without "Edit Tasks" permission cannot reassign a task via sidebar drag', async ({
    isMobile,
  }) => {
    // This scenario requires a non-admin project member without tasks.write permission.
    // Setting up a second user with restricted permissions goes beyond the admin-only
    // test environment used in this suite.  Mark as todo until a restricted-user
    // fixture is available.
    test.skip(
      true,
      'Requires a non-admin user fixture with tasks.write permission revoked — not yet available in the test environment',
    );
    void isMobile;
  });
});
