// spec: features/projects/conversation-follow.feature
// seed: tests/seed.spec.ts
//
// Regression: sending a message while scrolled up in a long conversation must
// bring the viewport to the tail and keep the sender's message visible. The
// tail-follow indicator used to undo the programmatic jump, which withheld the
// new message until a reload. A fake ACP bridge stands in for the agent daemon
// (see acp-agent-conversations.spec.ts).
//
// Desktop only: the Conversations page is a master/detail split on mobile.

import { expect, type Page, test } from "@playwright/test";
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

const PROJECT_PREFIX = "E2E_FOLLOW_";
const RUN_ID = newRunId();
const HISTORY_MESSAGES = 40;

function composerInput(page: Page) {
	return page.getByRole("textbox", { name: "Message input" });
}

/** The scroll container of the thread (the closest scrollable ancestor). */
async function threadScroller(page: Page) {
	const handle = await page
		.getByText("E2E_FOLLOW_HISTORY_0", { exact: false })
		.first()
		.evaluateHandle((el) => {
			let node: HTMLElement | null = el as HTMLElement;
			while (node && node.scrollHeight <= node.clientHeight) {
				node = node.parentElement;
			}
			return node ?? document.scrollingElement;
		});
	return handle;
}

test.describe("Following the tail of a conversation", () => {
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
		bridge = await FakeBridge.connect(
			agentId,
			(await tokenResponse.json()).data.token,
		);
	});

	test.afterEach(async ({ request }) => {
		bridge?.close();
		await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
	});

	test("A message sent while scrolled up is shown without a reload", async ({
		page,
		request,
	}) => {
		const { id } = await startProjectChat(
			request,
			projectId,
			agentId,
			"E2E_FOLLOW_HISTORY_0",
		);
		await bridge.next("start_turn", id);
		for (let i = 1; i < HISTORY_MESSAGES; i++) {
			const response = await request.post(
				`${API_URL}/projects/${projectId}/conversations/${id}/messages`,
				{ data: { message: `E2E_FOLLOW_HISTORY_${i}`, on_busy: "force" } },
			);
			expect(response.ok()).toBeTruthy();
		}

		await signIn(page);
		await page.goto(`${BASE_URL}/projects/${projectId}/conversations/${id}`);
		await expect(
			page.getByText(`E2E_FOLLOW_HISTORY_${HISTORY_MESSAGES - 1}`).first(),
		).toBeVisible();

		// Scroll to the top so the viewport is not at the bottom.
		const scroller = await threadScroller(page);
		await scroller.evaluate((el) => {
			(el as HTMLElement).scrollTop = 0;
		});

		const sent = `E2E_FOLLOW_SENT_${RUN_ID}`;
		await composerInput(page).fill(sent);
		await page.getByRole("button", { name: "Send message" }).click();

		// The sender's message appears and the viewport follows it to the tail.
		const sentMessage = page.getByText(sent).first();
		await expect(sentMessage).toBeVisible();
		await expect(sentMessage).toBeInViewport();
		// No "N new messages" pill is left over.
		await expect(page.getByText(/^\d+ new messages?$/)).toHaveCount(0);
	});
});
