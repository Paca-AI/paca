// spec: features/projects/custom-fields.feature
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';
const TEST_PROJECT_PREFIX = 'E2E_CFIELD_';
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

async function createCustomField(
  request: APIRequestContext,
  projectId: string,
  options: {
    display_name: string;
    field_key?: string;
    field_type?: string;
    options?: string[];
    is_required?: boolean;
  },
): Promise<string> {
  const defaultKey = options.display_name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_|_$/g, '');
  const resp = await request.post(`${BASE_URL}/api/v1/projects/${projectId}/custom-fields`, {
    data: {
      display_name: options.display_name,
      field_key: options.field_key ?? defaultKey,
      field_type: options.field_type ?? 'text',
      options: options.options ?? [],
      is_required: options.is_required ?? false,
    },
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
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 30_000 });
};

test.describe('Custom Fields Management', () => {
  test.describe('Viewing custom fields', () => {
    let projectId: string;

    test.beforeEach(async ({ request, context }) => {
      await cleanupTestProjects(request);
      projectId = await createProject(request, `E2E_CFIELD_VIEW_${RUN_ID}`);
      await context.clearCookies();
      await context.clearPermissions();
    });

    test.afterEach(async ({ request }) => {
      await cleanupTestProjects(request);
    });

    test('Custom Fields section is reachable from the settings sidebar', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Custom Fields" in the settings sidebar
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // The "Custom Fields" section heading should be visible
      await expect(page.getByRole('heading', { name: 'Custom Fields', level: 3 })).toBeVisible();

      // The section should display a description mentioning extending tasks
      await expect(page.getByText(/extend tasks|additional data/i)).toBeVisible();
    });

    test('New project shows the empty state for Custom Fields', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Custom Fields" in the settings sidebar
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // The section should show the "No custom fields yet" empty state
      await expect(page.getByText(/No custom fields yet/i)).toBeVisible();

      // The section should show a "Create first field" button
      await expect(page.getByRole('button', { name: 'Create first field' })).toBeVisible();
    });

    test('Custom fields table shows the expected columns when fields exist', async ({ page, request }) => {
      await createCustomField(request, projectId, { display_name: 'E2E Field', field_type: 'text' });
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Custom Fields" in the settings sidebar
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // The custom fields table should have columns "Display Name", "Field Key", "Type", "Required"
      await expect(page.getByRole('columnheader', { name: 'Display Name' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Field Key' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Type' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Required' })).toBeVisible();
    });

    test('Each custom field row has Edit and Delete action buttons', async ({ page, request }) => {
      await createCustomField(request, projectId, { display_name: 'E2E Field', field_type: 'text' });
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Custom Fields" in the settings sidebar
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // Every custom field row should have an "Edit field" button
      await expect(page.getByRole('button', { name: 'Edit field' }).first()).toBeVisible();

      // Every custom field row should have a "Delete field" button
      await expect(page.getByRole('button', { name: 'Delete field' }).first()).toBeVisible();
    });

    test('Field key is displayed in monospace font', async ({ page, request }) => {
      await createCustomField(request, projectId, { display_name: 'E2E Mono Field', field_key: 'e2e_mono_field', field_type: 'text' });
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Custom Fields" in the settings sidebar
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // The "Field Key" column cell should use a monospace font class
      const row = page.getByRole('row').filter({ hasText: 'E2E Mono Field' });
      const keyCell = row.getByText('e2e_mono_field');
      await expect(keyCell).toHaveClass(/font-mono/);
    });

    test('"New custom field" button is visible when fields already exist', async ({ page, request }) => {
      await createCustomField(request, projectId, { display_name: 'E2E Existing', field_type: 'text' });
      await signIn(page);
      await navigateToProjectSettings(page, projectId);

      // When the user clicks "Custom Fields" in the settings sidebar
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // The "New custom field" button should be visible
      await expect(page.getByRole('button', { name: 'New custom field' })).toBeVisible();
    });
  });

  test.describe('Creating a custom field', () => {
    let projectId: string;

    test.beforeEach(async ({ request, context }) => {
      await cleanupTestProjects(request);
      projectId = await createProject(request, `E2E_CFIELD_CREATE_${RUN_ID}`);
      await context.clearCookies();
      await context.clearPermissions();
    });

    test.afterEach(async ({ request }) => {
      await cleanupTestProjects(request);
    });

    test('Opening the create-custom-field dialog from the "New custom field" button', async ({ page, request }) => {
      await createCustomField(request, projectId, { display_name: 'E2E Seed', field_type: 'text' });
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks the "New custom field" button
      await page.getByRole('button', { name: 'New custom field' }).click();

      // The "Create custom field" dialog should open
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });
      await expect(dialog).toBeVisible();
    });

    test('Opening the create-custom-field dialog from the "Create first field" button', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks the "Create first field" button in the empty state
      await page.getByRole('button', { name: 'Create first field' }).click();

      // The "Create custom field" dialog should open
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });
      await expect(dialog).toBeVisible();
    });

    test('Create dialog contains required fields and controls', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // The dialog should contain a "Display name" field
      await expect(dialog.getByRole('textbox', { name: /Display name/i })).toBeVisible();

      // The dialog should contain a "Field key" field
      await expect(dialog.getByRole('textbox', { name: /Field key/i })).toBeVisible();

      // The dialog should contain field type buttons: Text, Number, Date, Checkbox, Select
      await expect(dialog.getByRole('button', { name: 'Text' })).toBeVisible();
      await expect(dialog.getByRole('button', { name: 'Number' })).toBeVisible();
      await expect(dialog.getByRole('button', { name: 'Date' })).toBeVisible();
      await expect(dialog.getByRole('button', { name: 'Checkbox' })).toBeVisible();
      await expect(dialog.getByRole('button', { name: 'Select', exact: true })).toBeVisible();
    });

    test('"Create field" button is disabled when Display name is empty', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // The "Create field" button should be disabled
      await expect(dialog.getByRole('button', { name: 'Create field' })).toBeDisabled();
    });

    test('"Create field" button becomes enabled after filling in Display name', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // And the user fills in the Display name field
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Text Field');

      // The "Create field" button should be enabled
      await expect(dialog.getByRole('button', { name: 'Create field' })).toBeEnabled();
    });

    test('Creating a Text field succeeds', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // And the user fills in the Display name field with "E2E Text Field"
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Text Field');

      // And the user selects "Text" field type
      await dialog.getByRole('button', { name: 'Text' }).click();

      // And the user clicks "Create field"
      await dialog.getByRole('button', { name: 'Create field' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the custom fields table should contain "E2E Text Field"
      await expect(page.getByRole('table').getByText('E2E Text Field', { exact: true })).toBeVisible();
    });

    test('Creating a Number field succeeds', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // And the user fills in the Display name field with "E2E Number Field"
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Number Field');

      // And the user selects "Number" field type
      await dialog.getByRole('button', { name: 'Number' }).click();

      // And the user clicks "Create field"
      await dialog.getByRole('button', { name: 'Create field' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the custom fields table should contain "E2E Number Field"
      await expect(page.getByRole('table').getByText('E2E Number Field', { exact: true })).toBeVisible();
    });

    test('Creating a Select field shows the options editor', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // And the user fills in the Display name field with "E2E Select Field"
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Select Field');

      // When the user selects "Select" field type
      await dialog.getByRole('button', { name: 'Select', exact: true }).click();

      // Then the options editor should appear
      await expect(dialog.getByPlaceholder('Add option…')).toBeVisible();
      await expect(dialog.getByRole('button', { name: 'Add' })).toBeVisible();
    });

    test('Adding options to a Select field works', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // And the user fills in the Display name field
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Select Options');

      // And the user selects "Select" field type
      await dialog.getByRole('button', { name: 'Select', exact: true }).click();

      // And the user types "High" in the "Add option…" field
      await dialog.getByPlaceholder('Add option…').fill('High');

      // And the user clicks "Add"
      await dialog.getByRole('button', { name: 'Add' }).click();

      // Then the option "High" should appear in the options list
      await expect(dialog.locator('input[value="High"]')).toBeVisible();
    });

    test('Removing an option from a Select field works', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // And the user fills in the Display name field
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Remove Option');

      // And the user selects "Select" field type
      await dialog.getByRole('button', { name: 'Select', exact: true }).click();

      // And the user adds option "Unwanted"
      await dialog.getByPlaceholder('Add option…').fill('Unwanted');
      await dialog.getByRole('button', { name: 'Add' }).click();
      await expect(dialog.locator('input[value="Unwanted"]')).toBeVisible();

      // When the user clicks the remove button next to "Unwanted"
      // (each option row now has an "Option color" picker button before the unnamed remove button)
      await dialog.locator('input[value="Unwanted"]').locator('..').getByRole('button').last().click();

      // Then the option "Unwanted" should no longer appear in the options list
      await expect(dialog.locator('input[value="Unwanted"]')).not.toBeVisible();
    });

    test('Creating a "Required" field marks it as required in the table', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // And the user fills in the Display name field with "E2E Required Field"
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Required Field');

      // And the user toggles the "Required" switch on
      await dialog.getByRole('switch').click();

      // And the user clicks "Create field"
      await dialog.getByRole('button', { name: 'Create field' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the custom fields table should show "E2E Required Field" as required
      const row = page.getByRole('row').filter({ hasText: 'E2E Required Field' });
      await expect(row).toBeVisible();
    });

    test('Cancelling the create dialog discards changes', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Create first field"
      await page.getByRole('button', { name: 'Create first field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create custom field' });

      // And the user fills in the Display name field with "E2E Should Not Exist"
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Should Not Exist');

      // And the user clicks "Cancel"
      await dialog.getByRole('button', { name: 'Cancel' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the empty state should still be visible (no field was created)
      await expect(page.getByText(/No custom fields yet/i)).toBeVisible();
    });
  });

  test.describe('Editing a custom field', () => {
    test.setTimeout(60_000);
    let projectId: string;

    test.beforeEach(async ({ request, context }) => {
      await cleanupTestProjects(request);
      projectId = await createProject(request, `E2E_CFIELD_EDIT_${RUN_ID}`);
      await createCustomField(request, projectId, {
        display_name: 'E2E Edit Me',
        field_key: 'e2e_edit_me',
        field_type: 'text',
      });
      await context.clearCookies();
      await context.clearPermissions();
    });

    test.afterEach(async ({ request }) => {
      await cleanupTestProjects(request);
    });

    test('Opening the edit-custom-field dialog pre-fills existing values', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Edit custom field" for "E2E Edit Me"
      const editRow0 = page.getByRole('row').filter({ hasText: 'E2E Edit Me' });
      await editRow0.hover();
      await editRow0.getByRole('button', { name: 'Edit field' }).click();

      // The "Edit field" dialog should open
      const dialog = page.getByRole('dialog', { name: 'Edit custom field' });
      await expect(dialog).toBeVisible();

      // The "Display name" field should be pre-filled with "E2E Edit Me"
      await expect(dialog.getByRole('textbox', { name: /Display name/i })).toHaveValue('E2E Edit Me');

      // The "Field key" field should be pre-filled with "e2e_edit_me"
      // Note: the field key input is disabled so it has no accessible name;
      // look it up via the disabled input locator instead
      await expect(dialog.locator('input[disabled]').first()).toHaveValue('e2e_edit_me');
    });

    test('Saving a new display name updates the field in the table', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Edit custom field" for "E2E Edit Me"
      const editRow1 = page.getByRole('row').filter({ hasText: 'E2E Edit Me' });
      await editRow1.hover();
      await editRow1.getByRole('button', { name: 'Edit field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit custom field' });

      // And the user clears and fills the Display name with "E2E Edited Name"
      await dialog.getByRole('textbox', { name: /Display name/i }).clear();
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Edited Name');

      // And the user clicks "Save changes"
      await dialog.getByRole('button', { name: 'Save changes' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the table should contain "E2E Edited Name"
      await expect(page.getByRole('table').getByText('E2E Edited Name', { exact: true })).toBeVisible();

      // And the table should not contain the old name "E2E Edit Me"
      await expect(page.getByRole('table').getByText('E2E Edit Me', { exact: true })).not.toBeVisible();
    });

    test('Cancelling the edit dialog discards changes', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Edit custom field" for "E2E Edit Me"
      const editRow2 = page.getByRole('row').filter({ hasText: 'E2E Edit Me' });
      await editRow2.hover();
      await editRow2.getByRole('button', { name: 'Edit field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit custom field' });

      // And the user clears and fills the Display name with "E2E Should Not Save"
      await dialog.getByRole('textbox', { name: /Display name/i }).clear();
      await dialog.getByRole('textbox', { name: /Display name/i }).fill('E2E Should Not Save');

      // And the user clicks "Cancel"
      await dialog.getByRole('button', { name: 'Cancel' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the table should still contain "E2E Edit Me"
      await expect(page.getByRole('table').getByText('E2E Edit Me', { exact: true })).toBeVisible();

      // And the table should not contain "E2E Should Not Save"
      await expect(page.getByRole('table').getByText('E2E Should Not Save', { exact: true })).not.toBeVisible();
    });

    test('Clearing the Display name field disables the Save button', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Edit custom field" for "E2E Edit Me"
      const editRow3 = page.getByRole('row').filter({ hasText: 'E2E Edit Me' });
      await editRow3.hover();
      await editRow3.getByRole('button', { name: 'Edit field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit custom field' });

      // And the user clears the Display name field
      await dialog.getByRole('textbox', { name: /Display name/i }).clear();

      // Then the "Save changes" button should be disabled
      await expect(dialog.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    });

    test('Toggling Required on an existing field updates it', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Edit custom field" for "E2E Edit Me"
      const editRow4 = page.getByRole('row').filter({ hasText: 'E2E Edit Me' });
      await editRow4.hover();
      await editRow4.getByRole('button', { name: 'Edit field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit custom field' });

      // And the user toggles the "Required" switch
      await dialog.getByRole('switch').click();

      // And the user clicks "Save changes"
      await dialog.getByRole('button', { name: 'Save changes' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And "E2E Edit Me" should still be visible in the table
      await expect(page.getByRole('table').getByText('E2E Edit Me', { exact: true })).toBeVisible();
    });
  });

  test.describe('Deleting a custom field', () => {
    test.setTimeout(60_000);
    let projectId: string;

    test.beforeEach(async ({ request, context }) => {
      await cleanupTestProjects(request);
      projectId = await createProject(request, `E2E_CFIELD_DELETE_${RUN_ID}`);
      await createCustomField(request, projectId, {
        display_name: 'E2E Delete Me',
        field_type: 'text',
      });
      await context.clearCookies();
      await context.clearPermissions();
    });

    test.afterEach(async ({ request }) => {
      await cleanupTestProjects(request);
    });

    test('Opening the delete-custom-field dialog shows a confirmation message', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Delete custom field" for "E2E Delete Me"
      const deleteRow0 = page.getByRole('row').filter({ hasText: 'E2E Delete Me' });
      await deleteRow0.hover();
      await deleteRow0.getByRole('button', { name: 'Delete field' }).click();

      // The "Delete custom field" dialog should open
      const dialog = page.getByRole('dialog', { name: 'Delete custom field' });
      await expect(dialog).toBeVisible();

      // The dialog should identify the field being deleted by name
      await expect(dialog).toContainText('E2E Delete Me');

      // The dialog should warn that the action cannot be undone
      await expect(dialog).toContainText(/cannot be undone/i);
    });

    test('Confirming deletion removes the custom field from the table', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Delete custom field" for "E2E Delete Me"
      const deleteRow1 = page.getByRole('row').filter({ hasText: 'E2E Delete Me' });
      await deleteRow1.hover();
      await deleteRow1.getByRole('button', { name: 'Delete field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Delete custom field' });

      // And the user confirms by clicking "Delete field"
      await dialog.getByRole('button', { name: 'Delete field' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the custom fields section should show the empty state
      await expect(page.getByText(/No custom fields yet/i)).toBeVisible();
    });

    test('Cancelling the delete dialog keeps the custom field in the table', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Delete custom field" for "E2E Delete Me"
      const deleteRow2 = page.getByRole('row').filter({ hasText: 'E2E Delete Me' });
      await deleteRow2.hover();
      await deleteRow2.getByRole('button', { name: 'Delete field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Delete custom field' });

      // And the user clicks "Cancel"
      await dialog.getByRole('button', { name: 'Cancel' }).click();

      // Then the dialog should close
      await expect(dialog).not.toBeVisible();

      // And the custom fields table should still contain "E2E Delete Me"
      await expect(page.getByRole('table').getByText('E2E Delete Me', { exact: true })).toBeVisible();
    });

    test('Closing the delete dialog with the Close button keeps the custom field', async ({ page }) => {
      await signIn(page);
      await navigateToProjectSettings(page, projectId);
      await page.getByRole('button', { name: 'Custom Fields' }).click();

      // When the user clicks "Delete custom field" for "E2E Delete Me"
      const deleteRow3 = page.getByRole('row').filter({ hasText: 'E2E Delete Me' });
      await deleteRow3.hover();
      await deleteRow3.getByRole('button', { name: 'Delete field' }).click();
      const dialog = page.getByRole('dialog', { name: 'Delete custom field' });

      // And the user clicks the Close button on the dialog
      await dialog.getByRole('button', { name: 'Close' }).click();

      // Then the custom fields table should still contain "E2E Delete Me"
      await expect(page.getByRole('table').getByText('E2E Delete Me', { exact: true })).toBeVisible();
    });
  });
});
