// spec: features/projects/task-statuses.feature
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';
const TEST_PROJECT_PREFIX = 'E2E_STATUS_';
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

async function createStatus(
  request: APIRequestContext,
  projectId: string,
  name: string,
  category = 'todo',
): Promise<string> {
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/task-statuses`, {
    data: { name, category },
  });
  const body = await resp.json();
  return body.data.id as string;
}

const signIn = async (page: Page) => {
  await page.goto(`${BASE_URL}/`);
  await ensureLoginForm(page);
  await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
  await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: /Good (morning|afternoon|evening)/i })).toBeVisible();
};

const navigateToProjectSettings = async (page: Page, projectId: string) => {
  await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
};

test.describe('Task Statuses Management', () => {
  test.describe('Viewing task statuses', () => {
    let projectId: string;

    test.beforeEach(async ({ request, context }) => {
      await cleanupTestProjects(request);
      projectId = await createProject(request, `E2E_STATUS_VIEW_${RUN_ID}`);
      await context.clearCookies();
      await context.clearPermissions();
    });

    test.afterEach(async ({ request }) => {
      await cleanupTestProjects(request);
    });

    test('Task Statuses section is reachable from the settings sidebar', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Task Statuses" in the settings sidebar
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // The "Task Statuses" section heading should be visible
      await expect(page.getByRole('heading', { name: 'Task Statuses', level: 3 })).toBeVisible();

      // The section should display a description mentioning workflow statuses
      await expect(page.getByText(/workflow statuses/i)).toBeVisible();
    });

    test('Default statuses are pre-populated for a new project', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Task Statuses" in the settings sidebar
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // The statuses table should contain a status named "Backlog" with category "Backlog"
      const backlogRow = page.getByRole('row').filter({ hasText: 'Backlog' }).first();
      await expect(backlogRow).toContainText('Backlog');

      // The statuses table should contain a status named "Todo" with category "To Do"
      const todoRow = page.getByRole('row').filter({ hasText: 'Todo' });
      await expect(todoRow).toContainText('To Do');

      // The statuses table should contain a status named "In Progress" with category "In Progress"
      const inProgressRow = page.getByRole('row').filter({ hasText: 'In Progress' });
      await expect(inProgressRow).toContainText('In Progress');

      // The statuses table should contain a status named "Done" with category "Done"
      const doneRow = page.getByRole('row').filter({ hasText: 'Done' });
      await expect(doneRow).toContainText('Done');
    });

    test('Statuses table shows the expected columns', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Task Statuses" in the settings sidebar
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // The statuses table should have columns "#", "Name", and "Category"
      await expect(page.getByRole('columnheader', { name: '#' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Category' })).toBeVisible();
    });

    test('Each status row has Edit and Delete action buttons', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Task Statuses" in the settings sidebar
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // Every status row should have an "Edit status" button
      await expect(page.getByRole('button', { name: 'Edit status' }).first()).toBeVisible();

      // Every status row should have a "Delete status" button
      await expect(page.getByRole('button', { name: 'Delete status' }).first()).toBeVisible();
    });

    test('"New status" button is visible on the Task Statuses section', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Task Statuses" in the settings sidebar
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // The "New status" button should be visible
      await expect(page.getByRole('button', { name: 'New status' })).toBeVisible();
    });
  });

  test.describe('Creating a task status', () => {
    let projectId: string;

    test.beforeEach(async ({ request, context }) => {
      await cleanupTestProjects(request);
      projectId = await createProject(request, `E2E_STATUS_CREATE_${RUN_ID}`);
      await context.clearCookies();
      await context.clearPermissions();
    });

    test.afterEach(async ({ request }) => {
      await cleanupTestProjects(request);
    });

    test('Opening the create-status dialog', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();

      // The "Create status" dialog should open
      const dialog = page.getByRole('dialog', { name: 'Create status' });
      await expect(dialog).toBeVisible();

      // The dialog should contain a required "Name" field
      await expect(dialog.getByRole('textbox', { name: 'Name' })).toBeVisible();

      // The dialog should contain a "Category" dropdown
      await expect(dialog.getByRole('combobox', { name: 'Category' })).toBeVisible();

      // The dialog should contain a colour picker
      await expect(dialog.locator('label[title="Custom color"]')).toBeVisible();
    });

    test('"Create status" button is disabled while the name field is empty', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();

      // The "Create status" button should be disabled
      await expect(
        page.getByRole('dialog', { name: 'Create status' }).getByRole('button', { name: 'Create status' }),
      ).toBeDisabled();
    });

    test('"Create status" button becomes enabled after typing a name', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create status' });

      // And the user fills the status name with "E2E In Review"
      await dialog.getByRole('textbox', { name: 'Name' }).fill('E2E In Review');

      // The "Create status" button should be enabled
      await expect(dialog.getByRole('button', { name: 'Create status' })).toBeEnabled();
    });

    test('Category dropdown lists all available categories', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create status' });

      // And the user opens the Category dropdown
      await dialog.getByRole('combobox', { name: 'Category' }).click();

      // The dropdown should list all available categories
      await expect(page.getByRole('option', { name: 'Backlog' })).toBeVisible();
      await expect(page.getByRole('option', { name: 'Refinement' })).toBeVisible();
      await expect(page.getByRole('option', { name: 'Ready' })).toBeVisible();
      await expect(page.getByRole('option', { name: 'To Do' })).toBeVisible();
      await expect(page.getByRole('option', { name: 'In Progress' })).toBeVisible();
      await expect(page.getByRole('option', { name: 'Done' })).toBeVisible();
    });

    test('Default category in the Create status dialog is "To Do"', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create status' });

      // Then the "Category" dropdown should display "To Do" as the selected value
      await expect(dialog.getByRole('combobox', { name: 'Category' })).toContainText('To Do');
    });

    test('Creating a status with only a name succeeds', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create status' });

      // And the user fills the status name with "E2E In Review"
      await dialog.getByRole('textbox', { name: 'Name' }).fill('E2E In Review');

      // And the user clicks "Create status"
      await dialog.getByRole('button', { name: 'Create status' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should contain a status named "E2E In Review"
      await expect(page.getByRole('table').getByText('E2E In Review', { exact: true })).toBeVisible();
    });

    test('Creating a status with name and category succeeds', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create status' });

      // And the user fills the status name with "E2E Refinement"
      await dialog.getByRole('textbox', { name: 'Name' }).fill('E2E Refinement');

      // And the user selects category "Refinement"
      await dialog.getByRole('combobox', { name: 'Category' }).click();
      await page.getByRole('option', { name: 'Refinement' }).click();

      // And the user clicks "Create status"
      await dialog.getByRole('button', { name: 'Create status' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should contain a status named "E2E Refinement" with category "Refinement"
      const row = page.getByRole('row').filter({ hasText: 'E2E Refinement' });
      await expect(row).toContainText('Refinement');
    });

    test('Creating a status with a custom colour succeeds', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create status' });

      // And the user fills the status name with "E2E Custom Colour"
      await dialog.getByRole('textbox', { name: 'Name' }).fill('E2E Custom Colour');

      // And the user enters a custom colour "#22c55e"
      await dialog.getByRole('button', { name: '#22c55e' }).click();

      // And the user clicks "Create status"
      await dialog.getByRole('button', { name: 'Create status' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should contain a status named "E2E Custom Colour"
      await expect(page.getByRole('table').getByText('E2E Custom Colour', { exact: true })).toBeVisible();
    });

    test('Cancelling the create-status dialog discards changes', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create status' });

      // And the user fills the status name with "E2E Should Not Exist"
      await dialog.getByRole('textbox', { name: 'Name' }).fill('E2E Should Not Exist');

      // And the user clicks "Cancel"
      await dialog.getByRole('button', { name: 'Cancel' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should not contain a status named "E2E Should Not Exist"
      await expect(page.getByRole('table').getByText('E2E Should Not Exist', { exact: true })).not.toBeVisible();
    });

    test('Closing the create-status dialog with the Close button discards changes', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks the "New status" button
      await page.getByRole('button', { name: 'New status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create status' });

      // And the user fills the status name with "E2E Should Not Exist via X"
      await dialog.getByRole('textbox', { name: 'Name' }).fill('E2E Should Not Exist via X');

      // And the user clicks the Close button on the dialog
      await dialog.getByRole('button', { name: 'Close' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should not contain a status named "E2E Should Not Exist via X"
      await expect(
        page.getByRole('table').getByText('E2E Should Not Exist via X', { exact: true }),
      ).not.toBeVisible();
    });
  });

  test.describe('Editing a task status', () => {
    let projectId: string;

    test.beforeEach(async ({ request, context }) => {
      await cleanupTestProjects(request);
      projectId = await createProject(request, `E2E_STATUS_EDIT_${RUN_ID}`);
      await createStatus(request, projectId, 'E2E Edit Me', 'todo');
      await context.clearCookies();
      await context.clearPermissions();
    });

    test.afterEach(async ({ request }) => {
      await cleanupTestProjects(request);
    });

    test('Opening the edit-status dialog pre-fills existing values', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Edit status" for the status named "E2E Edit Me"
      await page.getByRole('row').filter({ hasText: 'E2E Edit Me' }).getByRole('button', { name: 'Edit status' }).click();

      // The "Edit status" dialog should open
      const dialog = page.getByRole('dialog', { name: 'Edit status' });
      await expect(dialog).toBeVisible();

      // The "Name" field should be pre-filled with "E2E Edit Me"
      await expect(dialog.getByRole('textbox', { name: 'Name' })).toHaveValue('E2E Edit Me');

      // The "Category" dropdown should display the current category
      await expect(dialog.getByRole('combobox', { name: 'Category' })).toBeVisible();
    });

    test('Saving a new name updates the status in the table', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Edit status" for the status named "E2E Edit Me"
      await page.getByRole('row').filter({ hasText: 'E2E Edit Me' }).getByRole('button', { name: 'Edit status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit status' });

      // And the user clears the name and types "E2E Edited Name"
      await dialog.getByRole('textbox', { name: 'Name' }).clear();
      await dialog.getByRole('textbox', { name: 'Name' }).fill('E2E Edited Name');

      // And the user clicks "Save changes"
      await dialog.getByRole('button', { name: 'Save changes' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should contain a status named "E2E Edited Name"
      await expect(page.getByRole('table').getByText('E2E Edited Name', { exact: true })).toBeVisible();

      // And the statuses table should not contain a status named "E2E Edit Me"
      await expect(page.getByRole('table').getByText('E2E Edit Me', { exact: true })).not.toBeVisible();
    });

    test('Changing the category updates the status in the table', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Edit status" for the status named "E2E Edit Me"
      await page.getByRole('row').filter({ hasText: 'E2E Edit Me' }).getByRole('button', { name: 'Edit status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit status' });

      // And the user selects category "In Progress"
      await dialog.getByRole('combobox', { name: 'Category' }).click();
      await page.getByRole('option', { name: 'In Progress' }).click();

      // And the user clicks "Save changes"
      await dialog.getByRole('button', { name: 'Save changes' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should contain a status named "E2E Edit Me" with category "In Progress"
      const row = page.getByRole('row').filter({ hasText: 'E2E Edit Me' });
      await expect(row).toContainText('In Progress');
    });

    test('Cancelling the edit-status dialog discards changes', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Edit status" for the status named "E2E Edit Me"
      await page.getByRole('row').filter({ hasText: 'E2E Edit Me' }).getByRole('button', { name: 'Edit status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit status' });

      // And the user clears the name and types "E2E Should Not Save"
      await dialog.getByRole('textbox', { name: 'Name' }).clear();
      await dialog.getByRole('textbox', { name: 'Name' }).fill('E2E Should Not Save');

      // And the user clicks "Cancel"
      await dialog.getByRole('button', { name: 'Cancel' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should still contain a status named "E2E Edit Me"
      await expect(page.getByRole('table').getByText('E2E Edit Me', { exact: true })).toBeVisible();

      // And the statuses table should not contain a status named "E2E Should Not Save"
      await expect(page.getByRole('table').getByText('E2E Should Not Save', { exact: true })).not.toBeVisible();
    });

    test('Clearing the name field and saving shows a validation error', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Edit status" for the status named "E2E Edit Me"
      await page.getByRole('row').filter({ hasText: 'E2E Edit Me' }).getByRole('button', { name: 'Edit status' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit status' });

      // And the user clears the name field
      await dialog.getByRole('textbox', { name: 'Name' }).clear();

      // Then the "Save changes" button should be disabled
      await expect(dialog.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    });
  });

  test.describe('Deleting a task status', () => {
    let projectId: string;

    test.beforeEach(async ({ request, context }) => {
      await cleanupTestProjects(request);
      projectId = await createProject(request, `E2E_STATUS_DELETE_${RUN_ID}`);
      await createStatus(request, projectId, 'E2E Delete Me', 'todo');
      await context.clearCookies();
      await context.clearPermissions();
    });

    test.afterEach(async ({ request }) => {
      await cleanupTestProjects(request);
    });

    test('Opening the delete-status dialog shows a confirmation message', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Delete status" for the status named "E2E Delete Me"
      await page
        .getByRole('row')
        .filter({ hasText: 'E2E Delete Me' })
        .getByRole('button', { name: 'Delete status' })
        .click();

      // The "Delete status" dialog should open
      const dialog = page.getByRole('dialog', { name: 'Delete status' });
      await expect(dialog).toBeVisible();

      // The dialog should identify the status being deleted by name
      await expect(dialog).toContainText('E2E Delete Me');

      // The dialog should warn that tasks using this status will lose their status
      await expect(dialog).toContainText(/tasks using this status will lose their status/i);

      // The dialog should warn that the action cannot be undone
      await expect(dialog).toContainText(/cannot be undone/i);
    });

    test('Confirming deletion removes the status from the table', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Delete status" for the status named "E2E Delete Me"
      await page
        .getByRole('row')
        .filter({ hasText: 'E2E Delete Me' })
        .getByRole('button', { name: 'Delete status' })
        .click();
      const dialog = page.getByRole('dialog', { name: 'Delete status' });

      // And the user confirms by clicking "Delete status" in the dialog
      await dialog.getByRole('button', { name: 'Delete status' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should not contain a status named "E2E Delete Me"
      await expect(page.getByRole('table').getByText('E2E Delete Me', { exact: true })).not.toBeVisible();
    });

    test('Cancelling the delete-status dialog keeps the status in the table', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Delete status" for the status named "E2E Delete Me"
      await page
        .getByRole('row')
        .filter({ hasText: 'E2E Delete Me' })
        .getByRole('button', { name: 'Delete status' })
        .click();
      const dialog = page.getByRole('dialog', { name: 'Delete status' });

      // And the user clicks "Cancel" in the delete confirmation dialog
      await dialog.getByRole('button', { name: 'Cancel' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the statuses table should still contain a status named "E2E Delete Me"
      await expect(page.getByRole('table').getByText('E2E Delete Me', { exact: true })).toBeVisible();
    });

    test('Closing the delete-status dialog with the Close button keeps the status', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Task Statuses' }).click();

      // When the user clicks "Delete status" for the status named "E2E Delete Me"
      await page
        .getByRole('row')
        .filter({ hasText: 'E2E Delete Me' })
        .getByRole('button', { name: 'Delete status' })
        .click();
      const dialog = page.getByRole('dialog', { name: 'Delete status' });

      // And the user clicks the Close button on the dialog
      await dialog.getByRole('button', { name: 'Close' }).click();

      // Then the statuses table should still contain a status named "E2E Delete Me"
      await expect(page.getByRole('table').getByText('E2E Delete Me', { exact: true })).toBeVisible();
    });
  });
});
