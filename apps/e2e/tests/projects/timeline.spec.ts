// spec: features/projects/timeline.feature
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';
const TEST_PROJECT_PREFIX = 'E2E_TL_';
const RUN_ID = Date.now().toString(36).slice(-5).toUpperCase();

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

async function createSprint(
  request: APIRequestContext,
  projectId: string,
  name: string,
): Promise<string> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/sprints`, {
    data: { name, status: 'active' },
  });
  const body = await resp.json();
  return body.data.id as string;
}

const signIn = async (uiPage: Page) => {
  await uiPage.goto(`${BASE_URL}/`);
  await ensureLoginForm(uiPage);
  await uiPage.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
  await uiPage.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
  await uiPage.getByRole('button', { name: 'Sign in' }).click();
  await expect(
    uiPage.getByRole('heading', { name: /Good (morning|afternoon|evening)/i }),
  ).toBeVisible();
};

const navigateToTimeline = async (uiPage: Page, projectId: string) => {
  await uiPage.goto(`${BASE_URL}/projects/${projectId}/interactions/timeline`);
  await expect(uiPage.getByRole('heading', { name: 'Timeline' })).toBeVisible({ timeout: 10_000 });
};

test.describe('Timeline interaction page header and navigation', () => {
  let projectId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}NAV_${RUN_ID}`);
    await createSprint(request, projectId, `${TEST_PROJECT_PREFIX}SPRINT_${RUN_ID}`);
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Timeline appears in the sidebar above Product Backlog', async ({ page, isMobile }) => {
    if (isMobile) {
      test.skip(true, 'Sidebar is a sheet overlay on mobile — link order cannot be inspected without opening the sheet');
      return;
    }
    await signIn(page);

    // Navigate to the project to expand the sidebar interactions section
    await navigateToTimeline(page, projectId);

    // Confirm sidebar link order: Timeline index < Product Backlog index
    const sidebarLinks = page.locator('[data-sidebar="sidebar"] li a');
    const allTexts = await sidebarLinks.allTextContents();
    const timelineIdx = allTexts.findIndex((t) => t.trim() === 'Timeline');
    const backlogIdx = allTexts.findIndex((t) => t.trim() === 'Product Backlog');
    expect(timelineIdx).toBeGreaterThanOrEqual(0);
    expect(backlogIdx).toBeGreaterThan(timelineIdx);
  });

  test('Clicking Timeline in the sidebar opens the Timeline interaction page', async ({ page, isMobile }) => {
    if (isMobile) {
      test.skip(true, 'Sidebar is a sheet overlay on mobile — Timeline link is not directly clickable without opening the sheet');
      return;
    }
    await signIn(page);
    await page.goto(`${BASE_URL}/projects/${projectId}`);

    // Click Timeline link in the sidebar
    await page.getByRole('link', { name: 'Timeline' }).click();

    // URL should contain /interactions/timeline
    await expect(page).toHaveURL(/\/interactions\/timeline/);
  });

  test('Timeline page heading is "Timeline"', async ({ page }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // Verify the main heading
    await expect(page.getByRole('heading', { name: 'Timeline' })).toBeVisible();
  });

  test('Timeline page subtitle reads "Epics and long-horizon planning on a roadmap."', async ({
    page,
  }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // Verify the subtitle text
    await expect(page.getByText('Epics and long-horizon planning on a roadmap.')).toBeVisible();
  });

  test('Timeline page does not show a "New sprint" button', async ({ page }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // New sprint button should not be present on the Timeline page
    await expect(page.getByRole('button', { name: 'New sprint' })).not.toBeVisible();
  });
});

test.describe('Timeline default Roadmap view', () => {
  let projectId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}VIEW_${RUN_ID}`);
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('Timeline opens with the Roadmap view tab active by default', async ({ page }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // Roadmap tab should be active (text-primary class)
    const roadmapTab = page.getByRole('button', { name: 'Roadmap', exact: true });
    await expect(roadmapTab).toBeVisible();
    await expect(roadmapTab).toHaveClass(/text-primary/);
  });

  test('Timeline empty state shows "No tasks to display" when no Epics exist', async ({ page }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // Empty state message should be visible
    await expect(page.getByText('No tasks to display')).toBeVisible();
  });

  test('Timeline Roadmap view shows month labels on the horizontal axis', async ({ page }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // At least one month + year label should be visible on the timeline axis
    const monthPattern =
      /\b(January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{4}/;
    await expect(page.getByText(monthPattern).first()).toBeVisible();
  });

  test('Timeline shows an "Add task" button', async ({ page }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // Add task button should be visible
    await expect(page.getByRole('button', { name: 'Add task' })).toBeVisible();
  });

  test('Timeline Roadmap view shows a "Task" column label in the left-side panel', async ({
    page,
  }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // The left panel header label "Task" should be visible
    await expect(page.getByText('Task', { exact: true }).first()).toBeVisible();
  });
});

test.describe('Timeline view settings defaults', () => {
  let projectId: string;

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    projectId = await createProject(request, `${TEST_PROJECT_PREFIX}SETTINGS_${RUN_ID}`);
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  test('View settings Task types: Epic is checked, Story/Bug/Task are unchecked', async ({
    page,
  }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // Open view settings
    await page.getByRole('button', { name: 'View settings' }).click();

    // Epic checkbox should be checked by default
    await expect(page.getByRole('checkbox', { name: 'Epic' })).toBeChecked();

    // Normal task type checkboxes should be unchecked
    await expect(page.getByRole('checkbox', { name: 'Bug' })).not.toBeChecked();
    await expect(page.getByRole('checkbox', { name: 'Story' })).not.toBeChecked();
    await expect(page.getByRole('checkbox', { name: 'Task', exact: true })).not.toBeChecked();

    // Close settings panel
    await page.keyboard.press('Escape');
  });

  test('View settings "Column by" defaults to "Status"', async ({ page }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // Open view settings
    await page.getByRole('button', { name: 'View settings' }).click();

    // The "Column by" row is a custom dropdown: its trigger button follows the
    // row label and shows the selected option's label.
    const columnByTrigger = page
      .getByText('Column by', { exact: true })
      .locator('xpath=following-sibling::button');
    await expect(columnByTrigger).toHaveText('Status');

    // Close settings panel
    await page.keyboard.press('Escape');
  });

  test('View settings panel contains a "Sprints" filter button', async ({ page }) => {
    await signIn(page);
    await navigateToTimeline(page, projectId);

    // Open view settings
    await page.getByRole('button', { name: 'View settings' }).click();

    // Sprints filter button should be visible
    await expect(page.getByRole('button', { name: 'Sprints' })).toBeVisible();

    // Close settings panel
    await page.keyboard.press('Escape');
  });
});
