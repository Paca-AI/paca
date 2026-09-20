// spec: features/projects/environments.feature
// seed: tests/seed.spec.ts
//
// Creating an environment queues real provisioning (agent-runner launches a
// sandbox container in the background), and whether that ever succeeds
// depends on the stack's container runtime. The environment status therefore
// keeps moving (Creating -> Starting -> Running/Error), so nothing here
// asserts WHICH status a card shows, only that one of the known labels is
// rendered. Live terminal, SSH connect and port-forward tunnelling are out of
// scope for the same reason.
//
// Cleanup deletes every environment in the test projects (after letting
// in-flight provisioning settle) before deleting the projects themselves, so
// half-created sandbox containers are not leaked on the e2e stack.
//
// A11y gaps worked around here (apps/web has no accessible name for them): the
// detail page's overflow-actions trigger is an icon-only button, located as
// the sibling right after the "Connect" link; the Overview name input has no
// label association, located by its current value.

import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	test,
} from "@playwright/test";
import {
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import {
	cleanupEnvironmentsInProjectsByPrefix,
	createEnvironment,
	listEnvironments,
	type SeededEnvironment,
	setEnvironmentAccessMode,
	waitForSettledStatus,
} from "../helpers/environments";

const PREFIX = "E2E_ENV_";
const RUN_ID = newRunId();

const STATUS_LABEL =
	/^(Creating|Starting|Running|Stopping|Stopped|Suspended|Error|Deleting)$/;
const BACKEND_BADGE = /^(docker|kubernetes)$/;

async function cleanup(request: APIRequestContext) {
	await cleanupEnvironmentsInProjectsByPrefix(request, PREFIX);
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
}

const environmentsUrl = (projectId: string) =>
	`${BASE_URL}/projects/${projectId}/environments`;

const detailUrl = (projectId: string, environmentId: string) =>
	`${environmentsUrl(projectId)}/${environmentId}`;

// Names only contain [A-Z0-9_], so they are safe to drop into a RegExp.
const environmentCard = (page: Page, name: string): Locator =>
	page.getByRole("link", { name: new RegExp(name) });

const openCreateDialog = async (page: Page) => {
	await page.getByRole("button", { name: "New Environment" }).click();
	const dialog = page.getByRole("dialog", { name: "Create Environment" });
	await expect(dialog).toBeVisible();
	return dialog;
};

const fillEnvironmentName = async (dialog: Locator, name: string) => {
	await dialog.getByRole("textbox", { name: /^Name/ }).fill(name);
};

const expandAdvanced = async (dialog: Locator) => {
	await dialog.getByRole("button", { name: "Advanced" }).click();
};

const submitButton = (dialog: Locator) =>
	dialog.getByRole("button", { name: "Create Environment", exact: true });

const openActionsMenu = async (page: Page) => {
	// The icon-only trigger sits right after the "Connect" link in the header.
	await page
		.getByRole("link", { name: "Connect", exact: true })
		.locator("xpath=following-sibling::button")
		.click();
};

// ===========================================================================
// Rule: Environments page — empty state and permission gating
// ===========================================================================

test.describe("Environments page", () => {
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		await cleanup(request);
		projectId = await createProject(request, `${PREFIX}PROJECT_${RUN_ID}`);
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("A project with no environments shows an empty state", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(environmentsUrl(projectId));

		await expect(
			page.getByRole("heading", { name: "Environments", exact: true }),
		).toBeVisible();
		await expect(page.getByText("No environments yet")).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Create your first environment" }),
		).toBeVisible();
	});

	test('The "New Environment" button is visible with environments.write permission', async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PREFIX}WRITER_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PREFIX}WRITE_ROLE_${RUN_ID}`,
			permissions: { "environments.read": true, "environments.write": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(environmentsUrl(projectId));

		await expect(
			page.getByRole("button", { name: "New Environment" }),
		).toBeVisible();
	});

	test('The "New Environment" button is hidden without environments.write permission', async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PREFIX}READER_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PREFIX}READ_ROLE_${RUN_ID}`,
			permissions: { "environments.read": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(environmentsUrl(projectId));

		await expect(page.getByText("No environments yet")).toBeVisible();
		await expect(
			page.getByRole("button", { name: "New Environment" }),
		).toHaveCount(0);
		await expect(
			page.getByRole("button", { name: "Create your first environment" }),
		).toHaveCount(0);
	});

	test("A member without environments.read permission sees the no-permission state", async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PREFIX}NOREAD_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PREFIX}NOREAD_ROLE_${RUN_ID}`,
			permissions: { "tasks.read": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(environmentsUrl(projectId));

		await expect(
			page.getByText("You don't have permission to view environments"),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "New Environment" }),
		).toHaveCount(0);
	});

	test("Opening the page with create=true opens the create dialog", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(`${environmentsUrl(projectId)}?create=true`);

		const dialog = page.getByRole("dialog", { name: "Create Environment" });
		await expect(dialog).toBeVisible();

		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page).not.toHaveURL(/create=true/);
	});
});

// ===========================================================================
// Rule: Creating an environment
// ===========================================================================

test.describe("Creating an environment", () => {
	let projectId: string;

	test.beforeEach(async ({ page, request }) => {
		await cleanup(request);
		projectId = await createProject(
			request,
			`${PREFIX}CREATE_PROJECT_${RUN_ID}`,
		);
		await signIn(page);
		await page.goto(environmentsUrl(projectId));
		await expect(page.getByText("No environments yet")).toBeVisible();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("The create dialog offers a name, a Docker access switch, and an Advanced section", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);

		await expect(dialog.getByRole("textbox", { name: /^Name/ })).toBeVisible();
		await expect(dialog.getByText("Docker access")).toBeVisible();
		await expect(dialog.getByRole("switch")).toBeVisible();
		await expect(
			dialog.getByRole("button", { name: "Advanced" }),
		).toBeVisible();
	});

	test('The "Create Environment" button is disabled until a name is entered', async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);

		await expect(submitButton(dialog)).toBeDisabled();

		await fillEnvironmentName(dialog, `${PREFIX}NEW_${RUN_ID}`);

		await expect(submitButton(dialog)).toBeEnabled();
	});

	test("The Advanced section reveals the image and resource limit fields", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);
		await expandAdvanced(dialog);

		await expect(dialog.getByRole("textbox", { name: "Image" })).toBeVisible();
		await expect(dialog.getByRole("textbox", { name: "CPU" })).toBeVisible();
		await expect(dialog.getByRole("textbox", { name: "Memory" })).toBeVisible();
		await expect(dialog.getByRole("spinbutton", { name: "Disk" })).toBeVisible();
		await expect(
			dialog.getByText(
				"Leave blank to use the platform defaults (2 vCPU, 4Gi memory, 20GB disk).",
			),
		).toBeVisible();
	});

	test("A CPU limit below the minimum is rejected before submitting", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);
		await fillEnvironmentName(dialog, `${PREFIX}BAD_CPU_${RUN_ID}`);
		await expandAdvanced(dialog);
		await dialog.getByRole("textbox", { name: "CPU" }).fill("0.01");

		await expect(
			dialog.getByText("CPU must be at least 0.1 (100m)."),
		).toBeVisible();
		await expect(submitButton(dialog)).toBeDisabled();
	});

	test("A memory limit below the minimum is rejected before submitting", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);
		await fillEnvironmentName(dialog, `${PREFIX}BAD_MEMORY_${RUN_ID}`);
		await expandAdvanced(dialog);
		await dialog.getByRole("textbox", { name: "Memory" }).fill("100Mi");

		await expect(
			dialog.getByText("Memory must be at least 256Mi."),
		).toBeVisible();
		await expect(submitButton(dialog)).toBeDisabled();
	});

	test("A disk limit that is not a positive whole number is rejected before submitting", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);
		await fillEnvironmentName(dialog, `${PREFIX}BAD_DISK_${RUN_ID}`);
		await expandAdvanced(dialog);
		await dialog.getByRole("spinbutton", { name: "Disk" }).fill("0");

		await expect(
			dialog.getByText("Disk must be a positive whole number of GB."),
		).toBeVisible();
		await expect(submitButton(dialog)).toBeDisabled();
	});

	test("Cancelling the create dialog discards the in-progress environment", async ({
		page,
		request,
	}) => {
		const name = `${PREFIX}CANCELLED_${RUN_ID}`;
		const dialog = await openCreateDialog(page);
		await fillEnvironmentName(dialog, name);
		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page.getByText(name)).toHaveCount(0);
		expect(await listEnvironments(request, projectId)).toHaveLength(0);
	});

	test("A newly created environment appears right away with a status and backend badge", async ({
		page,
		request,
	}) => {
		const name = `${PREFIX}CREATED_${RUN_ID}`;
		const dialog = await openCreateDialog(page);
		await fillEnvironmentName(dialog, name);
		await submitButton(dialog).click();

		await expect(dialog).not.toBeVisible();
		await expect(page).toHaveURL(
			new RegExp(`/projects/${projectId}/environments`),
		);

		const card = environmentCard(page, name);
		await expect(card).toBeVisible();

		const created = (await listEnvironments(request, projectId)).find(
			(e) => e.name === name,
		);
		expect(created).toBeDefined();
		await expect(
			card.getByText((created as SeededEnvironment).slug, { exact: true }),
		).toBeVisible();
		await expect(card.getByText(STATUS_LABEL)).toBeVisible();
		await expect(card.getByText(BACKEND_BADGE)).toBeVisible();
	});
});

// ===========================================================================
// Rule: Environment cards
// ===========================================================================

test.describe("Environment cards", () => {
	test.describe.configure({ timeout: 150_000 });

	let projectId: string;

	test.beforeEach(async ({ request }) => {
		await cleanup(request);
		projectId = await createProject(request, `${PREFIX}CARDS_PROJECT_${RUN_ID}`);
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("An environment card shows its name, slug, backend badge, and status", async ({
		page,
		request,
	}) => {
		const env = await createEnvironment(
			request,
			projectId,
			`${PREFIX}CARD_${RUN_ID}`,
		);

		await signIn(page);
		await page.goto(environmentsUrl(projectId));

		const card = environmentCard(page, env.name);
		await expect(card).toBeVisible();
		await expect(card.getByText(env.name, { exact: true })).toBeVisible();
		await expect(card.getByText(env.slug, { exact: true })).toBeVisible();
		await expect(card.getByText(BACKEND_BADGE)).toBeVisible();
		await expect(card.getByText(STATUS_LABEL)).toBeVisible();
	});

	test("Clicking an environment card opens its detail page", async ({
		page,
		request,
	}) => {
		const env = await createEnvironment(
			request,
			projectId,
			`${PREFIX}OPEN_CARD_${RUN_ID}`,
		);

		await signIn(page);
		await page.goto(environmentsUrl(projectId));
		await environmentCard(page, env.name).click();

		await expect(page).toHaveURL(detailUrl(projectId, env.id));
	});

	test('A restricted environment shows a "Restricted" badge to a member with no access grant', async ({
		page,
		request,
		playwright,
	}) => {
		const locked = await createEnvironment(
			request,
			projectId,
			`${PREFIX}LOCKED_${RUN_ID}`,
		);
		const unlocked = await createEnvironment(
			request,
			projectId,
			`${PREFIX}UNLOCKED_${RUN_ID}`,
		);
		// Let provisioning settle first so the worker's own status writes
		// can't race the access-mode update.
		await waitForSettledStatus(request, projectId, locked.id);
		await setEnvironmentAccessMode(request, projectId, locked.id, "restricted");

		const username = `${PREFIX}MEMBER_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PREFIX}MEMBER_ROLE_${RUN_ID}`,
			permissions: { "environments.read": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(environmentsUrl(projectId));

		await expect(
			environmentCard(page, locked.name).getByText("Restricted", {
				exact: true,
			}),
		).toBeVisible();
		await expect(environmentCard(page, unlocked.name)).toBeVisible();
		await expect(
			environmentCard(page, unlocked.name).getByText("Restricted", {
				exact: true,
			}),
		).toHaveCount(0);
	});
});

// ===========================================================================
// Rule: Environment detail — Overview tab
// ===========================================================================

test.describe("Environment detail Overview", () => {
	let projectId: string;
	let env: SeededEnvironment;

	test.beforeEach(async ({ request }) => {
		await cleanup(request);
		projectId = await createProject(
			request,
			`${PREFIX}DETAIL_PROJECT_${RUN_ID}`,
		);
		env = await createEnvironment(request, projectId, `${PREFIX}DETAIL_${RUN_ID}`);
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("The detail header shows the name, slug, status, and tab bar with Overview selected", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(detailUrl(projectId, env.id));

		await expect(page.getByRole("heading", { name: env.name })).toBeVisible();
		await expect(page.getByText(env.slug, { exact: true })).toBeVisible();
		await expect(page.getByText(STATUS_LABEL).first()).toBeVisible();

		for (const tab of ["Overview", "Folders", "Port forwards", "Access"]) {
			await expect(
				page.getByRole("button", { name: tab, exact: true }),
			).toBeVisible();
		}
		await expect(
			page.getByRole("link", { name: "Connect", exact: true }),
		).toBeVisible();
		// Overview is the default tab.
		await expect(page.getByText("Configuration", { exact: true })).toBeVisible();
	});

	test("The Overview tab shows usage vitals and the environment's configuration", async ({
		page,
	}) => {
		await signIn(page);
		await page.goto(detailUrl(projectId, env.id));

		for (const vital of ["CPU", "Memory", "Disk", "Last active"]) {
			await expect(page.getByText(vital, { exact: true })).toBeVisible();
		}
		await expect(page.getByText("Configuration", { exact: true })).toBeVisible();
		// The name input has no accessible label; locate it by its value.
		await expect(page.locator(`input[value="${env.name}"]`)).toBeVisible();
		await expect(page.getByText("Default (agent-server)")).toBeVisible();
		await expect(page.getByText("Disabled", { exact: true })).toBeVisible();
		await expect(page.getByRole("spinbutton")).toHaveValue("60");
	});

	test("Switching tabs updates the URL hash", async ({ page }) => {
		await signIn(page);
		await page.goto(detailUrl(projectId, env.id));

		await page.getByRole("button", { name: "Access", exact: true }).click();
		await expect(page).toHaveURL(/#access$/);

		await page
			.getByRole("button", { name: "Port forwards", exact: true })
			.click();
		await expect(page).toHaveURL(/#portForwards$/);
	});

	test("A member with environments.write can edit the configuration", async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PREFIX}EDITOR_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PREFIX}EDITOR_ROLE_${RUN_ID}`,
			permissions: { "environments.read": true, "environments.write": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(detailUrl(projectId, env.id));

		await expect(page.locator(`input[value="${env.name}"]`)).toBeEnabled();
		await expect(page.getByRole("spinbutton")).toBeEnabled();
		await expect(
			page.getByRole("button", { name: "Save changes" }),
		).toBeDisabled();
	});

	test("A member without environments.write sees a read-only Overview", async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PREFIX}VIEWER_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${PREFIX}VIEWER_ROLE_${RUN_ID}`,
			permissions: { "environments.read": true },
		});

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(detailUrl(projectId, env.id));

		await expect(page.locator(`input[value="${env.name}"]`)).toBeDisabled();
		await expect(page.getByRole("spinbutton")).toBeDisabled();
		await expect(page.getByRole("button", { name: "Save changes" })).toHaveCount(
			0,
		);
	});
});

// ===========================================================================
// Rule: Deleting an environment
// ===========================================================================

test.describe("Deleting an environment", () => {
	// Deleting waits for provisioning to settle first: once agent-runner has
	// attached a container, delete also has to remove it, and doing that while
	// the create command is still in flight is racy.
	test.describe.configure({ timeout: 150_000 });

	let projectId: string;
	let env: SeededEnvironment;

	test.beforeEach(async ({ page, request }) => {
		await cleanup(request);
		projectId = await createProject(
			request,
			`${PREFIX}DELETE_PROJECT_${RUN_ID}`,
		);
		env = await createEnvironment(
			request,
			projectId,
			`${PREFIX}TO_DELETE_${RUN_ID}`,
		);
		await waitForSettledStatus(request, projectId, env.id);

		await signIn(page);
		await page.goto(detailUrl(projectId, env.id));
		await expect(page.getByRole("heading", { name: env.name })).toBeVisible();
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("Cancelling the delete confirmation keeps the environment", async ({
		page,
		request,
	}) => {
		await openActionsMenu(page);
		await page.getByRole("menuitem", { name: "Delete" }).click();

		const dialog = page.getByRole("dialog", { name: `Delete ${env.name}?` });
		await expect(dialog).toBeVisible();
		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(page).toHaveURL(detailUrl(projectId, env.id));
		await expect(page.getByRole("heading", { name: env.name })).toBeVisible();
		const remaining = await listEnvironments(request, projectId);
		expect(remaining.map((e) => e.id)).toContain(env.id);
	});

	test("Confirming the deletion removes the environment and returns to the list", async ({
		page,
		request,
	}) => {
		await openActionsMenu(page);
		await page.getByRole("menuitem", { name: "Delete" }).click();

		const dialog = page.getByRole("dialog", { name: `Delete ${env.name}?` });
		await expect(dialog).toBeVisible();
		await dialog.getByRole("button", { name: "Delete", exact: true }).click();

		await expect(page).toHaveURL(
			new RegExp(`/projects/${projectId}/environments(\\?.*)?$`),
		);
		await expect(page.getByText(env.name)).toHaveCount(0);
		const remaining = await listEnvironments(request, projectId);
		expect(remaining.map((e) => e.id)).not.toContain(env.id);
	});
});
