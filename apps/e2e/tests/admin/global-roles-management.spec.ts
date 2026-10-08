// spec: features/admin/global-roles.feature
// seed: tests/seed.spec.ts

import { allowPolicy, ensureLoginForm } from '../helpers/e2e-api';
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';

const TEST_ROLE_PREFIX = 'E2E_GR_';

// Each permission is a switch named by its label.
function permSwitch(page: Page, label: string) {
  return page.getByRole('switch', { name: label, exact: true });
}

// A role's permissions are edited as switches (Simple) or as the policy document
// (Advanced); the toggle is a pair of tabs.
function modeTab(page: Page, name: 'Simple' | 'Advanced (JSON)') {
  return page.getByRole('tab', { name, exact: true });
}

/** The actions the workspace role called `roleName` allows, as stored. */
async function storedActions(page: Page, roleName: string): Promise<string[]> {
  const response = await page.request.get(`${BASE_URL}/api/v1/admin/roles`);
  expect(response.ok()).toBeTruthy();
  const roles: Array<{
    name: string;
    policy: { statements: Array<{ effect: string; actions: string[] }> };
  }> = (await response.json()).data ?? [];
  const role = roles.find((r) => r.name === roleName);
  expect(role, `role ${roleName} should exist`).toBeTruthy();
  return (role?.policy.statements ?? [])
    .filter((statement) => statement.effect === 'Allow')
    .flatMap((statement) => statement.actions)
    .sort();
}

function policyJson(page: Page) {
  return page.getByRole('textbox', { name: 'Policy (JSON)' });
}

async function cleanupTestRoles(request: APIRequestContext): Promise<void> {
  await request.post(`${BASE_URL}/api/v1/auth/login`, {
    data: { username: USERNAME, password: PASSWORD, rememberMe: false },
  });

  const listResp = await request.get(`${BASE_URL}/api/v1/admin/roles`);
  if (!listResp.ok()) return;

  const body = await listResp.json();
  const roles: Array<{ id: string; name: string }> = body.data ?? [];

  await Promise.all(
    roles
      .filter((r) => r.name.startsWith(TEST_ROLE_PREFIX))
      .map((r) => request.delete(`${BASE_URL}/api/v1/admin/roles/${r.id}`)),
  );
}

async function createTestRole(
  request: APIRequestContext,
  name: string,
  actions: string[] = [],
): Promise<{ id: string; name: string }> {
  await request.post(`${BASE_URL}/api/v1/auth/login`, {
    data: { username: USERNAME, password: PASSWORD, rememberMe: false },
  });
  const response = await request.post(`${BASE_URL}/api/v1/admin/roles`, {
    data: { name, description: '', policy: allowPolicy(actions) },
  });
  expect(response.ok()).toBeTruthy();
  return (await response.json()).data;
}

test.describe('Global Roles Management', () => {
  const signInAsAdmin = async (page: Page) => {
    await page.goto(`${BASE_URL}/`);
    await ensureLoginForm(page);
    await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
    await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
    await page.getByRole('button', { name: 'Sign in' }).click();
    // Wait for home page then navigate directly — avoids mobile sidebar modal staying open
    await page.waitForURL(/\/home/);
    // Wait for the home page to fully hydrate before navigating away (avoids mobile-safari redirect)
    await expect(page.getByRole('heading', { name: /Good (morning|afternoon|evening)/i })).toBeVisible();
    await page.goto(`${BASE_URL}/admin/global-roles`);
    await expect(page.getByRole('heading', { name: 'Global Roles' })).toBeVisible();
  };

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestRoles(request);
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestRoles(request);
  });

  test.describe('Viewing the roles list', () => {
    test('Page header and statistics are visible', async ({ page }) => {
      // 1. Navigate to the app and sign in as admin
      await page.goto(`${BASE_URL}/`);
      await ensureLoginForm(page);
      await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
      await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
      await page.getByRole('button', { name: 'Sign in' }).click();

      // 2. Navigate directly to Global Roles — avoids mobile sidebar modal staying open
      await page.waitForURL(/\/home/);
      // Wait for the home page to fully hydrate before navigating away (avoids webkit/mobile-safari redirect)
      await expect(page.getByRole('heading', { name: /Good (morning|afternoon|evening)/i })).toBeVisible();
      await page.goto(`${BASE_URL}/admin/global-roles`);

      // 3. The "Global Roles" page heading should be visible
      await expect(page.getByRole('heading', { name: 'Global Roles' })).toBeVisible();

      // 4. The statistics bar should show the total number of roles
      await expect(page.getByText(/\d+\s*roles defined/)).toBeVisible();

      // 5. The statistics bar should show the total permission grants across all roles
      await expect(page.getByText(/permission grants across all roles/)).toBeVisible();
    });

    test('Roles table displays expected columns and rows', async ({ page }) => {
      await signInAsAdmin(page);

      // Roles table should have columns "Name", "Description", and "Default"
      await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Description' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Permissions' })).toHaveCount(0);
      await expect(page.getByRole('columnheader', { name: 'Default' })).toBeVisible();

      // Each default role should appear as a row in the table
      await expect(page.getByRole('table').getByText('ADMIN', { exact: true })).toBeVisible();
      await expect(page.getByRole('table').getByText('SUPER_ADMIN', { exact: true })).toBeVisible();
      await expect(page.getByRole('table').getByText('USER', { exact: true })).toBeVisible();
    });

    test('"New Role" button is displayed for users with write permission', async ({ page }) => {
      await signInAsAdmin(page);
      await expect(page.getByRole('button', { name: 'New Role' })).toBeVisible();
    });
  });

  test.describe('Creating a global role', () => {
    test('Opening the create-role dialog', async ({ page }) => {
      await signInAsAdmin(page);

      // When the user clicks the "New Role" button
      await page.getByRole('button', { name: 'New Role' }).click();

      // The role form dialog should open with title "Create Role"
      await expect(page.getByRole('dialog', { name: 'Create Role' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Create Role' })).toBeVisible();

      // The name field should be empty
      await expect(page.getByRole('textbox', { name: 'Role Name' })).toHaveValue('');

      // All permission switches should be off by default
      const dialog = page.getByRole('dialog', { name: 'Create Role' });
      const switches = dialog.getByRole('switch');
      const count = await switches.count();
      for (let i = 0; i < count; i++) {
        await expect(switches.nth(i)).toHaveAttribute('aria-checked', 'false');
      }
    });

    test('Creating a role with a name and selected permissions', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_SECURITY_MANAGER_${timestamp}`;

      // When the user clicks the "New Role" button and fills in details
      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      // The dialog should close and the role should appear in the table
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();
      await expect(page.getByText(/\d+\s*roles defined/)).toBeVisible();
    });

    test('Submitting without a name is blocked', async ({ page }) => {
      await signInAsAdmin(page);

      // When the user clicks the "New Role" button and leaves the name empty
      await page.getByRole('button', { name: 'New Role' }).click();
      await expect(page.getByRole('textbox', { name: 'Role Name' })).toHaveValue('');

      // The button is enabled but submission is blocked by inline validation
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).toBeVisible();
      await expect(page.getByText('Enter a role name of up to 100 characters.')).toBeVisible();
    });

    test('Cancelling the dialog discards changes', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_SHOULD_NOT_EXIST_${timestamp}`;

      // When the user fills the name and then cancels
      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Cancel' }).click();

      // The dialog should close and the role should NOT appear in the table
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).not.toBeVisible();
    });

    test('Creating a role without any permissions is allowed', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_EMPTY_PERMISSIONS_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Create role' }).click();

      // Role should appear without a description
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('No description')).toBeVisible();
      expect(await storedActions(page, roleName)).toEqual([]);
    });

    test('Creating a role with a description shows it in the table', async ({ page }) => {
      await signInAsAdmin(page);

      const roleName = `E2E_GR_DESCRIBED_${Date.now()}`;
      const description = 'Reads users and nothing else';

      await page.getByRole('button', { name: 'New Role' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create Role' });
      await dialog.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await dialog.getByRole('textbox', { name: 'Description' }).fill(description);
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(dialog).not.toBeVisible();
      const row = page.getByRole('row', { name: new RegExp(roleName) });
      await expect(row.getByText(description, { exact: true })).toBeVisible();
      await expect(row.getByText('No description')).toHaveCount(0);
      expect(await storedActions(page, roleName)).toEqual(['users:read']);

      // The description is pre-filled when the role is edited, and can be changed.
      await row.hover();
      await row.getByRole('button', { name: 'Edit role' }).click();
      const edit = page.getByRole('dialog', { name: 'Edit Role' });
      await expect(edit.getByRole('textbox', { name: 'Description' })).toHaveValue(description);
      await edit.getByRole('textbox', { name: 'Description' }).fill('Now with a new description');
      await edit.getByRole('button', { name: 'Save changes' }).click();
      await expect(edit).not.toBeVisible();
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('Now with a new description', { exact: true })).toBeVisible();
    });

    test('Enabling every permission of a domain collapses it to a wildcard', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_GLOBAL_ROLES_WILDCARD_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Write Global Roles').click();
      await permSwitch(page, 'Assign Global Roles').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      expect(await storedActions(page, roleName)).toEqual(['roles:*']);
    });

    test('Enabling all users permissions collapses to users wildcard', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_USERS_WILDCARD_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Users').click();
      await permSwitch(page, 'Write Users').click();
      await permSwitch(page, 'Delete Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      expect(await storedActions(page, roleName)).toEqual(['users:*']);
    });

    test('Enabling all projects permissions collapses to projects wildcard', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_PROJECTS_WILDCARD_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read All Projects').click();
      await permSwitch(page, 'Create Projects').click();
      await permSwitch(page, 'Write Projects').click();
      await permSwitch(page, 'Delete Projects').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      expect(await storedActions(page, roleName)).toEqual(['projects:*']);
    });

    test('Enabling all permissions across all groups collapses each domain independently', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_SUPER_ROLE_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Write Global Roles').click();
      await permSwitch(page, 'Assign Global Roles').click();
      await permSwitch(page, 'Read Users').click();
      await permSwitch(page, 'Write Users').click();
      await permSwitch(page, 'Delete Users').click();
      await permSwitch(page, 'Read All Projects').click();
      await permSwitch(page, 'Create Projects').click();
      await permSwitch(page, 'Write Projects').click();
      await permSwitch(page, 'Delete Projects').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();

      expect(await storedActions(page, roleName)).toEqual(['projects:*', 'roles:*', 'users:*']);
    });

    test('Creating a role with permissions from multiple groups', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_MIXED_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Write Global Roles').click();
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();

      expect(await storedActions(page, roleName)).toEqual(['roles:write', 'users:read']);
    });

    test('Toggling a permission on then off leaves it disabled', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();

      // Enable then disable "Read Users"
      const readUsersSwitch = permSwitch(page, 'Read Users');
      await readUsersSwitch.click();
      await readUsersSwitch.click();

      // The "Read Users" permission switch should be off
      await expect(readUsersSwitch).toHaveAttribute('aria-checked', 'false');
    });
  });

  test.describe('Permission management in the role form dialog', () => {
    test('Permission switches are organised into domain groups', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();

      const dialog = page.getByRole('dialog', { name: 'Create Role' });
      await expect(dialog.getByText('Global Roles', { exact: true })).toBeVisible();
      await expect(dialog.getByText('Users', { exact: true })).toBeVisible();
      await expect(dialog.getByText('AI Agents', { exact: true })).toBeVisible();
      await expect(dialog.getByText('Projects', { exact: true })).toBeVisible();
      await expect(dialog.getByText('Plugins', { exact: true })).toBeVisible();
      await expect(dialog.getByText('Settings', { exact: true })).toBeVisible();
    });

    test('Each permission switch shows a label and description', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();

      const descriptions = [
        'View global role definitions',
        'Create and update global role definitions',
        'Assign global roles to users',
        'View user profiles and list',
        'Create and update user accounts',
        'Remove user accounts',
        'View all projects in the workspace',
        'Create new projects',
        'Update project details',
        'Permanently delete projects',
        'View workspace-level AI agents',
        'View installed plugins',
      ];
      for (const description of descriptions) {
        await expect(page.getByText(description, { exact: true })).toBeVisible();
      }
    });

    test('All permission switches are off by default in the create dialog', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();

      const dialog = page.getByRole('dialog', { name: 'Create Role' });
      const switches = dialog.getByRole('switch');
      const count = await switches.count();
      for (let i = 0; i < count; i++) {
        await expect(switches.nth(i)).toHaveAttribute('aria-checked', 'false');
      }
    });

    test('Enabling a permission updates the switch to on', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();

      const assignGlobalRolesSwitch = permSwitch(page, 'Assign Global Roles');
      await assignGlobalRolesSwitch.click();

      await expect(assignGlobalRolesSwitch).toHaveAttribute('aria-checked', 'true');
    });

    test('The stored role matches the granted permissions', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_COUNT_CHECK_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Delete Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();

      expect(await storedActions(page, roleName)).toEqual(['roles:read', 'users:delete']);
    });

    test('Closing and reopening the dialog resets permission state', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Cancel' }).click();

      await page.getByRole('button', { name: 'New Role' }).click();
      await expect(page.getByRole('heading', { name: 'Create Role' })).toBeVisible();

      const dialog = page.getByRole('dialog', { name: 'Create Role' });
      const switches = dialog.getByRole('switch');
      const count = await switches.count();
      for (let i = 0; i < count; i++) {
        await expect(switches.nth(i)).toHaveAttribute('aria-checked', 'false');
      }
    });
  });

  test.describe('Editing a global role', () => {
    test('Opening the edit dialog pre-populates current data', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_EDITABLE_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();

      await expect(page.getByRole('dialog', { name: 'Edit Role' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Edit Role' })).toBeVisible();
      await expect(page.getByRole('textbox', { name: 'Role Name' })).toHaveValue(roleName);
      await expect(permSwitch(page, 'Read Global Roles')).toHaveAttribute('aria-checked', 'true');
    });

    test('Saving updated role name and permissions', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const originalName = `E2E_GR_EDITABLE_${timestamp}`;
      const renamedName = `E2E_GR_RENAMED_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(originalName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(originalName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(originalName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).clear();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(renamedName);
      await permSwitch(page, 'Delete Users').click();
      await page.getByRole('button', { name: 'Save changes' }).click();

      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(renamedName, { exact: true })).toBeVisible();
    });

    test('Cancelling the edit dialog discards all changes', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const originalName = `E2E_GR_EDITABLE_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(originalName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(originalName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(originalName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).clear();
      await page.getByRole('textbox', { name: 'Role Name' }).fill('E2E_GR_UNSAVED_CHANGE');
      await page.getByRole('button', { name: 'Cancel' }).click();

      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(originalName, { exact: true })).toBeVisible();
      await expect(page.getByRole('table').getByText('E2E_GR_UNSAVED_CHANGE', { exact: true })).not.toBeVisible();
    });

    test('Completing a domain group during edit collapses it to a wildcard', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_PARTIAL_GR_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Write Global Roles').click();
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();
      await permSwitch(page, 'Assign Global Roles').click();
      await page.getByRole('button', { name: 'Save changes' }).click();

      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();
      expect(await storedActions(page, roleName)).toEqual(['roles:*']);
    });

    test('Removing all permissions from an existing role', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_REMOVE_PERMS_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Save changes' }).click();

      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();
      expect(await storedActions(page, roleName)).toEqual([]);
    });

    test('Edit dialog pre-populates the correct permission switches', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_PREPOP_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Assign Global Roles').click();
      await permSwitch(page, 'Delete Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();

      await expect(permSwitch(page, 'Assign Global Roles')).toHaveAttribute('aria-checked', 'true');
      await expect(permSwitch(page, 'Delete Users')).toHaveAttribute('aria-checked', 'true');
      await expect(permSwitch(page, 'Read Global Roles')).toHaveAttribute('aria-checked', 'false');
      await expect(permSwitch(page, 'Write Global Roles')).toHaveAttribute('aria-checked', 'false');
      await expect(permSwitch(page, 'Read Users')).toHaveAttribute('aria-checked', 'false');
    });

    test('Toggling a permission off during edit persists after save', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_TOGGLE_PERSIST_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Save changes' }).click();
      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();

      // Re-open edit dialog and verify switch is still off
      await page.getByRole('row', { name: new RegExp(roleName) }).hover();
      await page.getByRole('row', { name: new RegExp(roleName) }).getByRole('button', { name: 'Edit role' }).click();
      await expect(permSwitch(page, 'Read Users')).toHaveAttribute('aria-checked', 'false');
    });
  });

  test.describe('Deleting a global role', () => {
    test('Confirming deletion removes the role', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_DELETABLE_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Delete role' }).click();

      // Delete confirmation dialog should open showing the role name
      await expect(page.getByRole('heading', { name: 'Delete role' })).toBeVisible();
      await expect(page.getByRole('dialog', { name: 'Delete role' }).getByText(roleName)).toBeVisible();

      // Confirm deletion
      await page.getByRole('dialog', { name: 'Delete role' }).getByRole('button', { name: 'Delete role' }).click();

      // The role should no longer appear in the roles table
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).not.toBeVisible();
      await expect(page.getByText(/\d+\s*roles defined/)).toBeVisible();
    });

    test('Cancelling the delete dialog preserves the role', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GR_PRESERVED_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Delete role' }).click();
      await expect(page.getByRole('heading', { name: 'Delete role' })).toBeVisible();
      await page.getByRole('button', { name: 'Cancel' }).click();

      await expect(page.getByRole('dialog', { name: 'Delete role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();
    });
  });

  // The default role is instance-wide: every new user and global agent starts
  // with it, and other specs create those at the same time. So the real default
  // (USER) is only read here, never changed; making another role the default is
  // driven through the page with the server's answer stubbed. The server side
  // of it is covered by the API's own end-to-end tests.
  test.describe('The default role', () => {
    const rowOf = (page: Page, name: string) =>
      page.getByRole('row').filter({ has: page.getByText(name, { exact: true }) });

    test('Marks the role new accounts start with, and does not let it be changed or deleted', async ({ page, request }) => {
      await signInAsAdmin(page);

      // Exactly one role carries the mark (the column header is not a row of the body).
      const marks = page.locator('tbody').getByText('Default', { exact: true });
      await expect(marks).toHaveCount(1);
      const userRow = rowOf(page, 'USER');
      await expect(userRow.getByText('Default', { exact: true })).toBeVisible();

      // USER is also a built-in role: it carries a lock and can be edited, but
      // not deleted, and there is nothing to make default on the default.
      await expect(userRow.getByLabel('Built-in role')).toBeVisible();
      await expect(userRow.getByRole('button', { name: 'Edit role' })).toBeVisible();
      await expect(userRow.getByRole('button', { name: 'Delete role' })).toHaveCount(0);
      await expect(userRow.getByRole('button', { name: 'Set as default role' })).toHaveCount(0);

      // The server holds the line too, for anything that bypasses the page.
      await request.post(`${BASE_URL}/api/v1/auth/login`, {
        data: { username: USERNAME, password: PASSWORD, rememberMe: false },
      });
      const roles: Array<{ id: string; name: string; is_default: boolean; is_system: boolean }> = (
        await (await request.get(`${BASE_URL}/api/v1/admin/roles`)).json()
      ).data;
      const defaultRole = roles.find((role) => role.is_default);
      if (!defaultRole) throw new Error('one role is the default');
      expect(defaultRole.name).toBe('USER');
      expect(defaultRole.is_system).toBe(true);
      const refused = await request.delete(`${BASE_URL}/api/v1/admin/roles/${defaultRole.id}`);
      expect(refused.status()).toBe(409);
      expect(['ROLE_IS_SYSTEM', 'ROLE_IS_DEFAULT']).toContain((await refused.json()).error_code);
    });

    test('Built-in roles can be edited but not deleted', async ({ page }) => {
      await signInAsAdmin(page);

      for (const name of ['SUPER_ADMIN', 'ADMIN', 'USER']) {
        const row = rowOf(page, name);
        await expect(row.getByLabel('Built-in role')).toBeVisible();
        await expect(row.getByRole('button', { name: 'Edit role' })).toBeVisible();
        await expect(row.getByRole('button', { name: 'Delete role' })).toHaveCount(0);
      }
    });

    test('Making another role the default asks first, then moves the mark in the table', async ({ page, request }) => {
      const role = await createTestRole(request, `E2E_GR_PROMOTED_${Date.now()}`);
      await signInAsAdmin(page);
      await expect(rowOf(page, role.name)).toBeVisible();

      // Answer the request here, and have the list show the outcome afterwards.
      let promoted = false;
      let promotion: { method: string; url: string } | null = null;
      await page.route(`**/api/v1/admin/roles/${role.id}/default`, async (route) => {
        promoted = true;
        promotion = { method: route.request().method(), url: route.request().url() };
        await route.fulfill({ status: 200, json: { success: true, data: { ...role, is_default: true } } });
      });
      await page.route('**/api/v1/admin/roles', async (route) => {
        if (route.request().method() !== 'GET') {
          await route.fallback();
          return;
        }
        const response = await route.fetch();
        const body = await response.json();
        if (promoted) {
          body.data = body.data.map((r: { id: string }) => ({ ...r, is_default: r.id === role.id }));
        }
        await route.fulfill({ response, json: body });
      });

      const target = rowOf(page, role.name);
      await target.hover();
      await target.getByRole('button', { name: 'Set as default role' }).click();

      // It says what the default is for before changing anything.
      const dialog = page.getByRole('dialog', { name: 'Set default role' });
      await expect(dialog).toBeVisible();
      await expect(
        dialog.getByText(`New users and new global agents will start with the ${role.name} role.`),
      ).toBeVisible();
      expect(promoted).toBe(false);
      await dialog.getByRole('button', { name: 'Set as default' }).click();
      await expect(dialog).toHaveCount(0);

      expect(promotion).toEqual({
        method: 'PUT',
        url: expect.stringMatching(new RegExp(`/admin/roles/${role.id}/default$`)),
      });
      // The mark moved: the new default cannot be deleted (its delete action is
      // disabled, with the reason), and the old one offers to become default again.
      const userRow = rowOf(page, 'USER');
      await expect(target.getByText('Default', { exact: true })).toBeVisible();
      await expect(userRow.getByText('Default', { exact: true })).toHaveCount(0);
      await expect(target.getByRole('button', { name: 'Delete role' })).toBeDisabled();
      await expect(target.getByRole('button', { name: 'Set as default role' })).toHaveCount(0);
      await userRow.hover();
      await expect(userRow.getByRole('button', { name: 'Set as default role' })).toBeVisible();
    });

    test('Cancelling the confirmation changes nothing', async ({ page, request }) => {
      const role = await createTestRole(request, `E2E_GR_NOT_PROMOTED_${Date.now()}`);
      await signInAsAdmin(page);

      let requested = false;
      await page.route(`**/api/v1/admin/roles/${role.id}/default`, async (route) => {
        requested = true;
        await route.fallback();
      });

      const target = rowOf(page, role.name);
      await target.hover();
      await target.getByRole('button', { name: 'Set as default role' }).click();
      const dialog = page.getByRole('dialog', { name: 'Set default role' });
      await dialog.getByRole('button', { name: 'Cancel' }).click();

      await expect(dialog).toHaveCount(0);
      expect(requested).toBe(false);
      await expect(rowOf(page, 'USER').getByText('Default', { exact: true })).toBeVisible();
      await expect(target.getByText('Default', { exact: true })).toHaveCount(0);
    });

    test('Warns before a full-access role would become the default', async ({ page, request }) => {
      const role = await createTestRole(request, `E2E_GR_FULL_ACCESS_${Date.now()}`, ['*']);
      await signInAsAdmin(page);

      const target = rowOf(page, role.name);
      await target.hover();
      await target.getByRole('button', { name: 'Set as default role' }).click();

      const dialog = page.getByRole('dialog', { name: 'Set default role' });
      await expect(dialog.getByText(/grants every permission/i)).toBeVisible();
      await dialog.getByRole('button', { name: 'Cancel' }).click();
    });
  });

  // A role is a policy document. The switches are one way to write it; the
  // Advanced view shows and edits the JSON itself, which can say more than the
  // switches can (a Deny, specific resources, conditions).
  test.describe('Editing a role as a policy', () => {
    test('The Simple view is the default and the Advanced view shows the policy as JSON', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();
      const dialog = page.getByRole('dialog', { name: 'Create Role' });
      await expect(modeTab(page, 'Simple')).toHaveAttribute('aria-selected', 'true');
      await expect(modeTab(page, 'Advanced (JSON)')).toHaveAttribute('aria-selected', 'false');

      // What is switched on in Simple is what Advanced shows.
      await permSwitch(page, 'Read Users').click();
      await modeTab(page, 'Advanced (JSON)').click();
      await expect(dialog.getByRole('switch')).toHaveCount(0);
      const json = policyJson(page);
      await expect(json).toBeVisible();
      const policy = JSON.parse(await json.inputValue());
      expect(policy.statements).toHaveLength(1);
      expect(policy.statements[0].effect).toBe('Allow');
      expect(policy.statements[0].actions).toEqual(['users:read']);

      // ...and back again.
      await modeTab(page, 'Simple').click();
      await expect(permSwitch(page, 'Read Users')).toHaveAttribute('aria-checked', 'true');
    });

    test('Creating a role from a JSON policy', async ({ page }) => {
      await signInAsAdmin(page);

      const roleName = `E2E_GR_JSON_${Date.now()}`;
      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await modeTab(page, 'Advanced (JSON)').click();
      await policyJson(page).fill(
        JSON.stringify({
          version: '2026-10-01',
          statements: [
            { effect: 'Allow', actions: ['users:read', 'roles:read'], resources: ['user', 'user/*', 'role', 'role/*'] },
          ],
        }),
      );
      // The server checks the policy while it is typed; Create waits for that.
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('row', { name: new RegExp(roleName) })).toBeVisible();
      expect(await storedActions(page, roleName)).toEqual(['roles:read', 'users:read']);
    });

    test('A policy that is not valid JSON cannot be saved', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(`E2E_GR_BAD_JSON_${Date.now()}`);
      await modeTab(page, 'Advanced (JSON)').click();
      await policyJson(page).fill('{ "statements": [');

      await expect(page.getByRole('alert').filter({ hasText: 'The JSON is not valid' })).toBeVisible();
      await expect(page.getByRole('button', { name: 'Create role' })).toBeDisabled();
    });

    test('A role with a Deny statement opens as JSON only, and the Deny is kept on save', async ({ page, request }) => {
      const roleName = `E2E_GR_DENY_${Date.now()}`;
      await request.post(`${BASE_URL}/api/v1/auth/login`, {
        data: { username: USERNAME, password: PASSWORD, rememberMe: false },
      });
      const created = await request.post(`${BASE_URL}/api/v1/admin/roles`, {
        data: {
          name: roleName,
          description: '',
          policy: {
            version: '2026-10-01',
            statements: [
              { effect: 'Allow', actions: ['users:*'], resources: ['user', 'user/*'] },
              { effect: 'Deny', actions: ['users:delete'], resources: ['user/*'] },
            ],
          },
        },
      });
      expect(created.ok()).toBeTruthy();
      const roleId = (await created.json()).data.id as string;

      await signInAsAdmin(page);
      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await expect(roleRow).toBeVisible();
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();

      const dialog = page.getByRole('dialog', { name: 'Edit Role' });
      await expect(modeTab(page, 'Advanced (JSON)')).toHaveAttribute('aria-selected', 'true');
      await expect(dialog.getByText('This role uses advanced features and can only be edited as JSON.')).toBeVisible();
      // The first statement already names narrower resources than the workspace roots, which is the first reason found.
      await expect(dialog.getByText('It applies to specific resources.')).toBeVisible();
      await expect(dialog.getByRole('switch')).toHaveCount(0);
      await expect(policyJson(page)).toHaveValue(/"Deny"/);

      // The switches cannot show it, so switching back is refused and the JSON stays.
      await modeTab(page, 'Simple').click();
      await expect(modeTab(page, 'Advanced (JSON)')).toHaveAttribute('aria-selected', 'true');
      await expect(policyJson(page)).toHaveValue(/"Deny"/);

      // Saving without touching it keeps the Deny.
      await dialog.getByRole('button', { name: 'Save changes' }).click();
      await expect(dialog).not.toBeVisible();
      const saved = await request.get(`${BASE_URL}/api/v1/admin/roles/${roleId}`);
      expect(saved.ok()).toBeTruthy();
      const statements = (await saved.json()).data.policy.statements as Array<{ effect: string }>;
      expect(statements.map((statement) => statement.effect)).toEqual(['Allow', 'Deny']);
    });
  });

  test.describe('Statistics integrity', () => {
    test('Statistics update after role creation', async ({ page }) => {
      await signInAsAdmin(page);

      const initialText = await page.getByText(/\d+\s*roles defined/).textContent();

      const timestamp = Date.now();
      const roleName = `E2E_GR_STATS_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      const updatedText = await page.getByText(/\d+\s*roles defined/).textContent();
      expect(updatedText).not.toBe(initialText);
    });
  });
});
