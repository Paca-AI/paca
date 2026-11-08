// spec: features/projects/acp-agent-conversations.feature
// seed: tests/seed.spec.ts
//
// The e2e stack has no real ACP CLI, so a fake bridge daemon (a plain
// WebSocket client) plays its part against the real stack: it connects to
// /agent-bridge/ws through the gateway with the agent's generated bridge
// token, receives the control messages the API dispatches, and reports
// turn_status back. Everything else (chat session, conversation status, the
// pause/stop endpoints, the UI) is the real thing.
//
// Desktop only: the Conversations page is a master/detail split on mobile.

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	createProject,
	createProjectAgent,
	newRunId,
	signIn,
	startProjectChat,
} from "../helpers/e2e-api";
import { FakeBridge } from "../helpers/fake-bridge";

test.skip(
	({ isMobile }) => isMobile,
	"Conversations use a mobile master/detail layout; desktop only",
);

const PROJECT_PREFIX = "E2E_ACPCONV_";
const RUN_ID = newRunId();

async function conversationStatus(
	request: APIRequestContext,
	projectId: string,
	conversationId: string,
): Promise<string> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/conversations/${conversationId}`,
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.status;
}

function headerBadge(page: Page) {
	return page
		.getByText("Chat session", { exact: true })
		.locator("xpath=following-sibling::*[1]");
}

test.describe("Conversations with an ACP agent", () => {
	let projectId: string;
	let agentId: string;
	let bridge: FakeBridge;

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
		projectId = await createProject(request, `${PROJECT_PREFIX}${RUN_ID}`);
		const agent = await createProjectAgent(
			request,
			projectId,
			`${PROJECT_PREFIX}BOT_${RUN_ID}`,
			"acp",
		);
		agentId = agent.id;

		const tokenResponse = await request.post(
			`${API_URL}/projects/${projectId}/agents/${agentId}/acp-bridge-token`,
		);
		expect(tokenResponse.ok()).toBeTruthy();
		const { token } = (await tokenResponse.json()).data;
		bridge = await FakeBridge.connect(agentId, token);
	});

	test.afterEach(async ({ request }) => {
		bridge?.close();
		await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
	});

	test("Sending a message dispatches start_turn to the connected bridge", async ({
		request,
	}) => {
		const { id } = await startProjectChat(request, projectId, agentId, "hi");

		await bridge.next("start_turn", id);
		await expect
			.poll(() => conversationStatus(request, projectId, id))
			.toBe("running");
	});

	test("The composer stop button pauses the turn and the conversation leaves Running", async ({
		page,
		request,
	}) => {
		const { id } = await startProjectChat(request, projectId, agentId, "work");
		await bridge.next("start_turn", id);

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/conversations/${id}`);
		await expect(headerBadge(page)).toHaveText("Running");

		await page.getByRole("button", { name: "Stop generating" }).click();

		await bridge.next("pause_turn", id);
		// The API writes nothing for a pause: still Running until the bridge
		// reports that the turn actually unwound.
		expect(await conversationStatus(request, projectId, id)).toBe("running");

		bridge.reportTurnStatus(id, "finished");

		await expect(headerBadge(page)).toHaveText("Finished");
		await expect(
			page.getByRole("button", { name: "Stop generating" }),
		).toHaveCount(0);
	});

	test("A full Stop is recorded as Stopped and dispatches stop_turn to the bridge", async ({
		page,
		request,
	}) => {
		const { id } = await startProjectChat(request, projectId, agentId, "work");
		await bridge.next("start_turn", id);

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/conversations/${id}`);
		await expect(headerBadge(page)).toHaveText("Running");

		// An ACP conversation has no header Stop button (the composer's stop
		// is the pause), so a full stop comes from the API.
		const stopResponse = await request.post(
			`${API_URL}/projects/${projectId}/conversations/${id}/stop`,
		);
		expect(stopResponse.ok()).toBeTruthy();

		await bridge.next("stop_turn", id);
		expect(await conversationStatus(request, projectId, id)).toBe("stopped");
		// A stop has no realtime push on the ACP path, so the badge only
		// catches up on the page's periodic reconcile.
		await expect(headerBadge(page)).toHaveText("Stopped", { timeout: 15_000 });
	});
});
