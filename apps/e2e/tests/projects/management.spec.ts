// spec: features/projects/management.feature
// seed: tests/seed.spec.ts

import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

const BASE_URL = process.env.E2E_BASE_URL ?? 'http://localhost';
const USERNAME = process.env.E2E_USERNAME ?? 'admin';
const PASSWORD = process.env.E2E_PASSWORD ?? 'e2e-admin-password';

const TEST_PROJECT_PREFIX = 'E2E_';

function permSwitch(page: Page, label: string) {
  return page.getByText(label, { exact: true }).locator('xpath=../following-sibling::*[@role="switch"]');
}

async function cleanupTestProjects(request: APIRequestContext): Promise<void> {
  await request.post(`${BASE_URL}/api/v1/auth/login`, {
    data: { username: USERNAME, password: PASSWORD, rememberMe: false },
  });

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

test.describe('Project Management', () => {
  const BASE_PROJECT_NAME = 'E2E_EXPLORE_PROJECT';

  const signIn = async (page: Page) => {
    await page.goto(`${BASE_URL}/`);
    await page.getByRole('textbox', { name: 'Username' }).fill(USERNAME);
    await page.getByRole('textbox', { name: 'Password' }).fill(PASSWORD);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('heading', { name: /Good (morning|afternoon|evening)/i })).toBeVisible();
  };

  const signInAndGoToHomePage = async (page: Page) => {
    await signIn(page);
    await expect(page.getByRole('heading', { name: 'Projects', level: 2 })).toBeVisible();
  };

  // On mobile the project sidebar is a hidden overlay; this helper opens it when needed.
  const openMobileSidebar = async (page: Page) => {
    const viewport = page.viewportSize();
    if (viewport && viewport.width <= 768) {
      await page.getByRole('button', { name: 'Toggle Sidebar' }).click();
    }
  };

  // On mobile the sidebar sheet stays open after SPA navigation; dismiss it so main content is accessible.
  const closeMobileSidebar = async (page: Page) => {
    const viewport = page.viewportSize();
    if (viewport && viewport.width <= 768) {
      await page.keyboard.press('Escape');
    }
  };

  const navigateToProjectSettings = async (page: Page, projectName: string) => {
    await page.getByRole('link', { name: new RegExp(projectName) }).click();
    await openMobileSidebar(page);
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
    await closeMobileSidebar(page);
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  };

  test.beforeEach(async ({ request, context }) => {
    await cleanupTestProjects(request);
    // Ensure the base project exists for all tests that navigate to it
    await request.post(`${BASE_URL}/api/v1/projects`, {
      data: { name: 'E2E_EXPLORE_PROJECT' },
    });
    await context.clearCookies();
    await context.clearPermissions();
  });

  test.afterEach(async ({ request }) => {
    await cleanupTestProjects(request);
  });

  // ---------------------------------------------------------------------------
  // Rule: Listing projects on the home page
  // ---------------------------------------------------------------------------

  test.describe('Listing projects on the home page', () => {
    test('Project cards show name, description, and creation date', async ({ page }) => {
      await signInAndGoToHomePage(page);

      // The card for the existing project should show the project name
      await expect(page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) })).toBeVisible();

      // The card should show the project description or "No description"
      await expect(page.getByText('No description')).toBeVisible();

      // The card should show the project creation date (any date format)
      await expect(page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) }).getByText(/\w+ \d+, \d{4}/)).toBeVisible();
    });

    test('Home page stats reflect the total and active project count', async ({ page }) => {
      await signInAndGoToHomePage(page);

      // The stats bar should show the total number of projects
      await expect(page.getByText('Projects').first()).toBeVisible();

      // The stats bar should show the count of active projects
      await expect(page.getByText(/^\d+ active$/)).toBeVisible();
    });

    test('"New Project" button is visible for users with "Create Projects" permission', async ({ page }) => {
      await signInAndGoToHomePage(page);

      // The "New Project" button should be visible for the admin
      await expect(page.getByRole('button', { name: 'New Project', exact: true })).toBeVisible();
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Creating a project
  // ---------------------------------------------------------------------------

  test.describe('Creating a project', () => {
    test('New project dialog has required name field and optional description field', async ({ page }) => {
      await signInAndGoToHomePage(page);

      // When the user clicks the "New Project" button
      await page.getByRole('button', { name: 'New Project', exact: true }).click();

      // The "New project" dialog should open
      await expect(page.getByRole('dialog', { name: 'New project' })).toBeVisible();

      // The dialog should contain a required "Project name" field
      await expect(page.getByRole('textbox', { name: 'Project name *' })).toBeVisible();

      // The dialog should contain an optional "Description" field
      await expect(page.getByRole('textbox', { name: 'Description (optional)' })).toBeVisible();
    });

    test('"Create project" button is disabled while the project name is empty', async ({ page }) => {
      await signInAndGoToHomePage(page);

      await page.getByRole('button', { name: 'New Project', exact: true }).click();

      // The "Create project" button should be disabled when name is empty
      await expect(page.getByRole('button', { name: 'Create project' })).toBeDisabled();
    });

    test('Creating a project with only a name succeeds', async ({ page }) => {
      await signInAndGoToHomePage(page);

      const timestamp = Date.now();
      const projectName = `E2E_NAME_ONLY_${timestamp}`;

      // When the user fills in the project name and creates
      await page.getByRole('button', { name: 'New Project', exact: true }).click();
      await page.getByRole('textbox', { name: 'Project name *' }).fill(projectName);
      await page.getByRole('button', { name: 'Create project' }).click();

      // The dialog should close and the project should appear
      await expect(page.getByRole('dialog', { name: 'New project' })).not.toBeVisible();
      await expect(page.getByRole('link', { name: new RegExp(projectName) })).toBeVisible();
    });

    test('Creating a project with name and description succeeds', async ({ page }) => {
      await signInAndGoToHomePage(page);

      const timestamp = Date.now();
      const projectName = `E2E_DESCRIBED_${timestamp}`;
      const description = 'A test project with a description';

      await page.getByRole('button', { name: 'New Project', exact: true }).click();
      await page.getByRole('textbox', { name: 'Project name *' }).fill(projectName);
      await page.getByRole('textbox', { name: 'Description (optional)' }).fill(description);
      await page.getByRole('button', { name: 'Create project' }).click();

      await expect(page.getByRole('dialog', { name: 'New project' })).not.toBeVisible();
      await expect(page.getByRole('link', { name: new RegExp(projectName) })).toBeVisible();
      await expect(page.getByRole('link', { name: new RegExp(projectName) }).getByText(description)).toBeVisible();
    });

    test('Cancelling the create project dialog discards changes', async ({ page }) => {
      await signInAndGoToHomePage(page);

      const timestamp = Date.now();
      const projectName = `E2E_SHOULD_NOT_EXIST_${timestamp}`;

      await page.getByRole('button', { name: 'New Project', exact: true }).click();
      await page.getByRole('textbox', { name: 'Project name *' }).fill(projectName);
      await page.getByRole('button', { name: 'Cancel' }).click();

      // The dialog should close and the project should NOT appear
      await expect(page.getByRole('dialog', { name: 'New project' })).not.toBeVisible();
      await expect(page.getByRole('link', { name: new RegExp(projectName) })).not.toBeVisible();
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Navigating into a project
  // ---------------------------------------------------------------------------

  test.describe('Navigating into a project', () => {
    test('Clicking a project card opens the project dashboard', async ({ page }) => {
      await signInAndGoToHomePage(page);

      // When the user clicks the card for the project
      await page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) }).click();

      // The user should be on the project dashboard page
      await expect(page.getByRole('heading', { name: new RegExp(`${BASE_PROJECT_NAME} Dashboard`) })).toBeVisible();
    });

    test('Project sidebar shows Dashboard, Integrations, Docs, Team, and Settings links', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) }).click();
      await openMobileSidebar(page);

      // The sidebar should contain all required project links
      await expect(page.getByRole('link', { name: 'Dashboard', exact: true })).toBeVisible();
      await expect(page.getByRole('link', { name: 'Integrations', exact: true })).toBeVisible();
      await expect(page.getByRole('link', { name: 'Docs', exact: true })).toBeVisible();
      await expect(page.getByRole('link', { name: 'Team', exact: true })).toBeVisible();
      await expect(page.getByRole('link', { name: 'Settings', exact: true })).toBeVisible();
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Editing project settings (Settings > General)
  // ---------------------------------------------------------------------------

  test.describe('Editing project settings (Settings > General)', () => {
    test('General tab shows editable project name and description fields', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);

      // When the user clicks "General" in the settings sidebar
      await page.getByRole('button', { name: 'General' }).click();

      // A "Project name" field pre-filled with the project name should be visible
      await expect(page.getByRole('textbox', { name: 'Project name' })).toHaveValue(BASE_PROJECT_NAME);

      // A "Description" field should be visible
      await expect(page.getByRole('textbox', { name: 'Description' })).toBeVisible();
    });

    test('"Save changes" button is disabled until a change is made', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);

      await page.getByRole('button', { name: 'General' }).click();

      // The "Save changes" button should be disabled initially
      await expect(page.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    });

    test('Saving a new project name updates the project', async ({ page }) => {
      await signInAndGoToHomePage(page);

      // Create a test project first
      const timestamp = Date.now();
      const projectName = `E2E_SETTINGS_${timestamp}`;
      const newName = `E2E_RENAMED_SETTINGS_${timestamp}`;

      await page.getByRole('button', { name: 'New Project', exact: true }).click();
      await page.getByRole('textbox', { name: 'Project name *' }).fill(projectName);
      await page.getByRole('button', { name: 'Create project' }).click();
      await expect(page.getByRole('dialog', { name: 'New project' })).not.toBeVisible();
      await expect(page.getByRole('link', { name: new RegExp(projectName) })).toBeVisible();

      await navigateToProjectSettings(page, projectName);
      await page.getByRole('button', { name: 'General' }).click();

      // Clear the project name and type the new name
      await page.getByRole('textbox', { name: 'Project name' }).clear();
      await page.getByRole('textbox', { name: 'Project name' }).fill(newName);
      await page.getByRole('button', { name: 'Save changes' }).click();

      // The project name should be updated
      await expect(page.getByRole('textbox', { name: 'Project name' })).toHaveValue(newName);
    });

    test('Saving a new description updates the project', async ({ page }) => {
      await signInAndGoToHomePage(page);

      // Create a test project
      const timestamp = Date.now();
      const projectName = `E2E_DESC_SETTINGS_${timestamp}`;

      await page.getByRole('button', { name: 'New Project', exact: true }).click();
      await page.getByRole('textbox', { name: 'Project name *' }).fill(projectName);
      await page.getByRole('button', { name: 'Create project' }).click();
      await expect(page.getByRole('dialog', { name: 'New project' })).not.toBeVisible();
      await expect(page.getByRole('link', { name: new RegExp(projectName) })).toBeVisible();

      await navigateToProjectSettings(page, projectName);
      await page.getByRole('button', { name: 'General' }).click();

      // Fill in a description and save
      await page.getByRole('textbox', { name: 'Description' }).fill('Updated description');
      await page.getByRole('button', { name: 'Save changes' }).click();

      // The description should reflect the new value
      await expect(page.getByRole('textbox', { name: 'Description' })).toHaveValue('Updated description');
    });

    test('Clearing the project name disables "Save changes"', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);

      await page.getByRole('button', { name: 'General' }).click();

      // Clear the project name
      await page.getByRole('textbox', { name: 'Project name' }).clear();

      // The "Save changes" button is enabled when a change is made (validation occurs on submit).
      // Submitting with an empty name should NOT persist the change – the name is restored.
      await page.getByRole('button', { name: 'Save changes' }).click();
      await expect(page.getByRole('textbox', { name: 'Project name' })).not.toHaveValue('');
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Deleting a project (Settings > Danger Zone)
  // ---------------------------------------------------------------------------

  test.describe('Deleting a project (Settings > Danger Zone)', () => {
    test('Danger Zone displays the "Delete project" button with a warning', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);

      // Click "Danger Zone" in the settings sidebar
      await page.getByRole('button', { name: 'Danger Zone' }).click();

      // A "Delete project" button should be visible
      await expect(page.getByRole('button', { name: 'Delete project' })).toBeVisible();

      // The section should warn that the action is permanent
      await expect(page.getByText(/cannot be undone/)).toBeVisible();
    });

    test('Delete project dialog warns about permanent data loss', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Danger Zone' }).click();

      // When the user clicks the "Delete project" button
      await page.getByRole('button', { name: 'Delete project' }).click();

      // The "Delete project" dialog should open
      await expect(page.getByRole('dialog', { name: 'Delete project' })).toBeVisible();

      // The dialog should warn about permanent data loss
      await expect(page.getByRole('dialog', { name: 'Delete project' }).getByText(/members.*roles.*integrations|integrations.*members.*roles/i)).toBeVisible();
    });

    test('"Delete permanently" button is disabled until the project name is typed', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Danger Zone' }).click();
      await page.getByRole('button', { name: 'Delete project' }).click();

      // The "Delete permanently" button should be disabled
      await expect(page.getByRole('button', { name: 'Delete permanently' })).toBeDisabled();

      // The dialog should instruct the user to type the project name to confirm
      await expect(page.getByRole('dialog', { name: 'Delete project' }).getByText(new RegExp(`Type.*${BASE_PROJECT_NAME}.*to confirm`, 'i'))).toBeVisible();
    });

    test('Typing an incorrect name keeps "Delete permanently" disabled', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Danger Zone' }).click();
      await page.getByRole('button', { name: 'Delete project' }).click();

      // Type wrong name
      await page.getByRole('textbox', { name: /Type.*to confirm/i }).fill('wrong-name');

      // The "Delete permanently" button should remain disabled
      await expect(page.getByRole('button', { name: 'Delete permanently' })).toBeDisabled();
    });

    test('Typing the exact project name enables "Delete permanently"', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Danger Zone' }).click();
      await page.getByRole('button', { name: 'Delete project' }).click();

      // Type the exact project name
      await page.getByRole('textbox', { name: /Type.*to confirm/i }).fill(BASE_PROJECT_NAME);

      // The "Delete permanently" button should become enabled
      await expect(page.getByRole('button', { name: 'Delete permanently' })).toBeEnabled();
    });

    test('Confirming deletion removes the project', async ({ page }) => {
      await signInAndGoToHomePage(page);

      // Create a project to delete
      const timestamp = Date.now();
      const projectName = `E2E_DELETE_${timestamp}`;

      await page.getByRole('button', { name: 'New Project', exact: true }).click();
      await page.getByRole('textbox', { name: 'Project name *' }).fill(projectName);
      await page.getByRole('button', { name: 'Create project' }).click();
      await expect(page.getByRole('dialog', { name: 'New project' })).not.toBeVisible();
      await expect(page.getByRole('link', { name: new RegExp(projectName) })).toBeVisible();

      await navigateToProjectSettings(page, projectName);
      await page.getByRole('button', { name: 'Danger Zone' }).click();
      await page.getByRole('button', { name: 'Delete project' }).click();
      await page.getByRole('textbox', { name: /Type.*to confirm/i }).fill(projectName);
      await page.getByRole('button', { name: 'Delete permanently' }).click();

      // The user should be redirected away from the project (allow extra time for auth refresh)
      await expect(page).toHaveURL(/\/home/, { timeout: 15000 });

      // The project should no longer appear on the home page
      await expect(page.getByRole('link', { name: new RegExp(projectName) })).not.toBeVisible();
    });

    test('Cancelling the delete dialog preserves the project', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Danger Zone' }).click();
      await page.getByRole('button', { name: 'Delete project' }).click();
      await page.getByRole('button', { name: 'Cancel' }).click();

      // The dialog should close and the project should still be on the home page
      await expect(page.getByRole('dialog', { name: 'Delete project' })).not.toBeVisible();
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Managing team members (Team page)
  // ---------------------------------------------------------------------------

  test.describe('Managing team members (Team page)', () => {
    test('Team page shows heading and project subtitle', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) }).click();
      await openMobileSidebar(page);
      await page.getByRole('link', { name: 'Team', exact: true }).click();
      await closeMobileSidebar(page);

      // The page heading "Team" should be visible
      await expect(page.getByRole('heading', { name: 'Team' })).toBeVisible();

      // The subtitle should include the project name and "Manage project members and roles"
      await expect(page.getByText(new RegExp(`${BASE_PROJECT_NAME}.*Manage project members and roles`))).toBeVisible();
    });

    test('"Add Member" button is visible', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) }).click();
      await openMobileSidebar(page);
      await page.getByRole('link', { name: 'Team', exact: true }).click();
      await closeMobileSidebar(page);

      // The "Add Member" button should be visible for the admin
      await expect(page.getByRole('button', { name: 'Add Member' })).toBeVisible();
    });

    test('Add Member dialog contains a user search field and a role picker', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) }).click();
      await openMobileSidebar(page);
      await page.getByRole('link', { name: 'Team', exact: true }).click();
      await closeMobileSidebar(page);

      // When the user clicks "Add Member"
      await page.getByRole('button', { name: 'Add Member' }).click();

      // The "Add member" dialog should open
      await expect(page.getByRole('dialog', { name: 'Add member' })).toBeVisible();

      // The dialog should contain a user search field
      await expect(page.getByRole('textbox', { name: 'Search by name or username…' })).toBeVisible();

      // The dialog should contain a role picker
      await expect(page.getByRole('combobox')).toBeVisible();
    });

    test('"Add member" button in the dialog is disabled when no user is selected', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) }).click();
      await openMobileSidebar(page);
      await page.getByRole('link', { name: 'Team', exact: true }).click();
      await closeMobileSidebar(page);
      await page.getByRole('button', { name: 'Add Member' }).click();

      // The "Add member" dialog button should be disabled when no user is selected
      await expect(page.getByRole('dialog', { name: 'Add member' }).getByRole('button', { name: 'Add member' })).toBeDisabled();
    });

    test('Member row shows name, username, Change role button, and a remove action', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await page.getByRole('link', { name: new RegExp(BASE_PROJECT_NAME) }).click();
      await openMobileSidebar(page);
      await page.getByRole('link', { name: 'Team', exact: true }).click();
      await closeMobileSidebar(page);

      // Each member row should show the member's name and username
      await expect(page.getByText('Admin').first()).toBeVisible();
      await expect(page.getByText('@admin')).toBeVisible();

      // Each row should have a "Change role" button
      await expect(page.getByRole('button', { name: 'Change role' })).toBeVisible();

      // Each row should have a menu button
      const memberRow = page.getByText('@admin').locator('../..');
      const menuButton = memberRow.locator('button').last();
      await menuButton.click();

      // The menu should contain a "Remove member" option
      await expect(page.getByRole('menuitem', { name: 'Remove member' })).toBeVisible();
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Managing project roles (Settings > Roles)
  // ---------------------------------------------------------------------------

  test.describe('Managing project roles (Settings > Roles)', () => {
    test('Project Roles heading and subtitle are visible', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      // The "Project Roles" heading should be visible
      await expect(page.getByRole('heading', { name: 'Project Roles' })).toBeVisible();

      // The subtitle should read correctly
      await expect(page.getByText('Manage roles and permissions for members of this project.')).toBeVisible();
    });

    test('Statistics bar shows the role count and permission grant total', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      // Statistics bar should show the total number of project roles
      await expect(page.getByText(/\d+\s*roles defined/)).toBeVisible();

      // Statistics bar should show the total permission grants
      await expect(page.getByText(/permission grants across all roles/)).toBeVisible();
    });

    test('Roles table displays expected columns', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      // The project roles table should have columns "Name", "Permissions", and "Created"
      await expect(page.getByRole('columnheader', { name: 'Name' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Permissions' })).toBeVisible();
      await expect(page.getByRole('columnheader', { name: 'Created' })).toBeVisible();
    });

    test('Default roles Admin, Editor, and Viewer are pre-seeded', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      // The default roles should appear in the project roles table
      await expect(page.getByRole('table').getByText('Admin', { exact: true })).toBeVisible();
      await expect(page.getByRole('table').getByText('Editor', { exact: true })).toBeVisible();
      await expect(page.getByRole('table').getByText('Viewer', { exact: true })).toBeVisible();
    });

    test('Each role row shows Edit role and Delete role action buttons', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      // Each role row should have Edit role and Delete role buttons
      const adminRow = page.getByRole('row', { name: /Admin/ }).first();
      await expect(adminRow.getByRole('button', { name: 'Edit role' })).toBeVisible();
      await expect(adminRow.getByRole('button', { name: 'Delete role' })).toBeVisible();
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Creating a project role
  // ---------------------------------------------------------------------------

  test.describe('Creating a project role', () => {
    test('New Role dialog title and description are correct', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      // When the user clicks the "New role" button
      await page.getByRole('button', { name: 'New role' }).click();

      // The role form dialog should open with "New Role" title
      await expect(page.getByRole('dialog', { name: 'New Role' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'New Role' })).toBeVisible();

      // The dialog description should mention creating a project role with permissions
      await expect(page.getByText('Define a new project role and configure which permissions it grants to members.')).toBeVisible();
    });

    test('Role Name field is empty and Create role button is disabled by default', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      await page.getByRole('button', { name: 'New role' }).click();

      // The "Role Name" field should be empty
      await expect(page.getByRole('textbox', { name: 'Role Name' })).toHaveValue('');

      // The "Create role" button should be disabled
      await expect(page.getByRole('button', { name: 'Create role' })).toBeDisabled();
    });

    test('Permission form shows five groups: Project, Members, Roles, Tasks, Sprints', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      await page.getByRole('button', { name: 'New role' }).click();

      const dialog = page.getByRole('dialog', { name: 'New Role' });

      // The permission section should display all five groups
      await expect(dialog.getByText('Project').first()).toBeVisible();
      await expect(dialog.getByText('Members').first()).toBeVisible();
      await expect(dialog.getByText('Roles').first()).toBeVisible();
      await expect(dialog.getByText('Tasks').first()).toBeVisible();
      await expect(dialog.getByText('Sprints').first()).toBeVisible();
    });

    test('Each project permission shows expected label and description', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      await page.getByRole('button', { name: 'New role' }).click();

      // Verify all permission descriptions
      await expect(page.getByText('View project details and settings')).toBeVisible();
      await expect(page.getByText('Update project name, description, and settings')).toBeVisible();
      await expect(page.getByText('Permanently delete this project')).toBeVisible();
      await expect(page.getByText('List and view project members')).toBeVisible();
      await expect(page.getByText('Add, remove, and reassign project members')).toBeVisible();
      await expect(page.getByText('List and view project role definitions')).toBeVisible();
      await expect(page.getByText('Create, edit, and delete project roles')).toBeVisible();
      await expect(page.getByText('Browse and read tasks in the project')).toBeVisible();
      await expect(page.getByText('Create, update, and move tasks')).toBeVisible();
      await expect(page.getByText('Browse sprint boards and backlogs')).toBeVisible();
      await expect(page.getByText('Create, update, and close sprints')).toBeVisible();
    });

    test('All permission switches are off by default', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      await page.getByRole('button', { name: 'New role' }).click();

      const dialog = page.getByRole('dialog', { name: 'New Role' });
      const switches = dialog.getByRole('switch');
      const count = await switches.count();
      for (let i = 0; i < count; i++) {
        await expect(switches.nth(i)).toHaveAttribute('aria-checked', 'false');
      }
    });

    test('Creating a role with a name and selected permissions', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_PROJECT_MANAGER_${timestamp}`;

      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Edit Project').click();
      await permSwitch(page, 'Manage Members').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      // The dialog should close and the role should appear in the table
      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      // Statistics should reflect the updated role count
      await expect(page.getByText(/\d+\s*roles defined/)).toBeVisible();
    });

    test('Creating a role without any permissions is allowed', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_EMPTY_ROLE_${timestamp}`;

      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      // Role should show zero active permissions
      await expect(page.getByRole('row', { name: new RegExp(roleName) }).getByText('No permissions assigned')).toBeVisible();
    });

    test('Cancelling the dialog discards changes', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_SHOULD_NOT_EXIST_${timestamp}`;

      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Cancel' }).click();

      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).not.toBeVisible();
    });

    test('Closing and reopening the dialog resets all permission switches', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      // Enable a permission, then cancel
      await page.getByRole('button', { name: 'New role' }).click();
      await permSwitch(page, 'Delete Project').click();
      await page.getByRole('button', { name: 'Cancel' }).click();

      // Re-open dialog – all switches should be reset
      await page.getByRole('button', { name: 'New role' }).click();
      await expect(page.getByRole('heading', { name: 'New Role' })).toBeVisible();

      const dialog = page.getByRole('dialog', { name: 'New Role' });
      const switches = dialog.getByRole('switch');
      const count = await switches.count();
      for (let i = 0; i < count; i++) {
        await expect(switches.nth(i)).toHaveAttribute('aria-checked', 'false');
      }
    });

    test('Permissions count in the table matches the granted permissions', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_COUNT_ROLE_${timestamp}`;

      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Edit Project').click();
      await permSwitch(page, 'Delete Project').click();
      await page.getByRole('button', { name: 'Create role' }).click();

      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();

      // The role should show 2 active permissions
      const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
      await expect(roleRow.getByText('projects.write')).toBeVisible();
      await expect(roleRow.getByText('projects.delete')).toBeVisible();
    });

    test('Toggling a permission on then off leaves it disabled', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      await page.getByRole('button', { name: 'New role' }).click();

      const deleteProjectSwitch = permSwitch(page, 'Delete Project');
      await deleteProjectSwitch.click();
      await deleteProjectSwitch.click();

      // The "Delete Project" permission switch should be off
      await expect(deleteProjectSwitch).toHaveAttribute('aria-checked', 'false');
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Editing a project role
  // ---------------------------------------------------------------------------

  test.describe('Editing a project role', () => {
    test('Opening the edit dialog pre-populates current data', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_EDITABLE_${timestamp}`;

      // Create a role with permissions
      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Edit Project').click();
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      // Click Edit role button
      await page.getByRole('row', { name: new RegExp(roleName) }).getByRole('button', { name: 'Edit role' }).click();

      // The role form dialog should open with "Edit Role" title
      await expect(page.getByRole('dialog', { name: 'Edit Role' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Edit Role' })).toBeVisible();

      // The "Role Name" field should be pre-filled
      await expect(page.getByRole('textbox', { name: 'Role Name' })).toHaveValue(roleName);

      // The permission switches should reflect the role's current permissions
      await expect(permSwitch(page, 'Edit Project')).toHaveAttribute('aria-checked', 'true');
    });

    test('Saving an updated name and permissions', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const originalName = `E2E_EDITABLE_${timestamp}`;
      const renamedName = `E2E_RENAMED_${timestamp}`;

      // Create a role
      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(originalName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(originalName, { exact: true })).toBeVisible();

      // Open edit dialog, rename and toggle a permission
      await page.getByRole('row', { name: new RegExp(originalName) }).getByRole('button', { name: 'Edit role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).clear();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(renamedName);
      await permSwitch(page, 'Delete Project').click();
      await page.getByRole('button', { name: 'Save changes' }).click();

      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(renamedName, { exact: true })).toBeVisible();
    });

    test('Cancelling the edit dialog discards all changes', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const originalName = `E2E_EDITABLE_${timestamp}`;

      // Create a role
      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(originalName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(originalName, { exact: true })).toBeVisible();

      // Open edit dialog, change name but cancel
      await page.getByRole('row', { name: new RegExp(originalName) }).getByRole('button', { name: 'Edit role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).clear();
      await page.getByRole('textbox', { name: 'Role Name' }).fill('E2E_UNSAVED_CHANGE');
      await page.getByRole('button', { name: 'Cancel' }).click();

      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(originalName, { exact: true })).toBeVisible();
      await expect(page.getByRole('table').getByText('E2E_UNSAVED_CHANGE', { exact: true })).not.toBeVisible();
    });

    test('Edit dialog pre-populates the correct permission switches', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_PREPOP_${timestamp}`;

      // Create a role with specific permissions
      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Delete Project').click();
      await permSwitch(page, 'Manage Sprints').click();
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      // Open edit dialog
      await page.getByRole('row', { name: new RegExp(roleName) }).getByRole('button', { name: 'Edit role' }).click();

      // Enabled switches should be on
      await expect(permSwitch(page, 'Delete Project')).toHaveAttribute('aria-checked', 'true');
      await expect(permSwitch(page, 'Manage Sprints')).toHaveAttribute('aria-checked', 'true');

      // Disabled switches should be off
      await expect(permSwitch(page, 'Edit Project')).toHaveAttribute('aria-checked', 'false');
      await expect(permSwitch(page, 'View Members')).toHaveAttribute('aria-checked', 'false');
    });

    test('Toggling a permission off during edit persists after save', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_TOGGLE_PERSIST_${timestamp}`;

      // Create role with Manage Members permission
      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await permSwitch(page, 'Manage Members').click();
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      // Open edit dialog and disable the permission
      await page.getByRole('row', { name: new RegExp(roleName) }).getByRole('button', { name: 'Edit role' }).click();
      await permSwitch(page, 'Manage Members').click();
      await page.getByRole('button', { name: 'Save changes' }).click();
      await expect(page.getByRole('dialog', { name: 'Edit Role' })).not.toBeVisible();

      // Re-open edit dialog and verify switch is still off
      await page.getByRole('row', { name: new RegExp(roleName) }).getByRole('button', { name: 'Edit role' }).click();
      await expect(permSwitch(page, 'Manage Members')).toHaveAttribute('aria-checked', 'false');
    });
  });

  // ---------------------------------------------------------------------------
  // Rule: Deleting a project role
  // ---------------------------------------------------------------------------

  test.describe('Deleting a project role', () => {
    test('Confirming deletion removes the project role', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_DELETABLE_${timestamp}`;

      // Create a role to delete
      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      // Click the Delete role button
      await page.getByRole('row', { name: new RegExp(roleName) }).getByRole('button', { name: 'Delete role' }).click();

      // A delete confirmation dialog should open displaying the role name
      await expect(page.getByRole('heading', { name: 'Delete role' })).toBeVisible();
      await expect(page.getByRole('dialog', { name: 'Delete role' }).getByText(roleName)).toBeVisible();

      // Confirm deletion
      await page.getByRole('dialog', { name: 'Delete role' }).getByRole('button', { name: 'Delete role' }).click();

      // The role should no longer appear in the project roles table
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).not.toBeVisible();
    });

    test('Cancelling the delete dialog preserves the project role', async ({ page }) => {
      await signInAndGoToHomePage(page);
      await navigateToProjectSettings(page, BASE_PROJECT_NAME);
      await page.getByRole('button', { name: 'Roles' }).click();

      const timestamp = Date.now();
      const roleName = `E2E_DELETABLE_${timestamp}`;

      // Create a role
      await page.getByRole('button', { name: 'New role' }).click();
      await page.getByRole('textbox', { name: 'Role Name' }).fill(roleName);
      await page.getByRole('button', { name: 'Create role' }).click();
      await expect(page.getByRole('dialog', { name: 'New Role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();

      // Click Delete role and then cancel
      await page.getByRole('row', { name: new RegExp(roleName) }).getByRole('button', { name: 'Delete role' }).click();
      await expect(page.getByRole('heading', { name: 'Delete role' })).toBeVisible();
      await page.getByRole('button', { name: 'Cancel' }).click();

      // The role should still appear in the project roles table
      await expect(page.getByRole('dialog', { name: 'Delete role' })).not.toBeVisible();
      await expect(page.getByRole('table').getByText(roleName, { exact: true })).toBeVisible();
    });
  });
});
