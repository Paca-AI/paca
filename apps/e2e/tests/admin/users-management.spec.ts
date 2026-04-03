// spec: features/admin/users.feature
// seed: tests/seed.spec.ts

import {
  expect,
  test,
  type APIRequestContext,
  type Locator,
  type Page,
} from '@playwright/test';
import { AUTH_FILE } from '../../playwright.config';

const AUTH_URL = `${process.env.E2E_BASE_URL ?? 'http://localhost'}/api/v1/auth/login`;
const USERS_URL = `${process.env.E2E_BASE_URL ?? 'http://localhost'}/api/v1/admin/users`;
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';
const TEMP_PASSWORD = 'TempPassword123!';
const DEFAULT_ROLE_USER_NEW_PASSWORD = 'BDDUserDefaultRole123!';
const RESET_USER_NEW_PASSWORD = 'ResettableUser123!';
const MOBILE_BREAKPOINT = 768;
const TEST_USER_PREFIX = 'E2E_USERS_';
const CLEANUP_PREFIXES = [TEST_USER_PREFIX, 'API_INSPECT_', 'RESETTABLE_UI_'];
const TEST_RUN_ID = Date.now().toString(36).slice(-6).toUpperCase();

type UserRole = 'ADMIN' | 'SUPER_ADMIN' | 'USER';

type AdminUser = {
  id: string;
  username: string;
  full_name: string;
  role: UserRole;
  must_change_password: boolean;
  created_at: string;
};

function uniqueUsername(label: string) {
  return `${TEST_USER_PREFIX}${label}_${TEST_RUN_ID}`;
}

async function authenticateAdmin(request: APIRequestContext) {
  const response = await request.post(AUTH_URL, {
    data: {
      username: USERNAME,
      password: PASSWORD,
      rememberMe: false,
    },
  });

  expect(response.ok()).toBeTruthy();
}

async function listUsers(request: APIRequestContext): Promise<AdminUser[]> {
  const response = await request.get(USERS_URL);
  expect(response.ok()).toBeTruthy();

  const body = await response.json();
  return body.data.items ?? [];
}

async function cleanupTestUsers(request: APIRequestContext) {
  const users = await listUsers(request);

  await Promise.all(
    users
      .filter((user) => CLEANUP_PREFIXES.some((prefix) => user.username.startsWith(prefix)))
      .map((user) => request.delete(`${USERS_URL}/${user.id}`)),
  );
}

async function createUser(
  request: APIRequestContext,
  user: { username: string; fullName: string; role?: UserRole },
) {
  const response = await request.post(USERS_URL, {
    data: {
      username: user.username,
      full_name: user.fullName,
      role: user.role ?? 'USER',
      password: TEMP_PASSWORD,
    },
  });

  expect(response.ok()).toBeTruthy();
}

async function ensureUser(
  request: APIRequestContext,
  user: { username: string; fullName: string; role?: UserRole },
) {
  await deleteUserIfExists(request, user.username);
  await createUser(request, user);
}

async function deleteUserIfExists(request: APIRequestContext, username: string) {
  const users = await listUsers(request);
  const user = users.find((candidate) => candidate.username === username);

  if (!user) {
    return;
  }

  const response = await request.delete(`${USERS_URL}/${user.id}`);
  expect(response.ok()).toBeTruthy();
}

async function openUsersPage(page: Page) {
  await page.goto('/admin/users');
  // If a prior test invalidated the server-side session stored in AUTH_FILE, the app
  // redirects to login. Re-authenticate directly so these tests remain self-contained.
  if (!page.url().includes('/admin')) {
    await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
    await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await page.waitForURL(/\/home/);
    await page.goto('/admin/users');
  }
  await expect(page.getByRole('heading', { name: 'User Management' })).toBeVisible();
}

function userRow(page: Page, username: string): Locator {
  return page.getByRole('row').filter({
    has: page.getByText(username, { exact: true }),
  });
}

function currentAdminRow(page: Page): Locator {
  return page
    .getByRole('row')
    .filter({ has: page.getByText('admin', { exact: true }) })
    .filter({ has: page.getByText('you', { exact: true }) });
}

function profileMenuButton(page: Page): Locator {
  return page.getByRole('button', { name: /Admin super_admin/i });
}

async function expectUsersSummary(page: Page, totalUsers: number) {
  await expect(page.getByText(/users? in system/i)).toBeVisible();
  await expect(page.getByRole('row')).toHaveCount(totalUsers + 1);
}

async function expectLoginPage(page: Page) {
  await expect(page.getByRole('textbox', { name: 'Username' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Password' })).toBeVisible();
  await expect(page.getByRole('button', { name: /sign in/i })).toBeVisible();
}

async function login(page: Page, username: string, password: string) {
  await expectLoginPage(page);
  await page.getByRole('textbox', { name: 'Username' }).fill(username);
  await page.getByRole('textbox', { name: 'Password' }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

async function openAdminProfileMenu(page: Page) {
  const viewport = page.viewportSize();

  if (viewport && viewport.width <= MOBILE_BREAKPOINT) {
    const menuButton = profileMenuButton(page);
    const isVisible = await menuButton.isVisible().catch(() => false);

    if (!isVisible) {
      await page.getByRole('button', { name: 'Toggle Sidebar' }).first().click();
    }
  }

  await profileMenuButton(page).click();
}

async function signOutAdmin(page: Page) {
  await openAdminProfileMenu(page);
  await page.getByRole('menuitem', { name: 'Log out' }).click();
  await expectLoginPage(page);
}

async function expectForcedPasswordChangePage(page: Page) {
  await expect(page).toHaveURL(/\/change-password$/);
  await expect(page.getByRole('heading', { name: 'Set new password' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Current password' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'New password', exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Confirm new password' })).toBeVisible();
}

async function changePassword(page: Page, currentPassword: string, newPassword: string) {
  await page.getByRole('textbox', { name: 'Current password' }).fill(currentPassword);
  await page.getByRole('textbox', { name: 'New password', exact: true }).fill(newPassword);
  await page.getByRole('textbox', { name: 'Confirm new password' }).fill(newPassword);
  await page.getByRole('button', { name: 'Change password' }).click();
  await expectLoginPage(page);
}

async function expectHomePage(page: Page) {
  await expect(page).toHaveURL(/\/home$/);
  await expect(
    page.getByRole('heading', { name: /Good (morning|afternoon|evening),/i }),
  ).toBeVisible({ timeout: 10000 });
}

test.describe('User Management', () => {
  test.use({ storageState: AUTH_FILE });

  test.beforeEach(async ({ request }) => {
    await authenticateAdmin(request);
    await cleanupTestUsers(request);
  });

  test.afterEach(async ({ request }) => {
    await authenticateAdmin(request);
    await cleanupTestUsers(request);
  });

  test('shows the users page header, summary, columns, and protected admin actions', async ({
    page,
    request,
  }) => {
    await openUsersPage(page);

    await expect(page.getByText('View and manage user accounts and their assigned roles.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'New User' })).toBeVisible();
    await expectUsersSummary(page, (await listUsers(request)).length);

    await expect(page.getByRole('columnheader', { name: 'Username' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Full Name' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Role' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Created' })).toBeVisible();

    const adminRow = currentAdminRow(page);
    await expect(adminRow).toBeVisible();
    await expect(adminRow.getByText('SUPER_ADMIN', { exact: true })).toBeVisible();
    await expect(adminRow.getByRole('button', { name: 'Edit user' })).toBeVisible();
    await expect(adminRow.getByRole('button', { name: 'Reset password' })).toBeVisible();
    await expect(adminRow.getByRole('button', { name: 'Delete user' })).toHaveCount(0);
  });

  test('opens the create-user dialog and shows validation and role options', async ({ page }) => {
    await openUsersPage(page);

    await page.getByRole('button', { name: 'New User' }).click();

    const dialog = page.getByRole('dialog', { name: 'Create User' });
    await expect(dialog).toBeVisible();
    await expect(
      dialog.getByText('A secure temporary password will be generated automatically.'),
    ).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: 'Username' })).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: 'Full Name' })).toBeVisible();
    await expect(dialog.getByRole('combobox', { name: /Role/ })).toBeVisible();

    await dialog.getByRole('button', { name: 'Create user' }).click();
    await expect(dialog.getByText('Full name is required.')).toBeVisible();

    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Jane Doe');
    await dialog.getByRole('button', { name: 'Create user' }).click();
    await expect(dialog.getByText('Username is required.')).toBeVisible();
    await expect(dialog).toBeVisible();

    await dialog.getByRole('combobox', { name: /Role/ }).click();
    await expect(page.getByRole('option', { name: 'ADMIN', exact: true })).toBeVisible();
    await expect(page.getByRole('option', { name: 'SUPER_ADMIN' })).toBeVisible();
    await expect(page.getByRole('option', { name: 'USER' })).toBeVisible();
  });

  test('creates a user with the default USER role and requires a password change on first login', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('DEFAULT');

    await openUsersPage(page);
    await page.getByRole('button', { name: 'New User' }).click();

    const dialog = page.getByRole('dialog', { name: 'Create User' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(username);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Default Role User');
    await dialog.getByRole('button', { name: 'Create user' }).click();

    const successDialog = page.getByRole('dialog', { name: 'User created' });
    await expect(successDialog).toBeVisible();
    await expect(
      successDialog.getByText(/will be asked to change it on first login/i),
    ).toBeVisible();

    const temporaryPassword = await successDialog.getByRole('textbox').inputValue();
    await expect(temporaryPassword).not.toEqual('');
    await expect(successDialog.getByText('This password will not be shown again.')).toBeVisible();
    await successDialog.getByRole('button', { name: 'Done' }).click();

    const createdRow = userRow(page, username);
    await expect(createdRow).toBeVisible();
    await expect(createdRow.getByText('USER', { exact: true })).toBeVisible();
    await expect(createdRow.getByText(/pwd reset/i)).toBeVisible();
    await expectUsersSummary(page, (await listUsers(request)).length);

    await signOutAdmin(page);
    await login(page, username, temporaryPassword);
    await expectForcedPasswordChangePage(page);

    await changePassword(page, temporaryPassword, DEFAULT_ROLE_USER_NEW_PASSWORD);
    await login(page, username, DEFAULT_ROLE_USER_NEW_PASSWORD);
    await expectHomePage(page);
  });

  test('creates a user with an explicitly selected role and cancels a discarded draft', async ({
    page,
  }) => {
    const selectedRoleUser = uniqueUsername('ADMIN');
    const cancelledUser = uniqueUsername('CANCELLED');

    await openUsersPage(page);

    await page.getByRole('button', { name: 'New User' }).click();
    let dialog = page.getByRole('dialog', { name: 'Create User' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(selectedRoleUser);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Admin Role User');
    await dialog.getByRole('combobox', { name: /Role/ }).click();
    await page.getByRole('option', { name: 'ADMIN', exact: true }).click();
    await dialog.getByRole('button', { name: 'Create user' }).click();

    const successDialog = page.getByRole('dialog', { name: 'User created' });
    await expect(successDialog).toBeVisible();
    await successDialog.getByRole('button', { name: 'Done' }).click();

    await expect(userRow(page, selectedRoleUser).getByText('ADMIN', { exact: true })).toBeVisible();

    await page.getByRole('button', { name: 'New User' }).click();
    dialog = page.getByRole('dialog', { name: 'Create User' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(cancelledUser);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Cancelled User');
    await dialog.getByRole('button', { name: 'Cancel' }).click();

    await expect(userRow(page, cancelledUser)).toHaveCount(0);
  });

  test('opens the edit dialog with current values and saves updated full name and role', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('EDITABLE');
    await ensureUser(request, {
      username,
      fullName: 'Editable User',
      role: 'USER',
    });

    await openUsersPage(page);
    const row = userRow(page, username);
    await row.getByRole('button', { name: 'Edit user' }).click();

    const dialog = page.getByRole('dialog', { name: 'Edit User' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: 'Full Name' })).toHaveValue('Editable User');
    await expect(dialog.getByRole('combobox', { name: /Role/ })).toContainText('USER');

    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Edited User Name');
    await dialog.getByRole('combobox', { name: /Role/ }).click();
    await page.getByRole('option', { name: 'ADMIN', exact: true }).click();
    await dialog.getByRole('button', { name: 'Save changes' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(row.getByText('Edited User Name', { exact: true })).toBeVisible();
    await expect(row.getByText('ADMIN', { exact: true })).toBeVisible();
  });

  test('cancels the edit dialog without saving changes', async ({ page, request }) => {
    const username = uniqueUsername('EDIT_CANCEL');
    await ensureUser(request, {
      username,
      fullName: 'Original Name',
      role: 'USER',
    });

    await openUsersPage(page);
    const row = userRow(page, username);
    await row.getByRole('button', { name: 'Edit user' }).click();

    const dialog = page.getByRole('dialog', { name: 'Edit User' });
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Unsaved Name');
    await dialog.getByRole('button', { name: 'Cancel' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(row.getByText('Original Name', { exact: true })).toBeVisible();
    await expect(row.getByText('Unsaved Name')).toHaveCount(0);
  });

  test('opens and cancels the reset-password dialog without changing the user row', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('RESET_CANCEL');
    await ensureUser(request, {
      username,
      fullName: 'Reset Cancel User',
      role: 'USER',
    });

    await openUsersPage(page);
    const row = userRow(page, username);
    await row.getByRole('button', { name: 'Reset password' }).click();

    const dialog = page.getByRole('dialog', { name: 'Reset password' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(`assigned to ${username}`)).toBeVisible();
    await expect(dialog.getByText('They will be required to change it on next login.')).toBeVisible();
    await dialog.getByRole('button', { name: 'Cancel' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(row).toBeVisible();
  });

  test('resets a password, shows a copyable one-time temporary password, and requires a password change on next login', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('RESET_OK');
    await ensureUser(request, {
      username,
      fullName: 'Reset Success User',
      role: 'USER',
    });

    await openUsersPage(page);
    const row = userRow(page, username);
    await row.getByRole('button', { name: 'Reset password' }).click();

    let dialog = page.getByRole('dialog', { name: 'Reset password' });
    await dialog.getByRole('button', { name: 'Reset password' }).click();

    dialog = page.getByRole('dialog', { name: 'Reset password' });
    await expect(dialog.getByText(`Password for ${username} has been reset.`)).toBeVisible();

    const temporaryPassword = await dialog.getByRole('textbox').inputValue();
    await expect(temporaryPassword).not.toEqual('');
    await expect(dialog.getByRole('button', { name: 'Copy password' })).toBeVisible();
    await expect(
      dialog.getByText(/This password will not be shown again after closing\./),
    ).toBeVisible();
    await dialog.getByRole('button', { name: 'Done' }).click();

    await signOutAdmin(page);
    await login(page, username, temporaryPassword);
    await expectForcedPasswordChangePage(page);

    await changePassword(page, temporaryPassword, RESET_USER_NEW_PASSWORD);
    await login(page, username, RESET_USER_NEW_PASSWORD);
    await expectHomePage(page);
  });

  test('opens and cancels the delete dialog without removing the user', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('DELETE_CANCEL');
    await ensureUser(request, {
      username,
      fullName: 'Delete Cancel User',
      role: 'USER',
    });

    await openUsersPage(page);
    const row = userRow(page, username);
    await row.getByRole('button', { name: 'Delete user' }).click();

    const dialog = page.getByRole('dialog', { name: 'Delete user' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(`delete ${username}?`)).toBeVisible();
    await expect(dialog.getByText('Their account will be permanently removed.')).toBeVisible();
    await expect(dialog.getByText('This action cannot be undone.')).toBeVisible();
    await dialog.getByRole('button', { name: 'Cancel' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(row).toBeVisible();
  });

  test('deletes a removable user and updates the visible users count', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('DELETE_OK');
    await ensureUser(request, {
      username,
      fullName: 'Delete Success User',
      role: 'USER',
    });

    const totalBeforeDelete = (await listUsers(request)).length;

    await openUsersPage(page);
    const row = userRow(page, username);
    await row.getByRole('button', { name: 'Delete user' }).click();

    const dialog = page.getByRole('dialog', { name: 'Delete user' });
    await dialog.getByRole('button', { name: 'Delete user' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(userRow(page, username)).toHaveCount(0);
    await expectUsersSummary(page, totalBeforeDelete - 1);
  });
});