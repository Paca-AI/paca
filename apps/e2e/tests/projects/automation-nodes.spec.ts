// spec: features/projects/automation.feature
// seed: tests/seed.spec.ts
//
// Coverage for the second wave of automation nodes: add/remove/clear and
// dynamic values on field updates, create-subtask, link / comment / notify
// actions, the field-changed / subtask-changed triggers and the subtask-count
// and relative-date conditions.
//
// Two kinds of scenario live here:
//   * Builder scenarios drive the config panel in the browser and then read the
//     node back through the API, so they prove what the UI actually stores.
//   * Engine scenarios seed an active automation through the API and watch real
//     tasks change — the stack's API container runs the engine — so they prove
//     the stored config does the right thing end to end.
//
// The admin account is a super_admin and is the project's first member, so it
// can be used as an assignee / notification recipient without extra setup.

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
	newRunId,
	signIn,
} from "../helpers/e2e-api";

const PROJECT_PREFIX = "E2E_AUTOMATION_NODES_";
const RUN_ID = newRunId();
let projectCounter = 0;

// ─── Types ────────────────────────────────────────────────────────────────────

interface GraphNode {
	id: string;
	kind: string;
	type: string;
	config: Record<string, any>;
}

// ─── API helpers ──────────────────────────────────────────────────────────────

async function api<T = any>(
	request: APIRequestContext,
	method: "get" | "post" | "patch",
	path: string,
	data?: unknown,
): Promise<T> {
	const response = await request[method](
		`${API_URL}${path}`,
		data ? { data } : {},
	);
	expect(
		response.ok(),
		`${method} ${path} -> ${response.status()}`,
	).toBeTruthy();
	return (await response.json()).data as T;
}

async function createAutomation(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<string> {
	return (
		await api(request, "post", `/projects/${projectId}/automations`, { name })
	).id;
}

async function addNode(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
	node: { kind: string; type: string; config?: Record<string, unknown> },
): Promise<string> {
	const created = await api(
		request,
		"post",
		`/projects/${projectId}/automations/${automationId}/nodes`,
		{ ...node, config: node.config ?? {}, pos_x: 120, pos_y: 120 },
	);
	return created.id as string;
}

async function addEdge(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
	source: string,
	target: string,
	sourceHandle?: string,
): Promise<void> {
	await api(
		request,
		"post",
		`/projects/${projectId}/automations/${automationId}/edges`,
		{
			source_node_id: source,
			target_node_id: target,
			...(sourceHandle ? { source_handle: sourceHandle } : {}),
		},
	);
}

async function activate(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
): Promise<void> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/automations/${automationId}/activate`,
	);
	expect(response.ok(), `activate -> ${response.status()}`).toBeTruthy();
}

async function nodesOf(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
): Promise<GraphNode[]> {
	const graph = await api(
		request,
		"get",
		`/projects/${projectId}/automations/${automationId}`,
	);
	return graph.nodes as GraphNode[];
}

async function nodeOfType(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
	type: string,
): Promise<GraphNode> {
	const node = (await nodesOf(request, projectId, automationId)).find(
		(n) => n.type === type,
	);
	expect(node, `a ${type} node`).toBeTruthy();
	return node as GraphNode;
}

async function statusId(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<string> {
	const data = await api(
		request,
		"get",
		`/projects/${projectId}/task-statuses`,
	);
	const status = (data.items as Array<{ id: string; name: string }>).find(
		(s) => s.name === name,
	);
	expect(status, `status ${name}`).toBeTruthy();
	return (status as { id: string }).id;
}

async function createTask(
	request: APIRequestContext,
	projectId: string,
	body: Record<string, unknown>,
): Promise<string> {
	return (await api(request, "post", `/projects/${projectId}/tasks`, body)).id;
}

async function getTask(
	request: APIRequestContext,
	projectId: string,
	taskId: string,
): Promise<Record<string, any>> {
	return api(request, "get", `/projects/${projectId}/tasks/${taskId}`);
}

async function setStatus(
	request: APIRequestContext,
	projectId: string,
	taskId: string,
	status: string,
): Promise<void> {
	await api(request, "patch", `/projects/${projectId}/tasks/${taskId}`, {
		status_id: await statusId(request, projectId, status),
	});
}

async function adminMemberId(
	request: APIRequestContext,
	projectId: string,
): Promise<string> {
	const data = await api(request, "get", `/projects/${projectId}/members`);
	const members = data.items as Array<{ id: string; username: string }>;
	const admin = members.find(
		(m) => m.username === (process.env.E2E_USERNAME ?? "admin"),
	);
	expect(admin, "the admin as a project member").toBeTruthy();
	return (admin as { id: string }).id;
}

async function childrenOf(
	request: APIRequestContext,
	projectId: string,
	parentId: string,
): Promise<Array<Record<string, any>>> {
	const data = await api(
		request,
		"get",
		`/projects/${projectId}/tasks?parent_task_id=${parentId}`,
	);
	return (data.items as Array<Record<string, any>>).filter(
		(t) => t.parent_task_id === parentId,
	);
}

// ─── UI helpers ───────────────────────────────────────────────────────────────

async function openBuilder(
	page: Page,
	projectId: string,
	automationId: string,
	name: string,
): Promise<void> {
	await page.goto(
		`${BASE_URL}/projects/${projectId}/automation/${automationId}`,
	);
	await expect(page.getByRole("heading", { name })).toBeVisible();
}

async function addFromPalette(
	page: Page,
	button: "Add Trigger" | "Add Condition" | "Add Action",
	item: string,
): Promise<void> {
	await page.getByRole("button", { name: button }).click();
	await page.getByRole("menuitem", { name: item, exact: true }).click();
}

function selectTrigger(page: Page, text: string) {
	return page.locator('[data-slot="select-trigger"]').filter({ hasText: text });
}

async function choose(
	page: Page,
	trigger: ReturnType<typeof selectTrigger>,
	option: string,
) {
	await trigger.first().click();
	await page.getByRole("option", { name: option, exact: true }).click();
}

async function save(page: Page): Promise<void> {
	await page.getByRole("button", { name: "Save", exact: true }).click();
}

// ─── Tests ────────────────────────────────────────────────────────────────────

test.describe("Automation nodes — fields, subtasks, links, comments and notifications", () => {
	let projectId: string;
	let automationId: string;
	let automationName: string;

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
		projectCounter += 1;
		projectId = await createProject(
			request,
			`${PROJECT_PREFIX}${RUN_ID}_${projectCounter}`,
		);
		automationName = `${PROJECT_PREFIX}GRAPH_${RUN_ID}_${projectCounter}`;
		automationId = await createAutomation(request, projectId, automationName);
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PROJECT_PREFIX);
	});

	// ── Palette ───────────────────────────────────────────────────────────────
	test.describe("The node palette", () => {
		test("groups the actions and offers the new ones", async ({ page }) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);

			await page.getByRole("button", { name: "Add Action" }).click();
			for (const group of ["Task", "Flow & integrations", "Sprint"]) {
				await expect(page.getByText(group, { exact: true })).toBeVisible();
			}
			for (const action of [
				"Update task",
				"Create subtask",
				"Link task",
				"Add comment",
				"Send notification",
			]) {
				await expect(
					page.getByRole("menuitem", { name: action, exact: true }),
				).toBeVisible();
			}
		});

		test("offers the new triggers", async ({ page }) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);

			await page.getByRole("button", { name: "Add Trigger" }).click();
			await expect(
				page.getByText("Activity & subtasks", { exact: true }),
			).toBeVisible();
			for (const trigger of [
				"Field changed",
				"Subtask changed",
				"Comment added",
				"Link added",
				"Attachment added",
			]) {
				await expect(
					page.getByRole("menuitem", { name: trigger, exact: true }),
				).toBeVisible();
			}
		});
	});

	// ── Action forms ──────────────────────────────────────────────────────────
	test.describe("Configuring the new actions in the builder", () => {
		test("add comment stores its text and cannot be saved empty", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Action", "Add comment");

			await expect(
				page.getByRole("button", { name: "Save", exact: true }),
			).toBeDisabled();
			await page
				.getByRole("textbox", { name: "Comment" })
				.fill("Done: {{task.title}}");
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "add_comment"))
							.config.comment?.text,
				)
				.toBe("Done: {{task.title}}");
		});

		test("send notification needs a recipient and a message", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Action", "Send notification");

			const saveButton = page.getByRole("button", {
				name: "Save",
				exact: true,
			});
			await page
				.getByRole("textbox", { name: "Message" })
				.fill("Ready for review");
			await expect(saveButton).toBeDisabled(); // a message alone is not enough

			await page
				.getByRole("switch", { name: "Assignees", exact: true })
				.click();
			await expect(saveButton).toBeEnabled();
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "notify"))
							.config.notify,
				)
				.toMatchObject({
					recipients: ["assignees"],
					message: "Ready for review",
				});
		});

		test("link task stores the link type and the task to link to", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Action", "Link task");

			await choose(page, selectTrigger(page, "Relates to"), "Blocks");
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "link_task"))
							.config.link,
				)
				.toMatchObject({ type: "blocks", to: { kind: "parent" } });
		});

		test("create subtask stores its title, a copied field and a link to the parent", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Action", "Create subtask");

			await page
				.getByPlaceholder("{{task.title}} - Review")
				.fill("{{task.title}} - QA");

			// Assignee copied from the triggering task instead of a fixed member.
			await choose(
				page,
				selectTrigger(page, "Add field to update"),
				"Assignee",
			);
			await page.getByLabel("How to apply this field").click();
			await page
				.getByRole("option", { name: "Copy from this task", exact: true })
				.click();

			await choose(page, selectTrigger(page, "No link"), "Blocks");
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "create_task"))
							.config.create,
				)
				.toMatchObject({
					title: "{{task.title}} - QA",
					link: "blocks",
					dynamic: { assignee_ids: { source: "task" } },
				});
		});
	});

	// ── update_task field operations ──────────────────────────────────────────
	test.describe("Field operations on Update task", () => {
		test("tags can be added instead of replaced", async ({ page, request }) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Action", "Update task");

			await choose(page, selectTrigger(page, "Add field to update"), "Tags");
			await page.getByLabel("How to apply this field").click();
			await page.getByRole("option", { name: "Add", exact: true }).click();
			await page.getByPlaceholder("Tag").fill("needs-review");
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "update_task"))
							.config.update,
				)
				.toMatchObject({ tags: ["needs-review"], tags_mode: "add" });
		});

		test("a date can be cleared, and no value is stored for it", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Action", "Update task");

			await choose(
				page,
				selectTrigger(page, "Add field to update"),
				"Due date",
			);
			await page.getByLabel("How to apply this field").click();
			await page.getByRole("option", { name: "Clear", exact: true }).click();
			await expect(page.getByText("Empties this field.")).toBeVisible();
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "update_task"))
							.config.update,
				)
				.toEqual({ clear: ["due_date"] });
		});

		test("a due date can be computed from today with an offset", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Action", "Update task");

			await choose(
				page,
				selectTrigger(page, "Add field to update"),
				"Due date",
			);
			await page.getByLabel("How to apply this field").click();
			await page.getByRole("option", { name: "Today", exact: true }).click();
			await page.getByRole("spinbutton").fill("3");
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "update_task"))
							.config.update,
				)
				.toEqual({ dynamic: { due_date: { source: "now", offset_days: 3 } } });
		});

		test("a saved add / clear / copy configuration reopens as it was saved", async ({
			page,
			request,
		}) => {
			await addNode(request, projectId, automationId, {
				kind: "action",
				type: "update_task",
				config: {
					update: {
						tags: ["x"],
						tags_mode: "remove",
						clear: ["sprint_id"],
						dynamic: { assignee_ids: { source: "parent" } },
					},
				},
			});
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await page.getByTitle("Update task", { exact: true }).click();

			// One row per field, each showing the operation that was saved.
			const operations = page.getByLabel("How to apply this field");
			await expect(operations).toHaveCount(3);
			await expect(page.getByText("Remove", { exact: true })).toBeVisible();
			await expect(page.getByText("Clear", { exact: true })).toBeVisible();
			await expect(
				page.getByText("Copy from parent", { exact: true }),
			).toBeVisible();

			// Saving without touching anything keeps the configuration intact.
			await save(page);
			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "update_task"))
							.config.update,
				)
				.toEqual({
					tags: ["x"],
					tags_mode: "remove",
					clear: ["sprint_id"],
					dynamic: { assignee_ids: { source: "parent" } },
				});
		});
	});

	// ── Triggers ──────────────────────────────────────────────────────────────
	test.describe("Configuring the new triggers in the builder", () => {
		test("field changed watches one field", async ({ page, request }) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Trigger", "Field changed");

			await expect(
				page.getByRole("button", { name: "Save", exact: true }),
			).toBeDisabled();
			await choose(page, selectTrigger(page, "Choose a field"), "Sprint");
			await save(page);

			await expect
				.poll(
					async () =>
						(
							await nodeOfType(
								request,
								projectId,
								automationId,
								"field_changed",
							)
						).config,
				)
				.toEqual({ field: "sprint" });
			await expect(page.getByTitle("Sprint", { exact: true })).toBeVisible();
		});

		test("subtask changed can be narrowed to subtasks entering a status", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await addFromPalette(page, "Add Trigger", "Subtask changed");

			await expect(
				page.getByText("Runs on the parent task", { exact: false }),
			).toBeVisible();
			await choose(page, selectTrigger(page, "Any status"), "Done");
			await save(page);

			const done = await statusId(request, projectId, "Done");
			await expect
				.poll(
					async () =>
						(
							await nodeOfType(
								request,
								projectId,
								automationId,
								"child_changed",
							)
						).config,
				)
				.toEqual({ status_id: done });
		});
	});

	// ── Conditions ────────────────────────────────────────────────────────────
	test.describe("Configuring the new conditions in the builder", () => {
		test("a branch can compare the subtask count", async ({
			page,
			request,
		}) => {
			await addNode(request, projectId, automationId, {
				kind: "condition",
				type: "condition",
			});
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await page.getByTitle("Condition", { exact: true }).click();

			await page.getByRole("button", { name: "Add branch" }).click();
			await choose(
				page,
				selectTrigger(page, "Status").first(),
				"Subtask count",
			);
			await choose(page, selectTrigger(page, "equals"), "greater_than");
			await page.getByRole("spinbutton").fill("1");
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "condition"))
							.config.branches?.[0]?.tree,
				)
				.toMatchObject({
					field: "child_count",
					operator: "greater_than",
					value: 1,
				});
		});

		test("a date branch can be relative to today", async ({
			page,
			request,
		}) => {
			await addNode(request, projectId, automationId, {
				kind: "condition",
				type: "condition",
			});
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);
			await page.getByTitle("Condition", { exact: true }).click();

			await page.getByRole("button", { name: "Add branch" }).click();
			await choose(page, selectTrigger(page, "Status").first(), "Due date");
			await choose(page, selectTrigger(page, "equals"), "less_than");
			await choose(
				page,
				selectTrigger(page, "Specific date"),
				"Relative to today",
			);
			await page.getByRole("spinbutton").fill("3");
			await save(page);

			await expect
				.poll(
					async () =>
						(await nodeOfType(request, projectId, automationId, "condition"))
							.config.branches?.[0]?.tree,
				)
				.toMatchObject({
					field: "due_date",
					operator: "less_than",
					value: "now+3d",
				});
		});
	});

	// ── Engine scenarios ──────────────────────────────────────────────────────
	test.describe("The stored configuration does its job", () => {
		test("a new task gets a templated subtask that inherits fields and does not recurse", async ({
			request,
		}) => {
			const admin = await adminMemberId(request, projectId);
			const trigger = await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "task_created",
			});
			const action = await addNode(request, projectId, automationId, {
				kind: "action",
				type: "create_task",
				config: {
					create: {
						title: "{{task.title}} - QA",
						dynamic: { assignee_ids: { source: "task" } },
						link: "blocks",
					},
				},
			});
			await addEdge(request, projectId, automationId, trigger, action);
			await activate(request, projectId, automationId);

			const parent = await createTask(request, projectId, {
				title: "Login page",
				assignee_ids: [admin],
			});

			await expect
				.poll(async () =>
					(await childrenOf(request, projectId, parent)).map((c) => c.title),
				)
				.toEqual(["Login page - QA"]);
			const [child] = await childrenOf(request, projectId, parent);
			expect(child.assignee_ids).toEqual([admin]);

			// Still exactly one subtask a few seconds later: the created task did not
			// fire "task created" again.
			await new Promise((resolve) => setTimeout(resolve, 3000));
			expect(await childrenOf(request, projectId, parent)).toHaveLength(1);
		});

		test("the parent closes once the last subtask is done", async ({
			request,
		}) => {
			const done = await statusId(request, projectId, "Done");
			const trigger = await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "status_changed",
				config: { status_id: done },
			});
			const condition = await addNode(request, projectId, automationId, {
				kind: "condition",
				type: "condition",
				config: {
					branches: [
						{
							handle: "all_done",
							label: "All sibling tasks done",
							tree: {
								field: "status_id",
								operator: "equals",
								value: done,
								target: { kind: "siblings" },
								match_mode: "all",
							},
						},
					],
				},
			});
			const action = await addNode(request, projectId, automationId, {
				kind: "action",
				type: "update_task",
				config: { update: { status_id: done }, target: { kind: "parent" } },
			});
			await addEdge(request, projectId, automationId, trigger, condition);
			await addEdge(
				request,
				projectId,
				automationId,
				condition,
				action,
				"all_done",
			);
			await activate(request, projectId, automationId);

			const parent = await createTask(request, projectId, { title: "Parent" });
			const first = await createTask(request, projectId, {
				title: "First",
				parent_task_id: parent,
			});
			const second = await createTask(request, projectId, {
				title: "Second",
				parent_task_id: parent,
			});

			await setStatus(request, projectId, first, "Done");
			await new Promise((resolve) => setTimeout(resolve, 2500));
			expect((await getTask(request, projectId, parent)).status_id).not.toBe(
				done,
			);

			await setStatus(request, projectId, second, "Done");
			await expect
				.poll(async () => (await getTask(request, projectId, parent)).status_id)
				.toBe(done);
		});

		test("a notify action reaches the assignee's notification bell", async ({
			page,
			request,
		}) => {
			const admin = await adminMemberId(request, projectId);
			const inProgress = await statusId(request, projectId, "In Progress");
			const trigger = await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "status_changed",
				config: { status_id: inProgress },
			});
			const action = await addNode(request, projectId, automationId, {
				kind: "action",
				type: "notify",
				config: {
					notify: { recipients: ["assignees"], message: "Ready for review" },
				},
			});
			await addEdge(request, projectId, automationId, trigger, action);
			await activate(request, projectId, automationId);

			const task = await createTask(request, projectId, {
				title: "Notify me",
				assignee_ids: [admin],
			});
			await setStatus(request, projectId, task, "In Progress");

			await signIn(page);
			await page.getByRole("button", { name: "Notifications" }).click();
			await expect(
				page.getByText(/Automation: Ready for review/).first(),
			).toBeVisible({
				timeout: 20_000,
			});
		});

		test("adding a tag keeps the existing ones", async ({ request }) => {
			const inProgress = await statusId(request, projectId, "In Progress");
			const trigger = await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "status_changed",
				config: { status_id: inProgress },
			});
			const action = await addNode(request, projectId, automationId, {
				kind: "action",
				type: "update_task",
				config: { update: { tags: ["added"], tags_mode: "add" } },
			});
			await addEdge(request, projectId, automationId, trigger, action);
			await activate(request, projectId, automationId);

			const task = await createTask(request, projectId, {
				title: "Tagged",
				tags: ["existing"],
			});
			await setStatus(request, projectId, task, "In Progress");

			await expect
				.poll(async () =>
					((await getTask(request, projectId, task)).tags as string[])
						.slice()
						.sort(),
				)
				.toEqual(["added", "existing"]);
		});
	});
});
