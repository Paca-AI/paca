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
import {
  cleanupGlobalRolesByPrefix,
  createUserWithGlobalPermissions,
  RESTRICTED_PASSWORD,
  signIn,
} from '../helpers/e2e-api';

const AUTH_URL = `${process.env.E2E_BASE_URL ?? 'http://localhost'}/api/v1/auth/login`;
const USERS_URL = `${process.env.E2E_BASE_URL ?? 'http://localhost'}/api/v1/admin/users`;
const GLOBAL_ROLES_URL = `${process.env.E2E_BASE_URL ?? 'http://localhost'}/api/v1/admin/global-roles`;
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

// The API assigns a global role by id, on its own endpoint (global_roles.assign):
// creating or editing a user never carries one, so a role is set in a second call.
async function assignRole(request: APIRequestContext, userId: string, roleName: string) {
  const list = await request.get(GLOBAL_ROLES_URL);
  expect(list.ok()).toBeTruthy();
  const roles: Array<{ id: string; name: string }> = (await list.json()).data ?? [];
  const role = roles.find((candidate) => candidate.name === roleName);
  expect(role, `global role ${roleName} exists`).toBeTruthy();

  const assigned = await request.put(`${USERS_URL}/${userId}/global-roles`, {
    data: { role_ids: [role?.id] },
  });
  expect(assigned.ok()).toBeTruthy();
}

async function createUser(
  request: APIRequestContext,
  user: { username: string; fullName: string; role?: UserRole },
) {
  const response = await request.post(USERS_URL, {
    data: {
      username: user.username,
      full_name: user.fullName,
      password: TEMP_PASSWORD,
    },
  });

  expect(response.ok()).toBeTruthy();

  // New accounts start as USER; anything else is a separate assignment.
  if (user.role && user.role !== 'USER') {
    const created = (await response.json()).data as { id: string };
    await assignRole(request, created.id, user.role);
  }
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

// The users table paginates at 20 rows per page (see `pageSize` in
// apps/web/src/routes/_authenticated/admin/users/index.tsx), so only the
// first page of rows is ever rendered — cap the expected row count there.
const USERS_PAGE_SIZE = 20;

// Other workers create and delete users at the same time, so the total is read
// from the page's own "N users in system" summary and checked against the rows
// rendered, instead of being compared with a count taken from the API earlier.
async function expectUsersSummary(page: Page) {
  const summary = page.getByText(/users? in system/i);
  await expect(summary).toBeVisible();
  await expect
    .poll(async () => {
      const total = Number((await summary.innerText()).match(/\d+/)?.[0]);
      const rows = await page.getByRole('row').count();
      return Number.isFinite(total) && total > 0 && rows === Math.min(total, USERS_PAGE_SIZE) + 1;
    })
    .toBe(true);
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
  }) => {
    await openUsersPage(page);

    await expect(page.getByText('View and manage user accounts and their assigned roles.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'New User' })).toBeVisible();
    await expectUsersSummary(page);

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

  test('opens the create-user wizard on its details step and validates before creating', async ({
    page,
  }) => {
    await openUsersPage(page);

    await page.getByRole('button', { name: 'New User' }).click();

    const dialog = page.getByRole('dialog', { name: 'Create User' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText('1 / 3')).toBeVisible();
    await expect(
      dialog.getByText('A secure temporary password will be generated automatically.'),
    ).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: 'Username' })).toBeVisible();
    await expect(dialog.getByRole('textbox', { name: 'Full Name' })).toBeVisible();
    // A role is a step of its own, not a field of the details form: this step
    // is the one that creates the account.
    await expect(dialog.getByRole('combobox')).toHaveCount(0);
    await expect(dialog.getByRole('button', { name: 'Continue' })).toHaveCount(0);

    await dialog.getByRole('button', { name: 'Create user' }).click();
    await expect(dialog.getByText('Full name is required.')).toBeVisible();

    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Jane Doe');
    await dialog.getByRole('button', { name: 'Create user' }).click();
    await expect(dialog.getByText('Username is required.')).toBeVisible();
    await expect(dialog.getByText('1 / 3')).toBeVisible();
  });

  test('creates the account first, then offers its roles on the second step with the default one held', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('ROLE_STEP');

    await openUsersPage(page);
    await page.getByRole('button', { name: 'New User' }).click();
    const dialog = page.getByRole('dialog', { name: 'Create User' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(username);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Role Step User');
    await dialog.getByRole('button', { name: 'Create user' }).click();

    // The account exists now, holding the default role: choosing another one is
    // a request of its own.
    await expect(dialog.getByText('2 / 3')).toBeVisible();
    await expect(dialog.getByText(`${username} was created with the USER role.`)).toBeVisible();
    const roles = dialog.getByRole('radiogroup', { name: 'Role' });
    await expect(roles.getByRole('radio', { name: 'ADMIN', exact: true })).toBeVisible();
    await expect(roles.getByRole('radio', { name: 'SUPER_ADMIN', exact: true })).toBeVisible();
    const defaultRole = roles.getByRole('radio', { name: 'USER', exact: true });
    await expect(defaultRole).toBeChecked();
    await expect(defaultRole).toHaveAccessibleDescription(/Default/);
    await expect(defaultRole).toHaveAccessibleDescription(/Current/);

    // There is no way back from here, and the password waits for the last step.
    await expect(dialog.getByRole('button', { name: 'Back' })).toHaveCount(0);
    await expect(dialog.getByRole('button', { name: 'Cancel' })).toHaveCount(0);
    await expect(dialog.getByRole('textbox')).toHaveCount(0);

    const created = (await listUsers(request)).find((user) => user.username === username);
    expect(created?.role).toBe('USER');
  });

  test('closing the dialog on the role step carries on to the password instead of losing it', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('CLOSE_ROLE');

    await openUsersPage(page);
    await page.getByRole('button', { name: 'New User' }).click();
    const dialog = page.getByRole('dialog', { name: 'Create User' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(username);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Close Role User');
    await dialog.getByRole('button', { name: 'Create user' }).click();
    await expect(dialog.getByText('2 / 3')).toBeVisible();

    await dialog.getByRole('button', { name: 'Close' }).click();

    // The account exists and its password has not been shown yet, so closing
    // keeps the role as it is and moves on to the password.
    const successDialog = page.getByRole('dialog', { name: 'User created' });
    await expect(successDialog).toBeVisible();
    await expect(successDialog.getByText('3 / 3')).toBeVisible();
    await expect(successDialog.getByRole('textbox')).not.toHaveValue('');
    await successDialog.getByRole('button', { name: 'Done' }).click();

    await expect(userRow(page, username).getByText('USER', { exact: true })).toBeVisible();
    expect((await listUsers(request)).some((user) => user.username === username)).toBe(true);
  });

  test('creates a user with the default USER role and requires a password change on first login', async ({
    page,
  }) => {
    const username = uniqueUsername('DEFAULT');

    await openUsersPage(page);
    await page.getByRole('button', { name: 'New User' }).click();

    const dialog = page.getByRole('dialog', { name: 'Create User' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(username);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Default Role User');
    await dialog.getByRole('button', { name: 'Create user' }).click();
    // The role step keeps USER, the default, so nothing else is asked for.
    await dialog.getByRole('button', { name: 'Continue' }).click();

    const successDialog = page.getByRole('dialog', { name: 'User created' });
    await expect(successDialog).toBeVisible();
    await expect(successDialog.getByText('3 / 3')).toBeVisible();
    await expect(
      successDialog.getByText(/will be asked to change it on first login/i),
    ).toBeVisible();
    await expect(successDialog.getByText('USER', { exact: true })).toBeVisible();

    const temporaryPassword = await successDialog.getByRole('textbox').inputValue();
    await expect(temporaryPassword).not.toEqual('');
    await expect(successDialog.getByText('This password will not be shown again.')).toBeVisible();
    await successDialog.getByRole('button', { name: 'Done' }).click();

    const createdRow = userRow(page, username);
    await expect(createdRow).toBeVisible();
    await expect(createdRow.getByText('USER', { exact: true })).toBeVisible();
    await expect(createdRow.getByText(/pwd reset/i)).toBeVisible();
    await expectUsersSummary(page);

    await signOutAdmin(page);
    await login(page, username, temporaryPassword);
    await expectForcedPasswordChangePage(page);

    await changePassword(page, temporaryPassword, DEFAULT_ROLE_USER_NEW_PASSWORD);
    await login(page, username, DEFAULT_ROLE_USER_NEW_PASSWORD);
    await expectHomePage(page);
  });

  test('creates a user, assigns the selected role through its own step, shows the password last, and cancels a discarded draft', async ({
    page,
  }) => {
    const selectedRoleUser = uniqueUsername('ADMIN');
    const cancelledUser = uniqueUsername('CANCELLED');

    await openUsersPage(page);

    await page.getByRole('button', { name: 'New User' }).click();
    let dialog = page.getByRole('dialog', { name: 'Create User' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(selectedRoleUser);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Admin Role User');
    await dialog.getByRole('button', { name: 'Create user' }).click();
    await dialog.getByRole('radio', { name: 'ADMIN', exact: true }).check();
    await dialog.getByRole('button', { name: 'Assign role' }).click();

    // The role is assigned; the account's one-time password comes last.
    const successDialog = page.getByRole('dialog', { name: 'User created' });
    await expect(successDialog).toBeVisible();
    await expect(successDialog.getByText('3 / 3')).toBeVisible();
    await expect(successDialog.getByText('ADMIN', { exact: true })).toBeVisible();
    await expect(successDialog.getByRole('textbox')).not.toHaveValue('');
    await successDialog.getByRole('button', { name: 'Done' }).click();

    await expect(userRow(page, selectedRoleUser).getByText('ADMIN', { exact: true })).toBeVisible();

    await page.getByRole('button', { name: 'New User' }).click();
    dialog = page.getByRole('dialog', { name: 'Create User' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill(cancelledUser);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Cancelled User');
    await dialog.getByRole('button', { name: 'Cancel' }).click();

    await expect(userRow(page, cancelledUser)).toHaveCount(0);
  });

  test('opens the edit dialog with current values and saves the updated full name only', async ({
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
    // Editing is the profile: no role field, and not a multi-step wizard.
    await expect(dialog.getByRole('combobox')).toHaveCount(0);
    await expect(dialog.getByRole('radiogroup')).toHaveCount(0);
    await expect(dialog.getByRole('button', { name: 'Continue' })).toHaveCount(0);

    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Edited User Name');
    await dialog.getByRole('button', { name: 'Save changes' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(row.getByText('Edited User Name', { exact: true })).toBeVisible();
    await expect(row.getByText('USER', { exact: true })).toBeVisible();
  });

  test("changes a user's role from the users table, in a dialog of its own", async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('CHANGE_ROLE');
    await ensureUser(request, { username, fullName: 'Change Role User', role: 'USER' });

    await openUsersPage(page);
    const row = userRow(page, username);
    // The role itself is the button: it can be changed without opening the profile.
    await row.getByRole('button', { name: 'USER', exact: true }).click();

    const dialog = page.getByRole('dialog', { name: 'Change role' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(`Choose a role for ${username}.`)).toBeVisible();
    const current = dialog.getByRole('radio', { name: 'USER', exact: true });
    await expect(current).toBeChecked();
    await expect(current).toHaveAccessibleDescription(/Current/);
    // Nothing to assign until a different role is picked.
    await expect(dialog.getByRole('button', { name: 'Assign role' })).toBeDisabled();

    await dialog.getByRole('radio', { name: 'ADMIN', exact: true }).check();
    await dialog.getByRole('button', { name: 'Assign role' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(row.getByRole('button', { name: 'ADMIN', exact: true })).toBeVisible();
    const saved = (await listUsers(request)).find((user) => user.username === username);
    expect(saved?.role).toBe('ADMIN');
  });

  test('flags a full-access role before it is assigned, and cancelling changes nothing', async ({
    page,
    request,
  }) => {
    const username = uniqueUsername('FULL_ACCESS');
    await ensureUser(request, { username, fullName: 'Full Access User', role: 'USER' });

    await openUsersPage(page);
    const row = userRow(page, username);
    await row.getByRole('button', { name: 'USER', exact: true }).click();

    const dialog = page.getByRole('dialog', { name: 'Change role' });
    await expect(dialog.getByText(/This role has full access/)).toHaveCount(0);
    await dialog.getByRole('radio', { name: 'SUPER_ADMIN', exact: true }).check();
    await expect(dialog.getByText(/This role has full access/)).toBeVisible();

    await dialog.getByRole('button', { name: 'Cancel' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(row.getByRole('button', { name: 'USER', exact: true })).toBeVisible();
    const saved = (await listUsers(request)).find((user) => user.username === username);
    expect(saved?.role).toBe('USER');
  });

  test('warns the signed-in administrator that changing their own role can lock them out', async ({
    page,
  }) => {
    await openUsersPage(page);
    const adminRow = currentAdminRow(page);
    await adminRow.getByRole('button', { name: 'SUPER_ADMIN', exact: true }).click();

    const dialog = page.getByRole('dialog', { name: 'Change role' });
    await expect(dialog.getByText(/This is your own account/)).toHaveCount(0);
    await dialog.getByRole('radio', { name: 'USER', exact: true }).check();
    await expect(dialog.getByText(/This is your own account/)).toBeVisible();

    // Never applied: cancel, and the administrator keeps full access.
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toHaveCount(0);
    await expect(adminRow.getByText('SUPER_ADMIN', { exact: true })).toBeVisible();
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

    await openUsersPage(page);
    const row = userRow(page, username);
    await row.getByRole('button', { name: 'Delete user' }).click();

    const dialog = page.getByRole('dialog', { name: 'Delete user' });
    await dialog.getByRole('button', { name: 'Delete user' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(userRow(page, username)).toHaveCount(0);
    await expectUsersSummary(page);
  });
});

// ===========================================================================
// Rule: Role assignment is a separate permission from managing users
// ===========================================================================

test.describe('Role assignment is a separate permission from managing users', () => {
  // Each scenario signs in as its own restricted account, so no shared session.
  test.beforeEach(async ({ request, context }) => {
    await authenticateAdmin(request);
    await cleanupTestUsers(request);
    await cleanupGlobalRolesByPrefix(request, TEST_USER_PREFIX);
    await context.clearCookies();
  });

  test.afterEach(async ({ request }) => {
    await authenticateAdmin(request);
    await cleanupTestUsers(request);
    await cleanupGlobalRolesByPrefix(request, TEST_USER_PREFIX);
  });

  test('writing users without assigning roles gives a two-step wizard and a plain role label', async ({
    page,
    request,
    playwright,
  }) => {
    const writer = uniqueUsername('WRITER');
    const roleName = `${TEST_USER_PREFIX}ROLE_WRITER_${TEST_RUN_ID}`;
    await createUserWithGlobalPermissions(request, playwright, {
      username: writer,
      roleName,
      // users.read shows the page and users.write creates; nothing for roles.
      permissions: { 'projects.read': true, 'users.read': true, 'users.write': true },
    });
    const created = uniqueUsername('BY_WRITER');

    await signIn(page, writer, RESTRICTED_PASSWORD);
    await page.goto('/admin/users');
    await expect(page.getByRole('heading', { name: 'User Management' })).toBeVisible();

    // No permission to change a role, so the role is plain text.
    const ownRow = userRow(page, writer);
    await expect(ownRow.getByText(roleName, { exact: true })).toBeVisible();
    await expect(ownRow.getByRole('button', { name: roleName, exact: true })).toHaveCount(0);

    await page.getByRole('button', { name: 'New User' }).click();
    const dialog = page.getByRole('dialog', { name: 'Create User' });
    await expect(dialog.getByText('1 / 2')).toBeVisible();
    await dialog.getByRole('textbox', { name: 'Username' }).fill(created);
    await dialog.getByRole('textbox', { name: 'Full Name' }).fill('Created By Writer');
    await expect(dialog.getByRole('button', { name: 'Continue' })).toHaveCount(0);
    await dialog.getByRole('button', { name: 'Create user' }).click();

    const successDialog = page.getByRole('dialog', { name: 'User created' });
    await expect(successDialog.getByText('2 / 2')).toBeVisible();
    await expect(successDialog.getByRole('textbox')).not.toHaveValue('');
    // There was no role step, but the role the account got is still confirmed.
    await expect(successDialog.getByText('USER', { exact: true })).toBeVisible();
    await successDialog.getByRole('button', { name: 'Done' }).click();
  });

  test('assigning roles without writing users offers the role button but no user editing', async ({
    page,
    request,
    playwright,
  }) => {
    const assigner = uniqueUsername('ASSIGNER');
    const target = uniqueUsername('TARGET');
    await createUserWithGlobalPermissions(request, playwright, {
      username: assigner,
      roleName: `${TEST_USER_PREFIX}ROLE_ASSIGNER_${TEST_RUN_ID}`,
      permissions: {
        'projects.read': true,
        'users.read': true,
        'global_roles.read': true,
        'global_roles.assign': true,
      },
    });
    await ensureUser(request, { username: target, fullName: 'Target User', role: 'USER' });

    await signIn(page, assigner, RESTRICTED_PASSWORD);
    await page.goto('/admin/users');
    await expect(page.getByRole('heading', { name: 'User Management' })).toBeVisible();

    // Writing users is a different permission: no creating, editing or resetting.
    await expect(page.getByRole('button', { name: 'New User' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Edit user' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Reset password' })).toHaveCount(0);

    // Roles can still be changed.
    const row = userRow(page, target);
    await row.getByRole('button', { name: 'USER', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Change role' });
    await dialog.getByRole('radio', { name: 'ADMIN', exact: true }).check();
    await dialog.getByRole('button', { name: 'Assign role' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(row.getByRole('button', { name: 'ADMIN', exact: true })).toBeVisible();
  });
});

// ===========================================================================
// Rule: Deleting a user is a separate permission from managing users
// ===========================================================================
// Regression coverage: the Delete action used to be shown to anyone who could
// edit a user (users.write), regardless of users.delete. Mirrors the "Role
// assignment..." describe block above for both structure and cleanup.

test.describe('Deleting a user is a separate permission from managing users', () => {
  test.beforeEach(async ({ request, context }) => {
    await authenticateAdmin(request);
    await cleanupTestUsers(request);
    await cleanupGlobalRolesByPrefix(request, TEST_USER_PREFIX);
    await context.clearCookies();
  });

  test.afterEach(async ({ request }) => {
    await authenticateAdmin(request);
    await cleanupTestUsers(request);
    await cleanupGlobalRolesByPrefix(request, TEST_USER_PREFIX);
  });

  test('writing users without deleting them hides Delete everywhere, but keeps editing and resetting', async ({
    page,
    request,
    playwright,
  }) => {
    const writer = uniqueUsername('NODELETE');
    await createUserWithGlobalPermissions(request, playwright, {
      username: writer,
      roleName: `${TEST_USER_PREFIX}ROLE_NODELETE_${TEST_RUN_ID}`,
      // No users.delete: this account can manage users but not remove them.
      permissions: { 'projects.read': true, 'users.read': true, 'users.write': true },
    });
    const target = uniqueUsername('NODELETE_TARGET');
    await ensureUser(request, { username: target, fullName: 'No Delete Target', role: 'USER' });

    await signIn(page, writer, RESTRICTED_PASSWORD);
    await page.goto('/admin/users');
    await expect(page.getByRole('heading', { name: 'User Management' })).toBeVisible();

    for (const row of [userRow(page, writer), userRow(page, target)]) {
      await expect(row.getByRole('button', { name: 'Edit user' })).toBeVisible();
      await expect(row.getByRole('button', { name: 'Reset password' })).toBeVisible();
      await expect(row.getByRole('button', { name: 'Delete user' })).toHaveCount(0);
    }
  });

  test('deleting users without writing them offers only Delete, and it actually removes the user', async ({
    page,
    request,
    playwright,
  }) => {
    const deleter = uniqueUsername('DELETEONLY');
    await createUserWithGlobalPermissions(request, playwright, {
      username: deleter,
      roleName: `${TEST_USER_PREFIX}ROLE_DELETEONLY_${TEST_RUN_ID}`,
      // No users.write: this account can remove users but not edit or create them.
      permissions: { 'projects.read': true, 'users.read': true, 'users.delete': true },
    });
    const target = uniqueUsername('DELETEONLY_TARGET');
    await ensureUser(request, { username: target, fullName: 'Delete Only Target', role: 'USER' });

    await signIn(page, deleter, RESTRICTED_PASSWORD);
    await page.goto('/admin/users');
    await expect(page.getByRole('heading', { name: 'User Management' })).toBeVisible();

    // Writing users is a different permission: no creating, editing or resetting.
    await expect(page.getByRole('button', { name: 'New User' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Edit user' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Reset password' })).toHaveCount(0);

    // Delete is still offered, and it is not a decoration: it works end to end.
    const row = userRow(page, target);
    await expect(row.getByRole('button', { name: 'Delete user' })).toBeVisible();
    await row.getByRole('button', { name: 'Delete user' }).click();
    const dialog = page.getByRole('dialog', { name: 'Delete user' });
    await dialog.getByRole('button', { name: 'Delete user' }).click();

    await expect(dialog).toHaveCount(0);
    await expect(userRow(page, target)).toHaveCount(0);
  });
});
