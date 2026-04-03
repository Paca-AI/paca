// spec: features/admin/global-roles.feature
// seed: tests/seed.spec.ts

import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';

const TEST_ROLE_PREFIX = 'E2E_';

function permSwitch(page: Page, label: string) {
  return page.getByText(label, { exact: true }).locator('xpath=../following-sibling::*[@role="switch"]');
}

async function cleanupTestRoles(request: APIRequestContext): Promise<void> {
  await request.post(`${BASE_URL}/api/v1/auth/login`, {
    data: { username: USERNAME, password: PASSWORD, rememberMe: false },
  });

  const listResp = await request.get(`${BASE_URL}/api/v1/admin/global-roles`);
  if (!listResp.ok()) return;

  const body = await listResp.json();
  const roles: Array<{ id: string; name: string }> = body.data ?? [];

  await Promise.all(
    roles
      .filter((r) => r.name.startsWith(TEST_ROLE_PREFIX))
      .map((r) => request.delete(`${BASE_URL}/api/v1/admin/global-roles/${r.id}`)),
  );
}

test.describe('Global Roles Management', () => {
  const signInAsAdmin = async (page: Page) => {
    await page.goto(`${BASE_URL}/`);
    await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
    await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
    await page.getByRole('button', { name: 'Sign in' }).click();
    // Wait for home page then navigate directly — avoids mobile sidebar modal staying open
    await page.waitForURL(/\/home/);
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
      await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
      await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
      await page.getByRole('button', { name: 'Sign in' }).click();

      // 2. Navigate directly to Global Roles — avoids mobile sidebar modal staying open
      await page.waitForURL(/\/home/);
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

      // Roles table should have columns "Name", "Permissions", and "Created"
      await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Permissions' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Created' })).toBeVisible();

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
      const roleName = `E2E_SECURITY_MANAGER_${timestamp}`;

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
      await expect(page.getByText('Role name is required.')).toBeVisible();
    });

    test('Cancelling the dialog discards changes', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_SHOULD_NOT_EXIST_${timestamp}`;

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
      const roleName = `E2E_EMPTY_PERMISSIONS_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Create role' }).click();

      // Role should appear with zero active permissions
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('No permissions assigned')).toBeVisible();
    });

    test('Enabling all global_roles permissions collapses to wildcard', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_GLOBAL_ROLES_WILDCARD_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Write Global Roles').click();
      await permSwitch(page, 'Assign Global Roles').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('global_roles.*')).toBeVisible();
    });

    test('Enabling all users permissions collapses to users wildcard', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_USERS_WILDCARD_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Users').click();
      await permSwitch(page, 'Write Users').click();
      await permSwitch(page, 'Delete Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('users.*')).toBeVisible();
    });

    test('Enabling all projects permissions collapses to projects wildcard', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_PROJECTS_WILDCARD_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read All Projects').click();
      await permSwitch(page, 'Create Projects').click();
      await permSwitch(page, 'Write Projects').click();
      await permSwitch(page, 'Delete Projects').click();
      await permSwitch(page, 'Read Project Members').click();
      await permSwitch(page, 'Write Project Members').click();
      await permSwitch(page, 'Read Project Roles').click();
      await permSwitch(page, 'Write Project Roles').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('projects.*')).toBeVisible();
    });

    test('Enabling all permissions across all groups collapses each domain independently', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_SUPER_ROLE_${timestamp}`;

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
      await permSwitch(page, 'Read Project Members').click();
      await permSwitch(page, 'Write Project Members').click();
      await permSwitch(page, 'Read Project Roles').click();
      await permSwitch(page, 'Write Project Roles').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await expect(roleRow.getByText('global_roles.*')).toBeVisible();
      await expect(roleRow.getByText('users.*')).toBeVisible();
      await expect(roleRow.getByText('projects.*')).toBeVisible();
    });

    test('Creating a role with permissions from multiple groups', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_MIXED_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Write Global Roles').click();
      await permSwitch(page, 'Read Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await expect(roleRow.getByText('global_roles.write')).toBeVisible();
      await expect(roleRow.getByText('users.read')).toBeVisible();
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
      await expect(dialog.getByText('Global Roles').first()).toBeVisible();
      await expect(dialog.getByText('Users').first()).toBeVisible();
      await expect(dialog.getByText('Projects').first()).toBeVisible();
    });

    test('Each permission switch shows a label and description', async ({ page }) => {
      await signInAsAdmin(page);

      await page.getByRole('button', { name: 'New Role' }).click();

      await expect(page.getByText('View global role definitions')).toBeVisible();
      await expect(page.getByText('Create and update global role definitions')).toBeVisible();
      await expect(page.getByText('Assign global roles to users')).toBeVisible();
      await expect(page.getByText('View user profiles and list')).toBeVisible();
      await expect(page.getByText('Create and update user accounts')).toBeVisible();
      await expect(page.getByText('Remove user accounts')).toBeVisible();
      await expect(page.getByText('View all projects in the workspace')).toBeVisible();
      await expect(page.getByText('Create new projects')).toBeVisible();
      await expect(page.getByText('Update project details')).toBeVisible();
      await expect(page.getByText('Permanently delete projects')).toBeVisible();
      await expect(page.getByText('View members of any project')).toBeVisible();
      await expect(page.getByText('Add, remove, and update members in any project')).toBeVisible();
      await expect(page.getByText('View roles defined in any project')).toBeVisible();
      await expect(page.getByText('Create and update roles in any project')).toBeVisible();
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

    test('Permissions count in the table matches granted permissions', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_COUNT_CHECK_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Read Global Roles').click();
      await permSwitch(page, 'Delete Users').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await expect(roleRow.getByText('global_roles.read')).toBeVisible();
      await expect(roleRow.getByText('users.delete')).toBeVisible();
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
      const roleName = `E2E_EDITABLE_${timestamp}`;

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
      const originalName = `E2E_EDITABLE_${timestamp}`;
      const renamedName = `E2E_RENAMED_${timestamp}`;

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
      const originalName = `E2E_EDITABLE_${timestamp}`;

      await page.getByRole('button', { name: 'New Role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(originalName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'Create Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(originalName, { exact: true })).toBeVisible();

      const roleRow = page.getByRole('row', { name: new RegExp(originalName) });
      await roleRow.hover();
      await roleRow.getByRole('button', { name: 'Edit role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).clear();
      await page.getByRole('textbox', { name: 'Role Name' }).fill('E2E_UNSAVED_CHANGE');
      await page.getByRole('button', { name: 'Cancel' }).click();

      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(originalName, { exact: true })).toBeVisible();
      await expect(page.getByRole('table').getByText('E2E_UNSAVED_CHANGE', { exact: true })).not.toBeVisible();
    });

    test('Completing a domain group during edit collapses it to a wildcard', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_PARTIAL_GR_${timestamp}`;

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
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('global_roles.*')).toBeVisible();
    });

    test('Removing all permissions from an existing role', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_REMOVE_PERMS_${timestamp}`;

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
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('No permissions assigned')).toBeVisible();
    });

    test('Edit dialog pre-populates the correct permission switches', async ({ page }) => {
      await signInAsAdmin(page);

      const timestamp = Date.now();
      const roleName = `E2E_PREPOP_${timestamp}`;

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
      const roleName = `E2E_TOGGLE_PERSIST_${timestamp}`;

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
      const roleName = `E2E_DELETABLE_${timestamp}`;

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
      const roleName = `E2E_PRESERVED_${timestamp}`;

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

  test.describe('Statistics integrity', () => {
    test('Statistics update after role creation', async ({ page }) => {
      await signInAsAdmin(page);

      const initialText = await page.getByText(/\d+\s*roles defined/).textContent();

      const timestamp = Date.now();
      const roleName = `E2E_STATS_${timestamp}`;

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
