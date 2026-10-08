// spec: features/iam/role-badges-selector.feature
// seed: tests/seed.spec.ts
//
// Principals hold any number of roles, so the UI shows them as badges (a tinted
// pill, a description tooltip, a "+N" overflow popover, tags for Default /
// Built-in / Full access) and picks them with a searchable multi-select (type
// to filter, several at once, a selected count, Clear). A project member must
// keep at least one role.
//
// Setup is over the API; the browser checks what each page shows and does. The
// agents' role tab is covered by tests/admin/agents-management.spec.ts.

import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	test,
} from "@playwright/test";
import {
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createProject,
	globalRoleIdByName,
	newRunId,
	projectRoleIdByName,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import {
	allow,
	createMember,
	createPerson,
	policyOf,
	projectRoleFrom,
	putUserRoles,
} from "../helpers/iam";
import {
	closeRoleSelect,
	openRoleSelect,
	roleOptionIn,
	setRole,
} from "../helpers/role-select";

const PREFIX = "E2E_IAMBADGE_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
}

test.beforeEach(async ({ request, context }) => {
	await authRequest(request);
	await cleanup(request);
	await context.clearCookies();
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await cleanup(request);
});

/**
 * Hovers an element and nudges the pointer inside it: the tooltip opens on
 * pointer movement over its trigger, which a single hover() does not produce.
 */
async function hoverBadge(page: Page, target: Locator) {
	await target.hover();
	const box = await target.boundingBox();
	if (box) {
		await page.mouse.move(
			box.x + box.width / 2 + 1,
			box.y + box.height / 2 + 1,
		);
	}
}

const readUsers = policyOf(allow(["users:read"], ["user", "user/*"]));

// ─── The users list ──────────────────────────────────────────────────────────

test.describe("Role badges on the users list", () => {
	let manyRoles: { username: string; roleNames: string[] };
	let plain: { username: string };
	let describedRole: string;
	const description = "Reads the audit trail";

	test.beforeEach(async ({ request, playwright, page }) => {
		const roleNames = [name("ROLE_A"), name("ROLE_B"), name("ROLE_C")];
		const roles = [];
		for (const [i, roleName] of roleNames.entries()) {
			const response = await request.post(`${BASE_URL}/api/v1/admin/roles`, {
				data: {
					name: roleName,
					description: i === 0 ? description : "",
					policy: readUsers,
				},
			});
			expect(response.status()).toBe(201);
			roles.push((await response.json()).data as { id: string });
		}
		describedRole = roleNames[0];
		const many = await createPerson(request, playwright, name("MANY"));
		const userRole = await globalRoleIdByName(request, "USER");
		expect(
			(
				await putUserRoles(request, many.userId, [
					userRole,
					...roles.map((r) => r.id),
				])
			).status(),
		).toBe(200);
		manyRoles = { username: many.username, roleNames: [...roleNames, "USER"] };
		plain = await createPerson(request, playwright, name("PLAIN"));

		await signIn(page);
		await page.goto(`${BASE_URL}/admin/users`);
		await expect(
			page.getByRole("heading", { name: "User Management" }),
		).toBeVisible();
		await page.getByRole("searchbox", { name: "Search users" }).fill(PREFIX);
		// The list is re-rendered once the debounced search has been answered;
		// wait for it so nothing is hovered or clicked on a row about to be replaced.
		await expect(page.getByText("Results: 2")).toBeVisible();
	});

	const rowOf = (page: Page, username: string): Locator =>
		page
			.getByRole("row")
			.filter({ has: page.getByText(username, { exact: true }) });

	test("The table has User, Role and Created columns and shows each user's roles", async ({
		page,
	}) => {
		await expect(
			page.getByRole("columnheader", { name: "User", exact: true }),
		).toBeVisible();
		await expect(
			page.getByRole("columnheader", { name: "Role" }),
		).toBeVisible();
		await expect(
			page.getByRole("columnheader", { name: "Created" }),
		).toBeVisible();
		await expect(
			rowOf(page, plain.username).getByText("USER", { exact: true }),
		).toBeVisible();
		await expect(rowOf(page, manyRoles.username)).toBeVisible();
	});

	test("Roles past the second fold into a +N badge that opens the full list", async ({
		page,
	}) => {
		const row = rowOf(page, manyRoles.username);
		const more = row.getByRole("button", { name: "Show more roles (+2)" });
		await expect(more).toBeVisible();
		await expect(more).toHaveText("+2");
		// A user with a single role has no overflow.
		await expect(
			rowOf(page, plain.username).getByRole("button", {
				name: /Show more roles/,
			}),
		).toHaveCount(0);

		await more.click();
		const popover = page.getByText("All roles (4)").locator("..");
		await expect(popover).toBeVisible();
		for (const roleName of manyRoles.roleNames) {
			await expect(popover.getByText(roleName, { exact: true })).toBeVisible();
		}
	});

	test("Hovering a badge shows the role's description and what kind of role it is", async ({
		page,
	}) => {
		// A described role: its description is in the tooltip.
		const described = rowOf(page, manyRoles.username).getByText(describedRole, {
			exact: true,
		});
		await hoverBadge(page, described);
		const tooltip = page.locator('[data-slot="tooltip-content"]');
		await expect(tooltip).toContainText(description);

		// A role without one says so; the default built-in role names its kind.
		const userBadge = rowOf(page, plain.username).getByText("USER", {
			exact: true,
		});
		await hoverBadge(page, userBadge);
		await expect(tooltip).toContainText("Built-in");
		await expect(tooltip).toContainText("Default");
		await expect(tooltip).toContainText("No description");
	});

	test("The role filter narrows the list to users holding a role, and Clear filters resets it", async ({
		page,
	}) => {
		await page.getByRole("combobox", { name: "Filter by role" }).click();
		await page
			.getByRole("option", { name: describedRole, exact: true })
			.click();

		await expect(rowOf(page, manyRoles.username)).toBeVisible();
		await expect(rowOf(page, plain.username)).toHaveCount(0);
		await expect(page.getByText("Results: 1")).toBeVisible();

		await page.getByRole("button", { name: "Clear filters" }).first().click();
		await expect(rowOf(page, plain.username)).toBeVisible();
		await expect(rowOf(page, manyRoles.username)).toBeVisible();
	});
});

// ─── The change-role dialog ──────────────────────────────────────────────────

test.describe("Choosing roles for a user", () => {
	let username: string;
	let userId: string;
	let roleA: string;
	let roleB: string;
	let roleC: string;
	const describedA = "Alpha reads users";

	test.beforeEach(async ({ request, playwright, page }) => {
		roleA = name("PICK_ALPHA");
		roleB = name("PICK_BETA");
		roleC = name("PICK_GAMMA");
		await createGlobalRole(request, roleA, readUsers);
		await createGlobalRole(request, roleB, readUsers);
		await createGlobalRole(request, roleC, readUsers);
		// Give one of them a description through the API for the search.
		const roles = (
			await (await request.get(`${BASE_URL}/api/v1/admin/roles`)).json()
		).data as Array<{ id: string; name: string }>;
		const a = roles.find((r) => r.name === roleA);
		await request.put(`${BASE_URL}/api/v1/admin/roles/${a?.id}`, {
			data: { name: roleA, description: describedA, policy: readUsers },
		});
		const person = await createPerson(request, playwright, name("PICKER"));
		username = person.username;
		userId = person.userId;

		await signIn(page);
		await page.goto(`${BASE_URL}/admin/users`);
		await page.getByRole("searchbox", { name: "Search users" }).fill(username);
		await page
			.getByRole("row")
			.filter({ has: page.getByText(username, { exact: true }) })
			.getByRole("button", { name: "Change role" })
			.click();
	});

	const dialog = (page: Page) =>
		page.getByRole("dialog", { name: "Change role" });

	test("Roles are listed with Current, Built-in, Default and Full access tags", async ({
		page,
	}) => {
		const list = await openRoleSelect(page, dialog(page), "Change role");
		await expect(roleOptionIn(list, "USER")).toHaveAccessibleDescription(
			/Current/,
		);
		await expect(roleOptionIn(list, "USER")).toHaveAccessibleDescription(
			/Built-in/,
		);
		await expect(roleOptionIn(list, "USER")).toHaveAccessibleDescription(
			/Default/,
		);
		await expect(roleOptionIn(list, "SUPER_ADMIN")).toHaveAccessibleDescription(
			/Full access/,
		);
		await expect(roleOptionIn(list, roleB)).not.toHaveAccessibleDescription(
			/Current/,
		);
		await expect(roleOptionIn(list, roleA)).toHaveAccessibleDescription(
			new RegExp(describedA),
		);
	});

	test("Typing filters the roles by name or description", async ({ page }) => {
		const list = await openRoleSelect(page, dialog(page), "Change role");
		const search = page.getByRole("combobox", { name: "Search roles" });

		await search.fill("PICK_BE");
		await expect(roleOptionIn(list, roleB)).toBeVisible();
		await expect(roleOptionIn(list, roleA)).toHaveCount(0);

		// The description is searched too.
		await search.fill("alpha reads");
		await expect(roleOptionIn(list, roleA)).toBeVisible();
		await expect(roleOptionIn(list, roleB)).toHaveCount(0);

		await search.fill("no-such-role-zzz");
		await expect(
			page.getByText("No roles match “no-such-role-zzz”"),
		).toBeVisible();

		await page.getByRole("button", { name: "Clear search" }).click();
		await expect(roleOptionIn(list, roleB)).toBeVisible();
		await expect(roleOptionIn(list, roleA)).toBeVisible();
	});

	test("Several roles can be picked, counted and cleared", async ({ page }) => {
		const list = await openRoleSelect(page, dialog(page), "Change role");
		await expect(page.getByText("Selected: 1")).toBeVisible();

		await roleOptionIn(list, roleA).click();
		await roleOptionIn(list, roleB).click();
		await expect(page.getByText("Selected: 3")).toBeVisible();
		await expect(roleOptionIn(list, roleA)).toHaveAttribute(
			"aria-selected",
			"true",
		);
		await expect(roleOptionIn(list, roleB)).toHaveAttribute(
			"aria-selected",
			"true",
		);

		// Clear empties the selection (a user may hold none).
		await page.getByRole("button", { name: "Clear", exact: true }).click();
		await expect(page.getByText("Selected: 0")).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Clear", exact: true }),
		).toHaveCount(0);
		await expect(roleOptionIn(list, "USER")).toHaveAttribute(
			"aria-selected",
			"false",
		);
	});

	test("The chosen set is saved and shown as badges", async ({
		page,
		request,
	}) => {
		await setRole(page, dialog(page), "Change role", roleA, true);
		await setRole(page, dialog(page), "Change role", roleB, true);
		await dialog(page).getByRole("button", { name: "Assign role" }).click();
		await expect(dialog(page)).toHaveCount(0);

		const response = await request.get(
			`${BASE_URL}/api/v1/admin/users/${userId}/roles`,
		);
		const roles: Array<{ name: string }> = (await response.json()).data;
		expect(roles.map((r) => r.name).sort()).toEqual(
			["USER", roleA, roleB].sort(),
		);
	});

	test("A role that grants everything is flagged before it is assigned", async ({
		page,
	}) => {
		await expect(
			dialog(page).getByText(/This role has full access/),
		).toHaveCount(0);
		await setRole(page, dialog(page), "Change role", "SUPER_ADMIN", true);
		await expect(
			dialog(page).getByText(/This role has full access/),
		).toBeVisible();
		await setRole(page, dialog(page), "Change role", "SUPER_ADMIN", false);
		await expect(
			dialog(page).getByText(/This role has full access/),
		).toHaveCount(0);
		await dialog(page).getByRole("button", { name: "Cancel" }).click();
	});
});

// ─── The Team page ───────────────────────────────────────────────────────────

test.describe("Role badges and the selector on the Team page", () => {
	let projectId: string;
	let viewer: string;
	let editor: string;

	test.beforeEach(async ({ request }) => {
		projectId = await createProject(request, name("TEAM"));
		viewer = await projectRoleIdByName(request, projectId, "Viewer");
		editor = await projectRoleIdByName(request, projectId, "Editor");
	});

	const teamUrl = () => `${BASE_URL}/projects/${projectId}/team`;
	const rowOf = (page: Page, username: string): Locator =>
		page.getByText(`@${username}`, { exact: true }).locator("../..");

	async function openChip(page: Page, username: string) {
		await rowOf(page, username)
			.getByRole("button", { name: "Change role" })
			.click();
		const list = page.getByRole("listbox", { name: "Change role" });
		await expect(list).toBeVisible();
		return list;
	}

	test("A reader sees up to three role badges and a +N popover for the rest", async ({
		page,
		request,
		playwright,
	}) => {
		const extra: string[] = [];
		for (const label of ["X1", "X2"]) {
			extra.push(
				await projectRoleFrom(
					request,
					projectId,
					name(`TEAM_${label}`),
					allow(["tasks:read"], [`project/${projectId}/*`]),
				),
			);
		}
		const holder = await createMember(
			request,
			playwright,
			projectId,
			name("HOLDER"),
			[viewer, editor, ...extra],
		);
		const reader = await createMember(
			request,
			playwright,
			projectId,
			name("READER"),
			[
				await projectRoleFrom(
					request,
					projectId,
					name("TEAM_READER"),
					allow(
						["projects:read", "project.members:read", "roles:read"],
						[`project/${projectId}`, `project/${projectId}/role/*`],
					),
				),
			],
		);

		await signIn(page, reader.username, RESTRICTED_PASSWORD);
		await page.goto(teamUrl());
		const row = rowOf(page, holder.username);
		await expect(row).toBeVisible();
		const more = row.getByRole("button", { name: "Show more roles (+1)" });
		await expect(more).toBeVisible();
		await more.click();
		const popover = page.getByText("All roles (4)").locator("..");
		for (const roleName of [
			"Viewer",
			"Editor",
			name("TEAM_X1"),
			name("TEAM_X2"),
		]) {
			await expect(popover.getByText(roleName, { exact: true })).toBeVisible();
		}
	});

	test("A member keeps at least one role: the last one cannot be unpicked and there is no Clear", async ({
		page,
		request,
		playwright,
	}) => {
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("ONEROLE"),
			[viewer],
		);
		await signIn(page);
		await page.goto(teamUrl());
		const list = await openChip(page, member.username);

		await expect(
			page.getByText("At least one role is required."),
		).toBeVisible();
		const only = roleOptionIn(list, "Viewer");
		await expect(only).toHaveAttribute("aria-selected", "true");
		await expect(only).toHaveAttribute("aria-disabled", "true");
		// aria-disabled: Playwright would wait for it to be enabled, so force the click.
		await only.click({ force: true });
		await expect(only).toHaveAttribute("aria-selected", "true");
		await expect(
			page.getByRole("button", { name: "Clear", exact: true }),
		).toHaveCount(0);

		// With a second role the first can be dropped again, down to one.
		await roleOptionIn(list, "Editor").click();
		await expect(page.getByText("Selected: 2")).toBeVisible();
		await expect(page.getByText("At least one role is required.")).toHaveCount(
			0,
		);
		await roleOptionIn(list, "Viewer").click();
		await expect(roleOptionIn(list, "Viewer")).toHaveAttribute(
			"aria-selected",
			"false",
		);
		await expect(
			page.getByText("At least one role is required."),
		).toBeVisible();
	});

	test("The selector filters as you type, tags each role, and picks several at once", async ({
		page,
		request,
		playwright,
	}) => {
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("TAGGED"),
			[viewer],
		);
		await signIn(page);
		await page.goto(teamUrl());
		const list = await openChip(page, member.username);

		// The roles the member holds are ticked.
		await expect(roleOptionIn(list, "Viewer")).toHaveAttribute(
			"aria-selected",
			"true",
		);
		await expect(roleOptionIn(list, "Editor")).toHaveAttribute(
			"aria-selected",
			"false",
		);
		// The built-in project Admin is a system role with full access.
		await expect(roleOptionIn(list, "Admin")).toHaveAccessibleDescription(
			/Built-in/,
		);
		await expect(roleOptionIn(list, "Admin")).toHaveAccessibleDescription(
			/Full access/,
		);

		const search = page.getByRole("combobox", { name: "Search roles" });
		await search.fill("edit");
		await expect(roleOptionIn(list, "Editor")).toBeVisible();
		await expect(roleOptionIn(list, "Viewer")).toHaveCount(0);
		await expect(roleOptionIn(list, "Admin")).toHaveCount(0);
		await search.fill("zzz-nothing");
		await expect(page.getByText("No roles match “zzz-nothing”")).toBeVisible();
		await search.fill("");

		await roleOptionIn(list, "Editor").click();
		await roleOptionIn(list, "Admin").click();
		await expect(page.getByText("Selected: 3")).toBeVisible();

		await expect
			.poll(async () => {
				const response = await request.get(
					`${BASE_URL}/api/v1/projects/${projectId}/members/${member.memberId}/roles`,
				);
				return ((await response.json()).data as Array<{ name: string }>)
					.map((r) => r.name)
					.sort();
			})
			.toEqual(["Admin", "Editor", "Viewer"]);

		// The chip on the row now shows two badges and a +1.
		await page.keyboard.press("Escape");
		await expect(
			rowOf(page, member.username).getByText("+1", { exact: true }),
		).toBeVisible();
	});

	test("Add member's role field is a searchable multi-select with a count and Clear", async ({
		page,
		request,
		playwright,
	}) => {
		await createPerson(request, playwright, name("CANDIDATE"));
		await signIn(page);
		await page.goto(teamUrl());
		await page.getByRole("button", { name: "Add Member" }).click();
		const dialog = page.getByRole("dialog", { name: "Add member" });
		const list = await openRoleSelect(page, dialog, "Role");

		const search = page.getByRole("combobox", { name: "Search roles" });
		await search.fill("view");
		await expect(roleOptionIn(list, "Viewer")).toBeVisible();
		await expect(roleOptionIn(list, "Editor")).toHaveCount(0);
		await search.fill("");

		await roleOptionIn(list, "Viewer").click();
		await roleOptionIn(list, "Editor").click();
		await expect(page.getByText("Selected: 2")).toBeVisible();
		await page.getByRole("button", { name: "Clear", exact: true }).click();
		await expect(page.getByText("Selected: 0")).toBeVisible();
		await closeRoleSelect(page, "Role");
		await expect(
			dialog.getByRole("button", { name: "Add member" }),
		).toBeDisabled();
	});
});
