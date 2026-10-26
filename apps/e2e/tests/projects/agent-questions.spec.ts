// spec: features/projects/agent-questions.feature
// seed: tests/seed.spec.ts
//
// The e2e stack has no LLM credentials, so the agent side is simulated: the
// question comments an agent would post through the ask_user_question MCP tool
// are posted over the API as the admin, and the chat transcript is supplied by
// overriding the conversation's events response (see conversations.spec.ts).
// The asker of a question comment is therefore the admin, which is also who the
// answer's reply mentions.

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
	cleanupUsersByPrefix,
	createProject,
	createProjectAgent,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
	startProjectChat,
} from "../helpers/e2e-api";

const PROJECT_PREFIX = "E2E_AQ_";
const MEMBER_PREFIX = "E2E_AQ_MEMBER_";
const RUN_ID = newRunId();

let counter = 0;

type Json = Record<string, unknown>;
type Block = Json & { content?: unknown; children?: unknown };
interface Comment {
	id: string;
	activity_type: string;
	content: Block[] | null;
}

function uniqueName(label: string): string {
	counter += 1;
	return `${PROJECT_PREFIX}${label}_${RUN_ID}${counter}`;
}

// ─── API helpers ─────────────────────────────────────────────────────────────

const text = (value: string, styles: Json = {}) => ({
	type: "text",
	text: value,
	styles,
});

function paragraph(...content: Json[]): Json {
	return { type: "paragraph", content, children: [] };
}

/** The block an agent's ask_user_question call stores in a comment. */
function askBlock(
	question: string,
	options: string[],
	multiSelect = false,
): Json {
	return {
		type: "codeBlock",
		props: { language: "paca-ask" },
		content: [
			text(
				JSON.stringify({
					type: "paca-ask-question",
					question,
					options: options.map((label) => ({ label })),
					multiSelect,
				}),
			),
		],
		children: [],
	};
}

async function seedTask(
	request: APIRequestContext,
	projectId: string,
): Promise<string> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/tasks`,
		{
			data: { title: uniqueName("TASK") },
		},
	);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data.id as string;
}

async function seedDoc(
	request: APIRequestContext,
	projectId: string,
	title = uniqueName("DOC"),
): Promise<{ id: string; title: string }> {
	const response = await request.post(`${API_URL}/projects/${projectId}/docs`, {
		data: { title },
	});
	expect(response.ok()).toBeTruthy();
	return { id: (await response.json()).data.id as string, title };
}

type Target = { kind: "task" | "doc"; projectId: string; id: string };

const commentsUrl = (t: Target) =>
	t.kind === "task"
		? `${API_URL}/projects/${t.projectId}/tasks/${t.id}/activities/comments`
		: `${API_URL}/projects/${t.projectId}/docs/${t.id}/comments`;
const activitiesUrl = (t: Target) =>
	t.kind === "task"
		? `${API_URL}/projects/${t.projectId}/tasks/${t.id}/activities?page_size=100`
		: `${API_URL}/projects/${t.projectId}/docs/${t.id}/activities?page_size=100`;

async function postComment(
	request: APIRequestContext,
	target: Target,
	blocks: Json[],
	asContext: APIRequestContext = request,
): Promise<void> {
	const response = await asContext.post(commentsUrl(target), {
		data: { content: blocks },
	});
	expect(response.ok()).toBeTruthy();
}

async function listComments(
	request: APIRequestContext,
	target: Target,
): Promise<Comment[]> {
	const response = await request.get(activitiesUrl(target));
	expect(response.ok()).toBeTruthy();
	const items: Comment[] = (await response.json()).data.items ?? [];
	// Newest first; return oldest first.
	return items.filter((a) => a.activity_type === "comment").reverse();
}

/** Every inline node of a comment, across blocks and nested children. */
function inlineNodes(blocks: Block[] | null): Json[] {
	const nodes: Json[] = [];
	for (const block of blocks ?? []) {
		if (Array.isArray(block.content)) nodes.push(...(block.content as Json[]));
		nodes.push(...inlineNodes((block.children as Block[]) ?? []));
	}
	return nodes;
}

const mentions = (blocks: Block[] | null) =>
	inlineNodes(blocks).filter((n) => n.type === "teamMention");

const plainText = (blocks: Block[] | null) =>
	inlineNodes(blocks)
		.filter((n) => n.type === "text")
		.map((n) => n.text as string)
		.join("");

async function me(
	request: APIRequestContext,
): Promise<{ id: string; full_name: string; username: string }> {
	const response = await request.get(`${API_URL}/users/me`);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data;
}

async function adminMemberId(
	request: APIRequestContext,
	projectId: string,
): Promise<string> {
	const response = await request.get(
		`${API_URL}/projects/${projectId}/members?search=admin`,
	);
	expect(response.ok()).toBeTruthy();
	const items: Array<{ id: string; username: string }> = (await response.json())
		.data.items;
	const admin = items.find((m) => m.username === "admin");
	expect(admin).toBeTruthy();
	return (admin as { id: string }).id;
}

/** Polls until the target has a comment after the first one satisfying `match`. */
async function waitForReply(
	request: APIRequestContext,
	target: Target,
	match: (c: Comment) => boolean,
): Promise<Comment> {
	let found: Comment | undefined;
	await expect
		.poll(
			async () => {
				found = (await listComments(request, target)).slice(1).find(match);
				return found !== undefined;
			},
			{ timeout: 15_000 },
		)
		.toBe(true);
	return found as Comment;
}

async function cleanup(request: APIRequestContext): Promise<void> {
	await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
	await cleanupUsersByPrefix(request, MEMBER_PREFIX);
}

// ─── UI helpers ──────────────────────────────────────────────────────────────

const askCard = (page: Page) => page.locator('[data-slot="ask-question-card"]');

async function openTarget(page: Page, target: Target): Promise<void> {
	if (target.kind === "task") {
		await page.goto(
			`${BASE_URL}/projects/${target.projectId}/tasks/${target.id}`,
		);
		return;
	}
	await page.goto(`${BASE_URL}/projects/${target.projectId}/docs/${target.id}`);
	await page.getByRole("button", { name: "Comments & activity" }).click();
}

test.skip(
	({ isMobile }) => isMobile,
	"The activity pane and conversation layout differ on phones; desktop only",
);

test.afterEach(async ({ request }) => {
	await cleanup(request);
});

// ─── Plain-text mentions become badges ───────────────────────────────────────

test.describe("Mentions written as plain text become mention badges", () => {
	let task: Target;
	let doc: Target;

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
		const projectId = await createProject(request, uniqueName("MENTION"));
		task = { kind: "task", projectId, id: await seedTask(request, projectId) };
		doc = {
			kind: "doc",
			projectId,
			id: (await seedDoc(request, projectId)).id,
		};
	});

	test('"@username" in a task comment becomes a mention', async ({
		request,
	}) => {
		const admin = await me(request);

		await postComment(request, task, [
			paragraph(text("Thanks @admin, over to @nobody_here")),
		]);

		const [comment] = await listComments(request, task);
		expect(mentions(comment.content)).toEqual([
			{
				type: "teamMention",
				props: { id: admin.id, name: admin.full_name, avatar: "" },
			},
		]);
		expect(plainText(comment.content)).toContain("@nobody_here");
	});

	test('"@Name (id: ...)" in a task comment becomes a mention', async ({
		request,
	}) => {
		const admin = await me(request);
		const memberId = await adminMemberId(request, task.projectId);

		await postComment(request, task, [
			paragraph(text(`@someone (id: ${memberId}) please look`)),
		]);

		const [comment] = await listComments(request, task);
		expect(mentions(comment.content).map((m) => m.props)).toEqual([
			{ id: admin.id, name: admin.full_name, avatar: "" },
		]);
		expect(plainText(comment.content)).toBe(" please look");
	});

	test('"@username" in a document comment becomes a mention', async ({
		request,
	}) => {
		await postComment(request, doc, [paragraph(text("Thanks @admin"))]);

		const [comment] = await listComments(request, doc);
		expect(mentions(comment.content)).toHaveLength(1);
		expect(plainText(comment.content)).toBe("Thanks ");
	});

	test("E-mail addresses, inline code and code blocks are left alone", async ({
		request,
	}) => {
		await postComment(request, task, [
			paragraph(
				text("write to ops@admin.example or run "),
				text("@admin", { code: true }),
			),
			{
				type: "codeBlock",
				props: { language: "text" },
				content: [text("@admin")],
				children: [],
			},
		]);

		const [comment] = await listComments(request, task);
		expect(mentions(comment.content)).toHaveLength(0);
		expect(plainText(comment.content)).toContain("ops@admin.example");
	});
});

// ─── A person mentioned in a document comment is notified ────────────────────

test.describe("A person mentioned in a document comment is notified", () => {
	test("The mentioned member sees a notification that opens the document", async ({
		page,
		request,
		playwright,
	}) => {
		await authRequest(request);
		await cleanup(request);
		const projectId = await createProject(request, uniqueName("NOTIFY"));
		const doc = await seedDoc(request, projectId, uniqueName("NOTIFY_DOC"));
		const username = `${MEMBER_PREFIX}${RUN_ID}_${++counter}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: `${username}_role`,
			permissions: { "docs:read": true },
		});

		await postComment(request, { kind: "doc", projectId, id: doc.id }, [
			paragraph(text(`@${username} can you review this?`)),
		]);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.getByRole("button", { name: "Notifications" }).click();
		const item = page.getByText(
			new RegExp(`mentioned you in document ${doc.title}`),
		);
		await expect(item).toBeVisible({ timeout: 15_000 });

		await item.click();
		await expect(page).toHaveURL(
			new RegExp(`/projects/${projectId}/docs/${doc.id}$`),
		);
	});
});

// ─── A question in a comment is shown as a card ──────────────────────────────

for (const kind of ["task", "doc"] as const) {
	test.describe(`A question in a ${kind} comment is shown as a card`, () => {
		let target: Target;

		test.beforeEach(async ({ request, page }) => {
			await authRequest(request);
			await cleanup(request);
			const projectId = await createProject(request, uniqueName("ASK"));
			target = {
				kind,
				projectId,
				id:
					kind === "task"
						? await seedTask(request, projectId)
						: (await seedDoc(request, projectId)).id,
			};
			await signIn(page);
		});

		test("Shows the question and its options, not the raw payload", async ({
			page,
			request,
		}) => {
			await postComment(request, target, [
				askBlock("Test 123?", ["Test 1", "Test 2", "Test 3"]),
			]);

			await openTarget(page, target);

			const card = askCard(page);
			await expect(card).toContainText("Test 123?");
			for (const option of ["Test 1", "Test 2", "Test 3"]) {
				await expect(card.getByRole("button", { name: option })).toBeVisible();
			}
			await expect(page.getByText("paca-ask-question")).toHaveCount(0);
		});

		test("Picking an option posts a reply that mentions the asker", async ({
			page,
			request,
		}) => {
			const admin = await me(request);
			await postComment(request, target, [
				askBlock("Test 123?", ["Test 1", "Test 2"]),
			]);
			await openTarget(page, target);

			await askCard(page).getByRole("button", { name: "Test 2" }).click();

			await expect(askCard(page)).toContainText("You answered: Test 2");
			const reply = await waitForReply(
				request,
				target,
				(c) => plainText(c.content).trim() === "Test 2",
			);
			expect(mentions(reply.content).map((m) => m.props)).toEqual([
				{ id: admin.id, name: admin.full_name, avatar: "" },
			]);
		});

		test("A multi-select question collects several options", async ({
			page,
			request,
		}) => {
			await postComment(request, target, [
				askBlock("Which?", ["Test 1", "Test 2", "Test 3"], true),
			]);
			await openTarget(page, target);
			const card = askCard(page);

			await card.getByRole("button", { name: "Test 1" }).click();
			await card.getByRole("button", { name: "Test 3" }).click();
			await card.getByRole("button", { name: "Send answer" }).click();

			await expect(card).toContainText("You answered: Test 1, Test 3");
			await waitForReply(
				request,
				target,
				(c) => plainText(c.content).trim() === "Test 1, Test 3",
			);
		});

		test("The user can type their own answer", async ({ page, request }) => {
			await postComment(request, target, [askBlock("Which?", ["Test 1"])]);
			await openTarget(page, target);
			const card = askCard(page);

			await card
				.getByPlaceholder("Or type your own answer…")
				.fill("Something else");
			await card.getByRole("button", { name: "Send answer" }).click();

			await expect(card).toContainText("You answered: Something else");
			await waitForReply(
				request,
				target,
				(c) => plainText(c.content).trim() === "Something else",
			);
		});
	});
}

// ─── A question in a chat conversation is shown as a card ────────────────────

test.describe("A question in a chat conversation is shown as a card", () => {
	test("The card is visible without expanding the tool calls and sends the answer as a message", async ({
		page,
		request,
	}) => {
		await authRequest(request);
		await cleanup(request);
		const projectId = await createProject(request, uniqueName("CHAT"));
		const agent = await createProjectAgent(
			request,
			projectId,
			uniqueName("AGENT"),
			"llm",
		);
		const conversationId = (
			await startProjectChat(request, projectId, agent.id, "Ask me something")
		).id;
		await signIn(page);

		// The stack cannot run an agent, so serve the transcript an agent that
		// called ask_user_question would have produced. The call's input is
		// empty on purpose (as with Goose-wrapped CLIs): the question must come
		// from the tool result.
		const askResult = `Question shown to the user. Stop now and wait.\n\n\`\`\`paca-ask\n${JSON.stringify(
			{
				type: "paca-ask-question",
				question: "Test 123?",
				options: [{ label: "Test 1" }, { label: "Test 2" }],
				multiSelect: false,
			},
		)}\n\`\`\``;
		const event = (index: number, type: string, payload: Json) => ({
			id: `00000000-0000-4000-8000-00000000000${index}`,
			conversation_id: conversationId,
			event_index: index,
			event_type: type,
			event_source: type === "user_message" ? "user" : "agent",
			payload,
			created_at: new Date(Date.now() - 10_000 + index * 100).toISOString(),
		});
		await page.route(
			new RegExp(
				`/api/v1/projects/${projectId}/conversations/${conversationId}/events(\\?.*)?$`,
			),
			async (route) => {
				if (route.request().method() !== "GET") return route.fallback();
				const items = [
					event(1, "user_message", {
						content: { type: "text", text: "Ask me something" },
					}),
					event(2, "tool_call", {
						sessionUpdate: "tool_call",
						toolCallId: "tc-ask",
						title: "mcp: paca  ask user question",
						rawInput: {},
					}),
					event(3, "tool_call_update", {
						sessionUpdate: "tool_call_update",
						toolCallId: "tc-ask",
						status: "completed",
						content: [
							{ type: "content", content: { type: "text", text: askResult } },
						],
					}),
				];
				await route.fulfill({
					json: {
						success: true,
						data: {
							items,
							total: items.length,
							next_cursor: null,
							prev_cursor: null,
						},
					},
				});
			},
		);
		const sent: string[] = [];
		// Replying to a chat goes through its chat session, so match any
		// project message POST rather than one exact path.
		await page.route(
			new RegExp(`/api/v1/projects/${projectId}/.*/messages$`),
			async (route) => {
				if (route.request().method() !== "POST") return route.fallback();
				sent.push(route.request().postData() ?? "");
				await route.fulfill({
					status: 200,
					json: { success: true, data: {} },
				});
			},
		);

		await page.goto(
			`${BASE_URL}/projects/${projectId}/conversations/${conversationId}`,
		);

		// Visible without opening the "N tool calls" group.
		const card = askCard(page);
		await expect(card).toContainText("Test 123?");
		await card.getByRole("button", { name: "Test 1" }).click();

		await expect.poll(() => sent.length).toBeGreaterThan(0);
		expect(sent[0]).toContain("Test 1");
	});
});
