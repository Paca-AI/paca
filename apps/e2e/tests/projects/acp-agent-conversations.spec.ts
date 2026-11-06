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

test.skip(
	({ isMobile }) => isMobile,
	"Conversations use a mobile master/detail layout; desktop only",
);

const PROJECT_PREFIX = "E2E_ACPCONV_";
const RUN_ID = newRunId();

type BridgeMessage = Record<string, unknown> & { type: string };

/** A minimal stand-in for the local paca-acp-bridge daemon. */
class FakeBridge {
	private socket: WebSocket;
	private received: BridgeMessage[] = [];
	private pingTimer?: ReturnType<typeof setInterval>;

	private constructor(socket: WebSocket) {
		this.socket = socket;
		socket.addEventListener("message", (event) => {
			this.received.push(JSON.parse(String(event.data)) as BridgeMessage);
		});
	}

	static async connect(agentId: string, token: string): Promise<FakeBridge> {
		const url = `${BASE_URL.replace(/^http/, "ws")}/agent-bridge/ws`;
		const socket = new WebSocket(url);
		await new Promise<void>((resolve, reject) => {
			socket.addEventListener("open", () => resolve(), { once: true });
			socket.addEventListener(
				"error",
				() => reject(new Error(`could not open ${url}`)),
				{ once: true },
			);
		});
		const bridge = new FakeBridge(socket);
		socket.send(JSON.stringify({ type: "hello", agent_id: agentId, token }));
		await bridge.next("hello_ack");
		// The server drops a bridge whose presence key (45 s TTL) is not
		// refreshed; the real daemon pings every 20 s.
		bridge.pingTimer = setInterval(
			() => socket.send(JSON.stringify({ type: "ping" })),
			20_000,
		);
		return bridge;
	}

	/** Waits for (and consumes) the next message of the given type. */
	async next(type: string, conversationId?: string): Promise<BridgeMessage> {
		let found: BridgeMessage | undefined;
		await expect
			.poll(
				() => {
					const idx = this.received.findIndex(
						(m) =>
							m.type === type &&
							(conversationId === undefined ||
								m.conversation_id === conversationId),
					);
					if (idx >= 0) found = this.received.splice(idx, 1)[0];
					return found !== undefined;
				},
				{ message: `bridge never received ${type}`, timeout: 20_000 },
			)
			.toBe(true);
		return found as BridgeMessage;
	}

	/** Reports a turn status back to Paca, as the real daemon would. */
	reportTurnStatus(conversationId: string, status: string): void {
		this.socket.send(
			JSON.stringify({
				type: "turn_status",
				conversation_id: conversationId,
				status,
			}),
		);
	}

	close(): void {
		clearInterval(this.pingTimer);
		this.socket.close();
	}
}

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
