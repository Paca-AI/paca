// spec: features/realtime/iam-changes.feature
// seed: tests/seed.spec.ts
//
// What a socket may receive follows the account's roles as they are NOW, not as
// they were when it connected. When a role is edited, attached, detached or
// deleted, or a member or a user is removed, the API announces it and the
// realtime service re-checks every open socket: rooms are re-synced, cached
// decisions are dropped, and the client is told with `permissions:changed`.
//
// Every spec keeps ONE socket open across the change (its id never changes) and
// asserts on what that same socket receives before and after.

import {
	type APIRequestContext,
	expect,
	type PlaywrightWorkerArgs,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createProject,
	newRunId,
} from "../helpers/e2e-api";
import {
	allow,
	createDoc,
	createMember,
	createSprint,
	createTask,
	deny,
	policyOf,
	projectRoleFrom,
	putMemberRoles,
	putUserRoles,
} from "../helpers/iam";
import {
	type Connected,
	closeAll,
	connectAs,
	cookieHeaderOf,
	Probe,
	sleep,
	track,
} from "../helpers/realtime";

const PREFIX = "E2E_RTIAM_";
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
	await closeAll();
	await authRequest(request);
	await cleanup(request);
});

// ─── Arrangement helpers ─────────────────────────────────────────────────────

const res = (projectId: string) => `project/${projectId}`;
const open = (projectId: string) => allow(["projects:read"], [res(projectId)]);
const readAll = (projectId: string, ...domains: string[]) =>
	allow(
		["projects:read", ...domains.map((d) => `${d}:read`)],
		[`${res(projectId)}/*`],
	);

interface Scene {
	projectId: string;
	roleId: string;
	roleName: string;
	member: { username: string; userId: string; memberId: string };
	bob: Connected;
}

/** A project, a member with one project role made of `statements`, and their joined socket. */
async function scene(
	request: APIRequestContext,
	playwright: PlaywrightWorkerArgs["playwright"],
	label: string,
	statementsFor: (projectId: string) => ReturnType<typeof allow>[],
): Promise<Scene> {
	await authRequest(request);
	const projectId = await createProject(request, name(label));
	const roleName = name(`role_${label}`);
	const roleId = await projectRoleFrom(
		request,
		projectId,
		roleName,
		...statementsFor(projectId),
	);
	const member = await createMember(
		request,
		playwright,
		projectId,
		name(`bob_${label}`),
		[roleId],
	);
	const bob = await connectAs(playwright, member.username, projectId);
	return { projectId, roleId, roleName, member, bob };
}

/** Replaces the policy of a project role. */
async function setPolicy(
	request: APIRequestContext,
	s: Pick<Scene, "projectId" | "roleId" | "roleName">,
	...statements: ReturnType<typeof allow>[]
) {
	const response = await request.put(
		`${API_URL}/projects/${s.projectId}/roles/${s.roleId}`,
		{
			data: {
				name: s.roleName,
				description: "",
				policy: policyOf(...statements),
			},
		},
	);
	expect(response.ok(), await response.text()).toBeTruthy();
}

async function patchTask(
	request: APIRequestContext,
	projectId: string,
	taskId: string,
	title: string,
) {
	const response = await request.patch(
		`${API_URL}/projects/${projectId}/tasks/${taskId}`,
		{ data: { title } },
	);
	expect(response.ok(), await response.text()).toBeTruthy();
}

/** Waits for the next `permissions:changed` since `since` and returns it. */
async function nextChange(probe: Probe, since: number, timeout = 10_000) {
	await expect.poll(() => probe.lastChange(since), { timeout }).toBeTruthy();
	return probe.lastChange(since) as NonNullable<
		ReturnType<Probe["lastChange"]>
	>;
}

/** Waits until the newest `permissions:changed` satisfies `ok`. */
async function changeMatching(
	probe: Probe,
	since: number,
	ok: (namespaces: string[]) => boolean,
) {
	await expect
		.poll(
			() => {
				const ns = probe.lastChange(since)?.namespaces;
				return ns !== undefined && ok(ns);
			},
			{
				timeout: 10_000,
				message: "the awaited permissions:changed never arrived",
			},
		)
		.toBe(true);
	return probe.lastChange(since)?.namespaces as string[];
}

async function expectDelivered(probe: Probe, taskId: string, since: number) {
	await expect
		.poll(() => probe.eventsAbout(taskId, since).length, { timeout: 10_000 })
		.toBeGreaterThan(0);
}

/** Nothing about `taskId` reached `probe`; waits on an allowed socket first. */
async function expectHeldBack(
	probe: Probe,
	control: Probe,
	taskId: string,
	since: number,
) {
	await expect
		.poll(() => control.eventsAbout(taskId, since).length, { timeout: 10_000 })
		.toBeGreaterThan(0);
	await sleep(400);
	expect(probe.eventsAbout(taskId, since)).toEqual([]);
}

async function adminProbe(request: APIRequestContext, projectId: string) {
	const probe = track(
		await Probe.connect({ cookie: await cookieHeaderOf(request) }),
	);
	await probe.join(projectId);
	return probe;
}

// ─── Editing a role ──────────────────────────────────────────────────────────

test.describe("Editing a role the member holds", () => {
	test("removing a Deny starts delivery and adding it back stops it at once", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "deny", (p) => [
			readAll(p, "tasks"),
		]);
		const a = await createTask(request, s.projectId, name("a"));
		const b = await createTask(request, s.projectId, name("b"));
		const admin = await adminProbe(request, s.projectId);
		const socketId = s.bob.probe.socket.id;

		// Nothing denied yet: both tasks reach bob (and the answer is now cached).
		let since = Date.now();
		await patchTask(request, s.projectId, b, name("x"));
		await expectDelivered(s.bob.probe, b, since);

		// Deny B: the very next event is held back although "allowed" was cached.
		const before = Date.now();
		await setPolicy(
			request,
			s,
			readAll(s.projectId, "tasks"),
			deny(["tasks:read"], [`${res(s.projectId)}/task/${b}`]),
		);
		await nextChange(s.bob.probe, before);
		since = Date.now();
		await patchTask(request, s.projectId, b, name("y"));
		await patchTask(request, s.projectId, a, name("z"));
		await expectDelivered(s.bob.probe, a, since);
		await expectHeldBack(s.bob.probe, admin, b, since);

		// Lift it again.
		const lifted = Date.now();
		await setPolicy(request, s, readAll(s.projectId, "tasks"));
		await nextChange(s.bob.probe, lifted);
		since = Date.now();
		await patchTask(request, s.projectId, b, name("w"));
		await expectDelivered(s.bob.probe, b, since);
		expect(s.bob.probe.socket.id).toBe(socketId);
		expect(s.bob.probe.connected).toBe(true);
	});

	test("revoking tasks:read takes the tasks room away and restoring it brings it back", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "revoke", (p) => [
			readAll(p, "tasks", "docs"),
		]);
		const task = await createTask(request, s.projectId, name("t"));
		const doc = await createDoc(request, s.projectId, name("d"));
		const socketId = s.bob.probe.socket.id;

		let t = Date.now();
		await setPolicy(request, s, readAll(s.projectId, "docs"));
		let namespaces = await changeMatching(
			s.bob.probe,
			t,
			(ns) => !ns.includes("tasks"),
		);
		expect(namespaces).toEqual(["docs"]);

		let since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		await request.patch(`${API_URL}/projects/${s.projectId}/docs/${doc}`, {
			data: { title: name("d2") },
		});
		// The docs room is intact (control), the tasks room is gone.
		await expect
			.poll(() => s.bob.probe.eventsAbout(doc, since).length)
			.toBeGreaterThan(0);
		await sleep(400);
		expect(s.bob.probe.eventsAbout(task, since)).toEqual([]);

		t = Date.now();
		await setPolicy(request, s, readAll(s.projectId, "tasks", "docs"));
		namespaces = await changeMatching(s.bob.probe, t, (ns) =>
			ns.includes("tasks"),
		);
		expect([...namespaces].sort()).toEqual(["docs", "tasks"]);
		since = Date.now();
		await patchTask(request, s.projectId, task, name("y"));
		await expectDelivered(s.bob.probe, task, since);
		expect(s.bob.probe.socket.id).toBe(socketId);
	});

	test("announces the project and the namespaces in permissions:changed", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "shape", (p) => [
			readAll(p, "tasks"),
		]);
		const t = Date.now();
		await setPolicy(request, s, readAll(s.projectId, "tasks", "sprints"));
		const change = await nextChange(s.bob.probe, t);
		expect(change.projectId).toBe(s.projectId);
		expect(Array.isArray(change.namespaces)).toBe(true);
		await changeMatching(s.bob.probe, t, (ns) => ns.includes("sprints"));
	});

	test("narrowing a role to one task keeps the tasks room and limits what arrives", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "narrow", (p) => [
			readAll(p, "tasks"),
		]);
		const mine = await createTask(request, s.projectId, name("mine"));
		const other = await createTask(request, s.projectId, name("other"));
		const admin = await adminProbe(request, s.projectId);

		const t = Date.now();
		await setPolicy(
			request,
			s,
			open(s.projectId),
			allow(["tasks:read"], [`${res(s.projectId)}/task/${mine}`]),
		);
		const namespaces = await changeMatching(s.bob.probe, t, () => true);
		expect(namespaces).toContain("tasks");

		const since = Date.now();
		await patchTask(request, s.projectId, other, name("x"));
		await patchTask(request, s.projectId, mine, name("y"));
		await expectDelivered(s.bob.probe, mine, since);
		await expectHeldBack(s.bob.probe, admin, other, since);
	});

	test("changing the sprint a condition names swaps which tasks are delivered", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "cond", () => []);
		const sprint1 = await createSprint(request, s.projectId, name("S1"));
		const sprint2 = await createSprint(request, s.projectId, name("S2"));
		const t1 = await createTask(request, s.projectId, name("t1"), sprint1);
		const t2 = await createTask(request, s.projectId, name("t2"), sprint2);
		const admin = await adminProbe(request, s.projectId);
		const onlySprint = (sprint: string) =>
			allow(["tasks:read"], [`${res(s.projectId)}/task/*`], {
				StringEquals: { "task.sprint_id": sprint },
			});

		let t = Date.now();
		await setPolicy(request, s, open(s.projectId), onlySprint(sprint1));
		await nextChange(s.bob.probe, t);
		let since = Date.now();
		await patchTask(request, s.projectId, t2, name("a"));
		await patchTask(request, s.projectId, t1, name("b"));
		await expectDelivered(s.bob.probe, t1, since);
		await expectHeldBack(s.bob.probe, admin, t2, since);

		t = Date.now();
		await setPolicy(request, s, open(s.projectId), onlySprint(sprint2));
		await nextChange(s.bob.probe, t);
		since = Date.now();
		await patchTask(request, s.projectId, t1, name("c"));
		await patchTask(request, s.projectId, t2, name("d"));
		await expectDelivered(s.bob.probe, t2, since);
		await expectHeldBack(s.bob.probe, admin, t1, since);
	});

	test("a burst of edits ends on the last one", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "burst", (p) => [
			readAll(p, "tasks"),
		]);
		const task = await createTask(request, s.projectId, name("t"));
		const t = Date.now();
		// Four edits without waiting in between; only the last one revokes tasks.
		await setPolicy(request, s, readAll(s.projectId, "tasks", "docs"));
		await setPolicy(request, s, readAll(s.projectId, "docs"));
		await setPolicy(request, s, readAll(s.projectId, "tasks"));
		await setPolicy(request, s, readAll(s.projectId, "sprints"));
		await changeMatching(
			s.bob.probe,
			t,
			(ns) => ns.length === 1 && ns[0] === "sprints",
		);

		const since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		const admin = await adminProbe(request, s.projectId);
		await sleep(600);
		expect(s.bob.probe.eventsAbout(task, since)).toEqual([]);
		expect(admin.connected).toBe(true);
	});

	test("takes effect within a few seconds", async ({ request, playwright }) => {
		const s = await scene(request, playwright, "fast", (p) => [
			readAll(p, "tasks"),
		]);
		const started = Date.now();
		await setPolicy(request, s, open(s.projectId));
		await changeMatching(s.bob.probe, started, (ns) => ns.length === 0);
		expect(Date.now() - started).toBeLessThan(5_000);
	});
});

// ─── Attaching and detaching roles ───────────────────────────────────────────

test.describe("Changing which roles a member holds", () => {
	test("replacing the member's roles re-syncs the open socket", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "swap", (p) => [
			readAll(p, "tasks"),
		]);
		const docsOnly = await projectRoleFrom(
			request,
			s.projectId,
			name("docsonly"),
			readAll(s.projectId, "docs"),
		);
		const task = await createTask(request, s.projectId, name("t"));

		let t = Date.now();
		expect(
			(
				await putMemberRoles(request, s.projectId, s.member.memberId, [
					docsOnly,
				])
			).ok(),
		).toBeTruthy();
		expect(
			await changeMatching(s.bob.probe, t, (ns) => !ns.includes("tasks")),
		).toEqual(["docs"]);

		t = Date.now();
		expect(
			(
				await putMemberRoles(request, s.projectId, s.member.memberId, [
					s.roleId,
				])
			).ok(),
		).toBeTruthy();
		await changeMatching(
			s.bob.probe,
			t,
			(ns) => ns.includes("tasks") && !ns.includes("docs"),
		);
		const since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		await expectDelivered(s.bob.probe, task, since);
	});

	test("attaching a second role adds its rooms and detaching it removes them", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "second", (p) => [
			readAll(p, "tasks"),
		]);
		const docsRole = await projectRoleFrom(
			request,
			s.projectId,
			name("docs"),
			readAll(s.projectId, "docs"),
		);
		const doc = await createDoc(request, s.projectId, name("d"));

		let t = Date.now();
		await putMemberRoles(request, s.projectId, s.member.memberId, [
			s.roleId,
			docsRole,
		]);
		await changeMatching(
			s.bob.probe,
			t,
			(ns) => ns.includes("docs") && ns.includes("tasks"),
		);
		let since = Date.now();
		await request.patch(`${API_URL}/projects/${s.projectId}/docs/${doc}`, {
			data: { title: name("d2") },
		});
		await expectDelivered(s.bob.probe, doc, since);

		t = Date.now();
		await putMemberRoles(request, s.projectId, s.member.memberId, [s.roleId]);
		await changeMatching(s.bob.probe, t, (ns) => !ns.includes("docs"));
		since = Date.now();
		const admin = await adminProbe(request, s.projectId);
		await request.patch(`${API_URL}/projects/${s.projectId}/docs/${doc}`, {
			data: { title: name("d3") },
		});
		await expectHeldBack(s.bob.probe, admin, doc, since);
	});

	test("a workspace role that names the project adds rooms too", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "platform", (p) => [open(p)]);
		expect(s.bob.namespaces).toEqual([]);
		const platformRole = await createGlobalRole(request, name("platform"), {
			version: "2026-10-01",
			statements: [allow(["tasks:read"], [`${res(s.projectId)}/*`])],
		});
		const task = await createTask(request, s.projectId, name("t"));

		const t = Date.now();
		expect(
			(await putUserRoles(request, s.member.userId, [platformRole.id])).ok(),
		).toBeTruthy();
		await changeMatching(s.bob.probe, t, (ns) => ns.includes("tasks"));
		const since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		await expectDelivered(s.bob.probe, task, since);
	});

	test("deleting the role the member holds empties their rooms", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "delrole", (p) => [
			readAll(p, "tasks"),
		]);
		const keep = await projectRoleFrom(
			request,
			s.projectId,
			name("keep"),
			open(s.projectId),
		);
		await putMemberRoles(request, s.projectId, s.member.memberId, [
			s.roleId,
			keep,
		]);
		const task = await createTask(request, s.projectId, name("t"));

		const t = Date.now();
		const deleted = await request.delete(
			`${API_URL}/projects/${s.projectId}/roles/${s.roleId}`,
		);
		expect(deleted.ok(), await deleted.text()).toBeTruthy();
		await changeMatching(s.bob.probe, t, (ns) => ns.length === 0);
		const since = Date.now();
		const admin = await adminProbe(request, s.projectId);
		await patchTask(request, s.projectId, task, name("x"));
		await expectHeldBack(s.bob.probe, admin, task, since);
	});
});

// ─── Removing people and projects ────────────────────────────────────────────

test.describe("Removing access altogether", () => {
	test("removing the member takes them out of every room", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "remove", (p) => [
			readAll(p, "tasks", "docs"),
		]);
		const task = await createTask(request, s.projectId, name("t"));
		const admin = await adminProbe(request, s.projectId);

		const t = Date.now();
		const removed = await request.delete(
			`${API_URL}/projects/${s.projectId}/members/${s.member.memberId}`,
		);
		expect(removed.ok(), await removed.text()).toBeTruthy();
		expect(
			await changeMatching(s.bob.probe, t, (ns) => ns.length === 0),
		).toEqual([]);
		const since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		await expectHeldBack(s.bob.probe, admin, task, since);
	});

	test("a member who is added back gets their rooms and events again on the same socket", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "readd", (p) => [
			readAll(p, "tasks"),
		]);
		const task = await createTask(request, s.projectId, name("t"));
		const admin = await adminProbe(request, s.projectId);
		const socketId = s.bob.probe.socket.id;

		let t = Date.now();
		await request.delete(
			`${API_URL}/projects/${s.projectId}/members/${s.member.memberId}`,
		);
		await changeMatching(s.bob.probe, t, (ns) => ns.length === 0);
		let since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		await expectHeldBack(s.bob.probe, admin, task, since);

		// Adding the member back is an IAM change too: the open socket is
		// re-synced into its rooms without joining again.
		t = Date.now();
		const readded = await request.post(
			`${API_URL}/projects/${s.projectId}/members`,
			{
				data: { user_id: s.member.userId, role_ids: [s.roleId] },
			},
		);
		expect(readded.ok(), await readded.text()).toBeTruthy();
		await changeMatching(s.bob.probe, t, (ns) => ns.includes("tasks"));
		since = Date.now();
		await patchTask(request, s.projectId, task, name("y"));
		await expectDelivered(s.bob.probe, task, since);
		expect(s.bob.probe.socket.id).toBe(socketId);
	});

	test("deleting the user takes them out of every room", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "deluser", (p) => [
			readAll(p, "tasks"),
		]);
		const task = await createTask(request, s.projectId, name("t"));
		const admin = await adminProbe(request, s.projectId);

		const t = Date.now();
		const deleted = await request.delete(
			`${API_URL}/admin/users/${s.member.userId}`,
		);
		expect(deleted.ok(), await deleted.text()).toBeTruthy();
		await changeMatching(s.bob.probe, t, (ns) => ns.length === 0);
		const since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		await expectHeldBack(s.bob.probe, admin, task, since);
	});

	test("deleting the project tells its members' sockets to refresh", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "delproject", (p) => [
			readAll(p, "tasks"),
		]);
		const t = Date.now();
		const deleted = await request.delete(`${API_URL}/projects/${s.projectId}`);
		expect(deleted.ok(), await deleted.text()).toBeTruthy();
		// The project is soft-deleted and its members keep their grants, so the
		// rooms stay; what matters is that the change is announced.
		const change = await nextChange(s.bob.probe, t);
		expect(change.projectId).toBe(s.projectId);
		expect(s.bob.probe.connected).toBe(true);
	});
});

// ─── Who is told ─────────────────────────────────────────────────────────────

test.describe("Other sockets", () => {
	test("every socket of the affected account is re-synced", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "tabs", (p) => [
			readAll(p, "tasks", "docs"),
		]);
		const second = await connectAs(playwright, s.member.username, s.projectId);

		const t = Date.now();
		await setPolicy(request, s, readAll(s.projectId, "docs"));
		await changeMatching(s.bob.probe, t, (ns) => !ns.includes("tasks"));
		await changeMatching(second.probe, t, (ns) => !ns.includes("tasks"));
	});

	test("a socket the change does not concern keeps its rooms and its events", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "bystander", (p) => [
			readAll(p, "tasks"),
		]);
		const carolRole = await projectRoleFrom(
			request,
			s.projectId,
			name("carol"),
			readAll(s.projectId, "tasks"),
		);
		const carol = await createMember(
			request,
			playwright,
			s.projectId,
			name("carol"),
			[carolRole],
		);
		const carolSocket = await connectAs(
			playwright,
			carol.username,
			s.projectId,
		);
		const task = await createTask(request, s.projectId, name("t"));

		// Edit bob's role only. Carol is told to refresh, with the same rooms.
		const t = Date.now();
		await setPolicy(request, s, open(s.projectId));
		await changeMatching(s.bob.probe, t, (ns) => ns.length === 0);
		const note = await nextChange(carolSocket.probe, t);
		expect(note.namespaces).toEqual(["tasks"]);

		const since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		await expectDelivered(carolSocket.probe, task, since);
		expect(s.bob.probe.eventsAbout(task, since)).toEqual([]);
	});

	test("an administrator's socket is unaffected by members' role edits", async ({
		request,
		playwright,
	}) => {
		const s = await scene(request, playwright, "admin", (p) => [
			readAll(p, "tasks"),
		]);
		const admin = await adminProbe(request, s.projectId);
		const task = await createTask(request, s.projectId, name("t"));
		await setPolicy(request, s, open(s.projectId));
		await sleep(800);
		const since = Date.now();
		await patchTask(request, s.projectId, task, name("x"));
		await expectDelivered(admin, task, since);
	});
});
