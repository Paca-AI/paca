// spec: features/projects/restricted-agent.feature
// seed: tests/seed.spec.ts
//
// An agent is restricted by a role, not by a setting on the agent: a project
// role with a Deny statement on the agent's resource, held by the member it
// should not reach. Deny always wins over what the member's other roles allow,
// so the member loses the agent while everyone else keeps it.
//
//   { effect: "Deny",
//     actions: ["agents:*", "conversations:*"],
//     resources: ["project/<projectId>/agent/<agentId>/*"] }
//
// Setup goes through the API (users, members, the agent, the role assignment);
// the UI is used where the scenario is about the UI: writing the Deny role in
// the Advanced JSON editor, and what each member sees on the Agents page.

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	addProjectMember,
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createProjectAgent,
	createUserWithPassword,
	denyResourceToMember,
	newRunId,
	projectRoleIdByName,
	replaceMemberRoles,
	RESTRICTED_PASSWORD,
	type SeededAgent,
	signIn,
} from "../helpers/e2e-api";
import { loginContext } from "../helpers/profile-users";

const PREFIX = "E2E_RESTRICT_";
const RUN_ID = newRunId();

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
}

interface Member {
	username: string;
	memberId: string;
}

const agentsUrl = (projectId: string) =>
	`${BASE_URL}/projects/${projectId}/agents`;

/** A project member holding the project's built-in Editor role. */
async function addMember(
	request: APIRequestContext,
	playwright: Parameters<typeof createUserWithPassword>[1],
	projectId: string,
	label: string,
): Promise<Member> {
	const username = `${PREFIX}${label}_${RUN_ID}`;
	const userId = await createUserWithPassword(request, playwright, {
		username,
		fullName: username,
	});
	const memberRoleId = await projectRoleIdByName(request, projectId, "Editor");
	const memberId = await addProjectMember(request, projectId, userId, [
		memberRoleId,
	]);
	return { username, memberId };
}

/** The agent's chat and configuration routes as one member sees them. */
async function agentAccess(
	playwright: Parameters<typeof loginContext>[0],
	member: Member,
	projectId: string,
	agent: SeededAgent,
	{ probeStart = true }: { probeStart?: boolean } = {},
) {
	const api = await loginContext(playwright, member.username);
	try {
		const base = `${API_URL}/projects/${projectId}/agents`;
		const list = await api.get(base);
		expect(list.ok()).toBeTruthy();
		const listed: Array<{ id: string }> = (await list.json()).data.items ?? [];
		return {
			listed: listed.some((a) => a.id === agent.id),
			detail: (await api.get(`${base}/${agent.id}`)).status(),
			sessions: (await api.get(`${base}/${agent.id}/chat-sessions`)).status(),
			// Starting a chat creates a running conversation, and an agent runs one
			// at a time by default, so only the probes that need the answer start one.
			start: probeStart
				? (
						await api.post(`${base}/${agent.id}/chat-sessions`, {
							data: { message: "Hello from a restricted-agent test" },
						})
					).status()
				: null,
		};
	} finally {
		await api.dispose();
	}
}

async function signInAs(page: Page, username: string) {
	await page.context().clearCookies();
	await signIn(page, username, RESTRICTED_PASSWORD);
}

test.describe("Restricting an agent with a role", () => {
	let projectId: string;
	let agent: SeededAgent;

	test.beforeEach(async ({ request, context }) => {
		await cleanup(request);
		await context.clearCookies();
		projectId = await createProject(request, `${PREFIX}PROJECT_${RUN_ID}`);
		agent = await createProjectAgent(
			request,
			projectId,
			`${PREFIX}SECRET_BOT_${RUN_ID}`,
		);
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	test("A member denied the agent cannot see or use it, while another member can", async ({
		page,
		request,
		playwright,
	}) => {
		const restricted = await addMember(
			request,
			playwright,
			projectId,
			"BLOCKED",
		);
		const other = await addMember(request, playwright, projectId, "ALLOWED");
		const memberRoleId = await projectRoleIdByName(
			request,
			projectId,
			"Editor",
		);

		// Before the Deny, both members reach the agent.
		const before = await agentAccess(playwright, restricted, projectId, agent, {
			probeStart: false,
		});
		expect(before.listed).toBe(true);
		expect(before.detail).toBe(200);

		await denyResourceToMember(request, {
			projectId,
			memberId: restricted.memberId,
			keepRoleIds: [memberRoleId],
			roleName: `${PREFIX}DENY_AGENT_${RUN_ID}`,
			kind: "agent",
			resourceId: agent.id,
			actions: ["agents:*", "conversations:*"],
		});

		// API: the agent is left out of the restricted member's list and every
		// route on it is refused...
		const blocked = await agentAccess(playwright, restricted, projectId, agent);
		expect(blocked).toEqual({
			listed: false,
			detail: 403,
			sessions: 403,
			start: 403,
		});
		// ...while the other member, holding the same Editor role, is unaffected.
		const allowed = await agentAccess(playwright, other, projectId, agent);
		expect(allowed.listed).toBe(true);
		expect(allowed.detail).toBe(200);
		expect(allowed.sessions).toBe(200);
		expect(allowed.start).toBeLessThan(300);

		// UI: the restricted member's Agents page does not show the agent...
		await signInAs(page, restricted.username);
		await page.goto(agentsUrl(projectId));
		await expect(
			page.getByRole("heading", { name: "AI Agents" }),
		).toBeVisible();
		await expect(page.getByText("No agents yet")).toBeVisible();
		await expect(page.getByText(agent.name, { exact: true })).toHaveCount(0);

		// ...and the other member's page does.
		await signInAs(page, other.username);
		await page.goto(agentsUrl(projectId));
		await expect(page.getByText(agent.name, { exact: true })).toBeVisible();
	});

	test("Denying only conversations leaves the agent visible but not usable", async ({
		page,
		request,
		playwright,
	}) => {
		const member = await addMember(request, playwright, projectId, "NOCHAT");
		const memberRoleId = await projectRoleIdByName(
			request,
			projectId,
			"Editor",
		);
		await denyResourceToMember(request, {
			projectId,
			memberId: member.memberId,
			keepRoleIds: [memberRoleId],
			roleName: `${PREFIX}DENY_CHAT_${RUN_ID}`,
			kind: "agent",
			resourceId: agent.id,
			actions: ["conversations:*"],
		});

		const access = await agentAccess(playwright, member, projectId, agent);
		expect(access).toEqual({
			listed: true,
			detail: 200,
			sessions: 403,
			start: 403,
		});

		await signInAs(page, member.username);
		await page.goto(agentsUrl(projectId));
		await expect(page.getByText(agent.name, { exact: true })).toBeVisible();
	});

	test("Taking the Deny role away gives the agent back", async ({
		request,
		playwright,
	}) => {
		const member = await addMember(request, playwright, projectId, "RESTORED");
		const memberRoleId = await projectRoleIdByName(
			request,
			projectId,
			"Editor",
		);
		await denyResourceToMember(request, {
			projectId,
			memberId: member.memberId,
			keepRoleIds: [memberRoleId],
			roleName: `${PREFIX}DENY_TEMP_${RUN_ID}`,
			kind: "agent",
			resourceId: agent.id,
			actions: ["agents:*", "conversations:*"],
		});
		expect(
			(await agentAccess(playwright, member, projectId, agent)).listed,
		).toBe(false);

		await replaceMemberRoles(request, projectId, member.memberId, [
			memberRoleId,
		]);

		const access = await agentAccess(playwright, member, projectId, agent);
		expect(access.listed).toBe(true);
		expect(access.detail).toBe(200);
		expect(access.sessions).toBe(200);
	});

	test("A Deny role written in the Advanced editor and given to a member restricts the agent", async ({
		page,
		request,
		playwright,
	}) => {
		const member = await addMember(request, playwright, projectId, "ADVANCED");
		const memberRoleId = await projectRoleIdByName(
			request,
			projectId,
			"Editor",
		);
		const roleName = `${PREFIX}NO_SECRET_BOT_${RUN_ID}`;

		// An admin writes the role as a policy: simple switches cannot say "not this agent".
		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
		await page.getByRole("button", { name: "Roles", exact: true }).click();
		await page.getByRole("button", { name: "New role" }).click();
		const dialog = page.getByRole("dialog", { name: "New Role" });
		await expect(dialog).toBeVisible();
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(roleName);
		await page.getByRole("tab", { name: "Advanced (JSON)" }).click();
		await dialog.getByRole("textbox", { name: "Policy (JSON)" }).fill(
			JSON.stringify({
				version: "2026-10-01",
				statements: [
					{
						effect: "Deny",
						actions: ["agents:*", "conversations:*"],
						resources: [`project/${projectId}/agent/${agent.id}/*`],
					},
				],
			}),
		);
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(dialog).not.toBeVisible();
		await expect(
			page
				.getByRole("table")
				.getByRole("row")
				.filter({ hasText: roleName }),
		).toBeVisible();

		// It is an ordinary role: give it to the member next to the one they hold.
		const roles: Array<{
			id: string;
			name: string;
			project_id: string | null;
		}> =
			(
				await (
					await request.get(`${API_URL}/projects/${projectId}/roles`)
				).json()
			).data ?? [];
		const denyRole = roles.find(
			(r) => r.name === roleName && r.project_id === projectId,
		);
		expect(denyRole, "the Deny role was saved").toBeTruthy();
		await replaceMemberRoles(request, projectId, member.memberId, [
			memberRoleId,
			denyRole?.id ?? "",
		]);

		// The Team page lists the roles each member holds.
		await page.goto(`${BASE_URL}/projects/${projectId}/team`);
		await expect(
			page
				.getByRole("button", { name: "Change role" })
				.filter({ hasText: roleName }),
		).toBeVisible();

		const access = await agentAccess(playwright, member, projectId, agent);
		expect(access).toEqual({
			listed: false,
			detail: 403,
			sessions: 403,
			start: 403,
		});
	});
});
