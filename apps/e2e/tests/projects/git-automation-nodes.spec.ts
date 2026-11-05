// spec: features/projects/git-automation-nodes.feature
// seed: tests/seed.spec.ts
//
// The e2e stack has no Git provider plugin, so no webhook can be delivered.
// The specs that run a graph put the linked pull request in the database and
// the event core would have queued on the trigger stream (tests/helpers/
// git-stack.ts); that is the whole path from the stream to the finished run.
// Webhook -> event is covered by the API's own tests.

import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";
import {
	emitGitEvent,
	seedChangeRequest,
	setChangeRequestState,
	stackReachable,
} from "../helpers/git-stack";
import { createTask, dataOf, errorCode } from "../helpers/iam";

const PREFIX = "E2E_GITNODES_";
const RUN_ID = newRunId();

type Kind = "trigger" | "condition" | "action";

interface NodeType {
	type: string;
	label: string;
	configSchema?: { properties?: Record<string, { enum?: string[] }> };
	eventTopic?: string;
	taskless?: boolean;
}

test.describe("Git nodes in the automation graph", () => {
	let projectId: string;
	let counter = 0;

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		counter += 1;
		projectId = await createProject(request, `${PREFIX}${RUN_ID}_${counter}`);
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
	});

	const api = (path: string) => `${API_URL}/projects/${projectId}${path}`;

	async function createAutomation(
		request: APIRequestContext,
		name = `${PREFIX}AUTO_${RUN_ID}`,
	): Promise<string> {
		const response = await request.post(api("/automations"), {
			data: { name },
		});
		expect(response.ok(), await response.text()).toBeTruthy();
		return (await dataOf<{ id: string }>(response)).id;
	}

	async function addNode(
		request: APIRequestContext,
		automationId: string,
		kind: Kind,
		type: string,
		config: object = {},
	) {
		return request.post(api(`/automations/${automationId}/nodes`), {
			data: { kind, type, config, pos_x: 120, pos_y: 120 },
		});
	}

	async function node(
		request: APIRequestContext,
		automationId: string,
		kind: Kind,
		type: string,
		config: object = {},
	): Promise<string> {
		const response = await addNode(request, automationId, kind, type, config);
		expect(response.ok(), await response.text()).toBeTruthy();
		return (await dataOf<{ id: string }>(response)).id;
	}

	function edge(
		request: APIRequestContext,
		automationId: string,
		from: string,
		to: string,
		handle?: string,
	) {
		return request.post(api(`/automations/${automationId}/edges`), {
			data: {
				source_node_id: from,
				target_node_id: to,
				...(handle ? { source_handle: handle } : {}),
			},
		});
	}

	async function activate(request: APIRequestContext, automationId: string) {
		const response = await request.post(
			api(`/automations/${automationId}/activate`),
		);
		expect(response.ok(), await response.text()).toBeTruthy();
	}

	// ── Rule: The palette offers the Git nodes ────────────────────────────────

	test.describe("The palette offers the Git nodes", () => {
		test("every Git node type is listed under its kind", async ({
			request,
		}) => {
			const response = await request.get(api("/automation-plugin-node-types"));
			expect(response.ok()).toBeTruthy();
			const types = await dataOf<{
				triggers: NodeType[];
				conditions: NodeType[];
				actions: NodeType[];
			}>(response);

			const labels = (list: NodeType[]) => list.map((t) => t.label);
			expect(labels(types.triggers)).toEqual(
				expect.arrayContaining([
					"Git: Pull Request Created",
					"Git: Pull Request State Changed",
					"Git: Pull Request Ready for Review",
					"Git: Pull Request Review Submitted",
					"Git: Pull Request Commented",
					"Git: Pull Request CI Completed",
					"Git: Branch Created",
					"Git: Issue Created",
				]),
			);
			expect(labels(types.conditions)).toEqual(
				expect.arrayContaining([
					"Git: Pull Request State",
					"Git: Pull Request CI Status",
					"Git: Pull Request Draft",
					"Git: Pull Request Target Branch",
					"Git: Task Has Pull Request",
				]),
			);
			expect(labels(types.actions)).toEqual(
				expect.arrayContaining([
					"Git: Merge Pull Request",
					"Git: Comment on Pull Request",
					"Git: Close Pull Request",
					"Git: Reopen Pull Request",
					"Git: Mark Pull Request Ready for Review",
					"Git: Add Labels to Pull Request",
					"Git: Request Reviewers",
				]),
			);

			// Only the issue trigger carries no task.
			const taskless = types.triggers.filter((t) => t.taskless);
			expect(taskless.map((t) => t.type)).toEqual(["git.issue_created"]);
			// Every trigger names the topic its events arrive on.
			for (const t of types.triggers.filter((t) => t.type.startsWith("git."))) {
				expect(t.eventTopic).toBe(t.type);
			}
			// A provider's own nodes are gone: no node type is GitHub-only.
			for (const t of [
				...types.triggers,
				...types.conditions,
				...types.actions,
			]) {
				expect(t.type).not.toMatch(/^github\./);
			}
		});

		test("the state condition's form offers open, closed and merged", async ({
			request,
		}) => {
			const types = await dataOf<{ conditions: NodeType[] }>(
				await request.get(api("/automation-plugin-node-types")),
			);
			const state = types.conditions.find((c) => c.type === "git.pr_state");
			expect(state?.configSchema?.properties?.expected_state?.enum).toEqual([
				"open",
				"closed",
				"merged",
			]);
		});

		test("the builder's Plugins section lists the Git triggers", async ({
			page,
			request,
		}) => {
			const name = `${PREFIX}PALETTE_${RUN_ID}`;
			const automationId = await createAutomation(request, name);
			await signIn(page);
			await page.goto(
				`${BASE_URL}/projects/${projectId}/automation/${automationId}`,
			);
			await expect(page.getByRole("heading", { name })).toBeVisible();

			await page.getByRole("button", { name: "Add Trigger" }).click();
			await expect(
				page.getByRole("menuitem", {
					name: "Pull Request State Changed",
					exact: true,
				}),
			).toBeVisible();
			await expect(
				page.getByRole("menuitem", { name: "Issue Created", exact: true }),
			).toBeVisible();
		});

		test("adding a Git trigger from the palette places it on the canvas", async ({
			page,
			request,
		}) => {
			const name = `${PREFIX}ADD_${RUN_ID}`;
			const automationId = await createAutomation(request, name);
			await signIn(page);
			await page.goto(
				`${BASE_URL}/projects/${projectId}/automation/${automationId}`,
			);
			await expect(page.getByRole("heading", { name })).toBeVisible();

			await page.getByRole("button", { name: "Add Trigger" }).click();
			await page
				.getByRole("menuitem", { name: "Issue Created", exact: true })
				.click();

			await expect(
				page.getByTitle("Git: Issue Created", { exact: true }),
			).toBeVisible();
		});
	});

	// ── Rule: The graph is validated ──────────────────────────────────────────

	test.describe("The graph is validated", () => {
		test("the old GitHub plugin node types are refused", async ({
			request,
		}) => {
			const automationId = await createAutomation(request);
			for (const [kind, type] of [
				["trigger", "github.pr_state_changed"],
				["trigger", "github.pr_linked"],
				["condition", "github.pr_state"],
				["action", "github.merge_pr"],
			] as const) {
				const response = await addNode(request, automationId, kind, type);
				expect(response.status(), type).toBe(400);
				expect(await errorCode(response), type).toBe(
					"AUTOMATION_NODE_INVALID_TYPE",
				);
			}
		});

		test("an issue belongs to no task, so it reaches only task-less nodes", async ({
			request,
		}) => {
			const automationId = await createAutomation(request);
			const issue = await node(
				request,
				automationId,
				"trigger",
				"git.issue_created",
			);
			const wait = await node(request, automationId, "action", "wait", {
				wait_minutes: 5,
			});
			const call = await node(request, automationId, "action", "call_api");
			const agent = await node(
				request,
				automationId,
				"action",
				"trigger_ai_agent",
			);
			const update = await node(request, automationId, "action", "update_task");
			const condition = await node(
				request,
				automationId,
				"condition",
				"git.pr_state",
				{ expected_state: "merged" },
			);

			// Nodes that work without a task are allowed: wait, call_api and an
			// agent message.
			for (const target of [wait, call, agent]) {
				expect(
					(await edge(request, automationId, issue, target)).ok(),
				).toBeTruthy();
			}
			for (const target of [update, condition]) {
				const refused = await edge(request, automationId, issue, target);
				expect(refused.status()).toBe(400);
				expect(await errorCode(refused)).toBe(
					"AUTOMATION_EDGE_REQUIRES_TARGET_TASK",
				);
			}
		});

		test("a pull request trigger reaches the condition and, through it, task nodes", async ({
			request,
		}) => {
			const automationId = await createAutomation(request);
			const trigger = await node(
				request,
				automationId,
				"trigger",
				"git.pr_state_changed",
			);
			const condition = await node(
				request,
				automationId,
				"condition",
				"git.pr_state",
				{ expected_state: "merged" },
			);
			const update = await node(request, automationId, "action", "update_task");

			expect(
				(await edge(request, automationId, trigger, condition)).ok(),
			).toBeTruthy();
			// A plugin-style condition is a gate: it needs its "true" branch.
			const missing = await edge(request, automationId, condition, update);
			expect(missing.status()).toBe(400);
			expect(await errorCode(missing)).toBe("AUTOMATION_EDGE_HANDLE_REQUIRED");
			expect(
				(await edge(request, automationId, condition, update, "true")).ok(),
			).toBeTruthy();
		});
	});

	// ── Rule: A graph runs when the event arrives ─────────────────────────────

	test.describe("A graph runs when the event arrives", () => {
		test.beforeAll(() => {
			test.skip(
				!stackReachable(),
				"needs the e2e docker stack (deploy/docker-compose.e2e.yml) reachable through docker compose",
			);
		});

		/** Two different statuses of the project: where a task starts and where the automation sends it. */
		async function statuses(request: APIRequestContext) {
			const response = await request.get(api("/task-statuses"));
			const items = (await response.json()).data.items as Array<{
				id: string;
				name: string;
			}>;
			expect(items.length).toBeGreaterThan(1);
			return items;
		}

		async function statusOf(
			request: APIRequestContext,
			taskId: string,
		): Promise<string> {
			const response = await request.get(api(`/tasks/${taskId}`));
			return (await dataOf<{ status_id: string }>(response)).status_id;
		}

		/** A task and the status the automation will move it to (never its own). */
		async function taskAndTarget(request: APIRequestContext) {
			const taskId = await createTask(
				request,
				projectId,
				"Ship the login page",
			);
			const current = await statusOf(request, taskId);
			const target = (await statuses(request)).find((s) => s.id !== current);
			if (!target) throw new Error("the project has a single status");
			return { taskId, current, target: target.id };
		}

		async function runsOf(request: APIRequestContext, automationId: string) {
			const response = await request.get(
				api(`/automations/${automationId}/runs`),
			);
			return (
				await dataOf<{
					items: Array<{ id: string; status: string; task_id: string | null }>;
				}>(response)
			).items;
		}

		async function stepsOf(
			request: APIRequestContext,
			automationId: string,
			runId: string,
		) {
			const response = await request.get(
				api(`/automations/${automationId}/runs/${runId}/steps`),
			);
			return (
				await dataOf<{
					items: Array<{ node_id: string; status: string; error?: string }>;
				}>(response)
			).items;
		}

		/** "state changed -> state is <expected> -> update the status". */
		async function mergedGraph(request: APIRequestContext, target: string) {
			const automationId = await createAutomation(request);
			const trigger = await node(
				request,
				automationId,
				"trigger",
				"git.pr_state_changed",
			);
			const condition = await node(
				request,
				automationId,
				"condition",
				"git.pr_state",
				{ expected_state: "merged" },
			);
			const update = await node(
				request,
				automationId,
				"action",
				"update_task",
				{
					update: { status_id: target },
				},
			);
			await edge(request, automationId, trigger, condition);
			await edge(request, automationId, condition, update, "true");
			await activate(request, automationId);
			return { automationId, update };
		}

		test("a merged pull request sets the task to the other status", async ({
			request,
		}) => {
			const { taskId, target } = await taskAndTarget(request);
			const { automationId, update } = await mergedGraph(request, target);
			seedChangeRequest({ projectId, taskId, number: "7", state: "merged" });

			emitGitEvent("git.pr_state_changed", {
				project_id: projectId,
				task_id: taskId,
				number: "7",
				state: "merged",
				previous_state: "open",
			});

			await expect.poll(() => statusOf(request, taskId)).toBe(target);
			await expect
				.poll(async () => (await runsOf(request, automationId))[0]?.status)
				.toBe("completed");
			const [run] = await runsOf(request, automationId);
			const steps = await stepsOf(request, automationId, run.id);
			expect(steps.find((s) => s.node_id === update)?.status).toBe("completed");
		});

		test("the same automation follows the pull request as it changes state", async ({
			request,
		}) => {
			const { taskId, current, target } = await taskAndTarget(request);
			const { automationId } = await mergedGraph(request, target);
			const cr = seedChangeRequest({
				projectId,
				taskId,
				number: "8",
				state: "open",
			});
			const event = {
				project_id: projectId,
				task_id: taskId,
				number: "8",
				previous_state: "open",
			};

			// Closed without merging: the condition does not match.
			setChangeRequestState(cr.changeRequestId, "closed");
			emitGitEvent("git.pr_state_changed", { ...event, state: "closed" });
			await expect
				.poll(async () => (await runsOf(request, automationId)).length)
				.toBe(1);
			expect(await statusOf(request, taskId)).toBe(current);
			const [closedRun] = await runsOf(request, automationId);
			const steps = await stepsOf(request, automationId, closedRun.id);
			expect(steps.some((s) => s.status === "failed")).toBeFalsy();

			// Then merged: the status moves.
			setChangeRequestState(cr.changeRequestId, "merged");
			emitGitEvent("git.pr_state_changed", { ...event, state: "merged" });
			await expect.poll(() => statusOf(request, taskId)).toBe(target);
		});

		test("a pull request that is not merged leaves the task alone", async ({
			request,
		}) => {
			const { taskId, current, target } = await taskAndTarget(request);
			const { automationId, update } = await mergedGraph(request, target);
			seedChangeRequest({ projectId, taskId, number: "9", state: "open" });

			emitGitEvent("git.pr_state_changed", {
				project_id: projectId,
				task_id: taskId,
				number: "9",
				state: "open",
			});

			await expect
				.poll(async () => (await runsOf(request, automationId)).length)
				.toBe(1);
			const [run] = await runsOf(request, automationId);
			const steps = await stepsOf(request, automationId, run.id);
			expect(steps.find((s) => s.node_id === update)).toBeUndefined();
			expect(await statusOf(request, taskId)).toBe(current);
		});

		test("a new pull request runs its automation", async ({ request }) => {
			const { taskId, target } = await taskAndTarget(request);
			const automationId = await createAutomation(request);
			const trigger = await node(
				request,
				automationId,
				"trigger",
				"git.pr_linked",
			);
			const update = await node(
				request,
				automationId,
				"action",
				"update_task",
				{
					update: { status_id: target },
				},
			);
			await edge(request, automationId, trigger, update);
			await activate(request, automationId);

			emitGitEvent("git.pr_linked", {
				project_id: projectId,
				task_id: taskId,
				number: "3",
				state: "open",
			});

			await expect.poll(() => statusOf(request, taskId)).toBe(target);
		});

		test("another project's automation is not run by the event", async ({
			request,
		}) => {
			const { taskId, target } = await taskAndTarget(request);
			const { automationId } = await mergedGraph(request, target);
			const otherProject = await createProject(
				request,
				`${PREFIX}${RUN_ID}_other_${counter}`,
			);
			emitGitEvent("git.pr_state_changed", {
				project_id: otherProject,
				task_id: taskId,
				state: "merged",
			});

			// Give the engine the time it needs to have run it, had it matched.
			await new Promise((resolve) => setTimeout(resolve, 3_000));
			expect(await runsOf(request, automationId)).toHaveLength(0);
		});

		test("an opened issue runs a task-less automation", async ({ request }) => {
			const automationId = await createAutomation(request);
			const trigger = await node(
				request,
				automationId,
				"trigger",
				"git.issue_created",
			);
			const wait = await node(request, automationId, "action", "wait", {
				wait_minutes: 5,
			});
			await edge(request, automationId, trigger, wait);
			await activate(request, automationId);

			emitGitEvent("git.issue_created", {
				project_id: projectId,
				number: "12",
				title: "Crash on login",
				text: "Crash on login\n\nSteps: open the app",
				url: "https://git.example.test/acme/web/issues/12",
				author: "ann",
			});

			await expect
				.poll(async () => (await runsOf(request, automationId)).length)
				.toBe(1);
			const [run] = await runsOf(request, automationId);
			expect(run.task_id).toBeNull();
			// The run waits at the wait node (it resumes in five minutes).
			await expect
				.poll(async () =>
					(await stepsOf(request, automationId, run.id)).map((s) => s.node_id),
				)
				.toContain(wait);
		});
	});
});
