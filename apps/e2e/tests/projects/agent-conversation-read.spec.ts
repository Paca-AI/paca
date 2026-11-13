// spec: features/projects/agent-conversation-read.feature
// seed: tests/seed.spec.ts
//
// GET /agents/me/conversations/:id is what the Paca MCP's read_conversation
// tool calls. The router gates it on conversations:read, judged on the roles
// of the agent the API key proves (nothing else the client sends counts), over
// the conversation's own resource; so an IAM condition can limit an agent to
// some conversations, for instance its own. An owner-private chat is never
// readable through this path; task-assigned runs (project-shared) are. These specs call the endpoint as
// the MCP client does: with the agent's own API key, no other header.

import {
	type APIRequestContext,
	expect,
	type Playwright,
	test,
} from "@playwright/test";
import {
	API_URL,
	allowPolicy,
	authRequest,
	cleanupProjectsByPrefix,
	createProject,
	createProjectRole,
	handleFor,
	newRunId,
	POLICY_VERSION,
	replaceMemberRoles,
	startProjectChat,
} from "../helpers/e2e-api";
import { FakeBridge } from "../helpers/fake-bridge";
import { asAgent, createAgentKey } from "../helpers/iam";

const PROJECT_PREFIX = "E2E_AGENTREAD_";
const RUN_ID = newRunId();

interface Actor {
	id: string;
	memberId: string;
	key: string;
	bridge: FakeBridge;
}

test.describe("An agent reading conversations through the agent API", () => {
	let projectId: string;
	let noReadRoleId: string;
	let readRoleId: string;
	let bridges: FakeBridge[] = [];

	/** An ACP agent with the given role, its API key and a connected bridge. */
	async function createActor(
		request: APIRequestContext,
		label: string,
		roleId: string,
	): Promise<Actor> {
		const name = `${PROJECT_PREFIX}${label}_${RUN_ID}`;
		const created = await request.post(
			`${API_URL}/projects/${projectId}/agents`,
			{
				data: {
					name,
					handle: handleFor(name),
					role_ids: [roleId],
					agent_type: "acp",
					acp_provider: "claude-code",
				},
			},
		);
		expect(created.ok(), await created.text()).toBeTruthy();
		const id = (await created.json()).data.id as string;

		const members = await request.get(
			`${API_URL}/projects/${projectId}/members?page_size=100`,
		);
		const memberId = (
			(await members.json()).data.items as Array<{
				id: string;
				agent_id?: string;
			}>
		).find((m) => m.agent_id === id)?.id;
		expect(memberId, "the agent is a project member").toBeTruthy();

		const tokenResponse = await request.post(
			`${API_URL}/projects/${projectId}/agents/${id}/acp-bridge-token`,
		);
		expect(tokenResponse.ok()).toBeTruthy();
		const bridge = await FakeBridge.connect(
			id,
			(await tokenResponse.json()).data.token,
		);
		bridges.push(bridge);
		return {
			id,
			memberId: memberId ?? "",
			key: await createAgentKey(request, id, projectId),
			bridge,
		};
	}

	/**
	 * Assigns a task to `actor` and returns the id of the project-shared run its
	 * bridge is told to start (a chat is owner-private; a task run is not).
	 */
	async function assignedRun(
		request: APIRequestContext,
		actor: Actor,
		title: string,
	): Promise<string> {
		const task = await request.post(`${API_URL}/projects/${projectId}/tasks`, {
			data: { title, assignee_ids: [actor.memberId] },
		});
		expect(task.ok(), await task.text()).toBeTruthy();
		const started = await actor.bridge.next("start_turn");
		const runId = started.conversation_id as string;
		expect(runId).toBeTruthy();
		return runId;
	}

	/** GET /agents/me/conversations/:id as `actor`: the HTTP status. */
	async function readAs(
		playwright: Playwright,
		actor: Actor,
		conversationId: string,
	): Promise<number> {
		return asAgent(playwright, actor.key, async (api) => {
			const response = await api.get(
				`${API_URL}/agents/me/conversations/${conversationId}`,
			);
			return response.status();
		});
	}

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
		projectId = await createProject(request, `${PROJECT_PREFIX}${RUN_ID}`);
		noReadRoleId = (
			await createProjectRole(
				request,
				projectId,
				"E2E_NO_CONVERSATIONS_READ",
				allowPolicy(["tasks:read"], { projectId }),
			)
		).id;
		readRoleId = (
			await createProjectRole(
				request,
				projectId,
				"E2E_CONVERSATIONS_READ",
				allowPolicy(["conversations:read"], { projectId }),
			)
		).id;
		bridges = [];
	});

	test.afterEach(async ({ request }) => {
		for (const bridge of bridges) bridge.close();
		await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
	});

	test("An agent without conversations:read cannot read even its own conversation", async ({
		request,
		playwright,
	}) => {
		const agent = await createActor(request, "NOREAD", noReadRoleId);
		const own = await startProjectChat(request, projectId, agent.id, "hi");

		expect(await readAs(playwright, agent, own.id)).toBe(403);
	});

	test("An agent with conversations:read reads its own task run, no other header needed", async ({
		request,
		playwright,
	}) => {
		const agent = await createActor(request, "READER", readRoleId);
		const run = await assignedRun(request, agent, "Own work");

		expect(await readAs(playwright, agent, run)).toBe(200);
	});

	test("An owner-private chat is never readable through the agent API, the agent's own included", async ({
		request,
		playwright,
	}) => {
		const reader = await createActor(request, "READER", readRoleId);
		const peer = await createActor(request, "PEER", noReadRoleId);
		const ownChat = await startProjectChat(request, projectId, reader.id, "a");
		const peerChat = await startProjectChat(request, projectId, peer.id, "b");

		expect(await readAs(playwright, reader, ownChat.id)).toBe(404);
		expect(await readAs(playwright, reader, peerChat.id)).toBe(404);
	});

	test("A task-assigned run of another agent is readable with conversations:read only", async ({
		request,
		playwright,
	}) => {
		const reader = await createActor(request, "READER", readRoleId);
		const blind = await createActor(request, "BLIND", noReadRoleId);
		const peer = await createActor(request, "PEER", noReadRoleId);
		const run = await assignedRun(request, peer, "Delegated work");

		expect(await readAs(playwright, reader, run)).toBe(200);
		expect(await readAs(playwright, blind, run)).toBe(403);
	});

	test("A role limited by conversation.agent_id reads its own conversations and no others", async ({
		request,
		playwright,
	}) => {
		const agent = await createActor(request, "OWNONLY", noReadRoleId);
		const peer = await createActor(request, "PEER", noReadRoleId);
		const ownOnly = await createProjectRole(
			request,
			projectId,
			"E2E_OWN_ONLY",
			{
				version: POLICY_VERSION,
				statements: [
					{
						effect: "Allow",
						actions: ["conversations:read"],
						resources: [`project/${projectId}/*`],
						conditions: { StringEquals: { "conversation.agent_id": agent.id } },
					},
				],
			},
		);
		await replaceMemberRoles(request, projectId, agent.memberId, [ownOnly.id]);
		const own = await assignedRun(request, agent, "Mine");
		const peerRun = await assignedRun(request, peer, "Theirs");

		expect(await readAs(playwright, agent, own)).toBe(200);
		expect(await readAs(playwright, agent, peerRun)).toBe(403);
	});
});
