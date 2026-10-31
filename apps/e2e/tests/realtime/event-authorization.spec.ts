// spec: features/realtime/event-authorization.feature
// seed: tests/seed.spec.ts
//
// Live events follow the same IAM policies as the REST routes. A socket joins
// the rooms its roles give it some action for, and every event is then checked
// against the account on the resource it is about: a Deny on one task, a role
// limited to one sprint's tasks or one folder's documents, or an Allow on one
// resource applies to what is pushed exactly as it does to what is fetched.
//
// The specs open real socket.io connections (the protocol the web app speaks)
// as signed-in members, change content as the admin over the API, and assert on
// what each socket receives. A negative assertion always waits for a socket
// that IS allowed to receive the same event first, so "nothing arrived" is
// never just "nothing has arrived yet".

import {
	type APIRequestContext,
	expect,
	type PlaywrightWorkerArgs,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	newRunId,
} from "../helpers/e2e-api";
import {
	cleanupEnvironmentsInProjectsByPrefix,
	createEnvironment,
} from "../helpers/environments";
import {
	allow,
	createDoc,
	createFolder,
	createMember,
	createSprint,
	createTask,
	deny,
	type Member,
	projectRoleFrom,
	sprintViewIds,
} from "../helpers/iam";
import {
	accessTokenOf,
	closeAll,
	connectAs,
	cookieHeaderOf,
	mintToken,
	Probe,
	sleep,
	track,
} from "../helpers/realtime";

const PREFIX = "E2E_RTAUTHZ_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupEnvironmentsInProjectsByPrefix(request, PREFIX);
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
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

/** A project member holding a project role made of `statements`. */
async function memberWith(
	request: APIRequestContext,
	playwright: PlaywrightWorkerArgs["playwright"],
	projectId: string,
	label: string,
	...statements: ReturnType<typeof allow>[]
): Promise<Member> {
	const roleId = await projectRoleFrom(
		request,
		projectId,
		name(`role_${label}`),
		...statements,
	);
	return createMember(request, playwright, projectId, name(label), [roleId]);
}

/** The whole project, read-only for the given domains (and openable). */
const readAll = (projectId: string, ...domains: string[]) =>
	allow(
		["projects:read", ...domains.map((d) => `${d}:read`)],
		[`${res(projectId)}/*`],
	);

/** A socket of the admin: it holds everything, so it sees every event. */
async function adminProbe(request: APIRequestContext, projectId: string) {
	const probe = track(
		await Probe.connect({ cookie: await cookieHeaderOf(request) }),
	);
	await probe.join(projectId);
	return probe;
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

/**
 * Waits until `control` has received an event about `id`, then a short grace
 * period, so that an event addressed to the same publish and still on its way
 * to another socket has had the time to arrive.
 */
async function afterControl(control: Probe, id: string, since: number) {
	await expect
		.poll(() => control.eventsAbout(id, since).length, { timeout: 10_000 })
		.toBeGreaterThan(0);
	await sleep(400);
}

// ─── Connecting ──────────────────────────────────────────────────────────────

test.describe("Connecting to the realtime service", () => {
	test("is refused without credentials", async () => {
		const { probe, error } = await Probe.tryConnect({});
		expect(probe).toBeUndefined();
		expect(error).toBe("missing authentication");
	});

	test("is refused for a token that is not a valid JWT", async () => {
		const { probe, error } = await Probe.tryConnect({ token: "not-a-token" });
		expect(probe).toBeUndefined();
		expect(error).toBe("invalid or expired token");
	});

	test("accepts the session cookie and the handshake token alike", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("conn"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);

		const viaCookie = await connectAs(playwright, bob.username, projectId);
		expect(viaCookie.namespaces).toContain("tasks");

		const token = await accessTokenOf(viaCookie.api);
		const viaToken = track(await Probe.connect({ token }));
		expect(await viaToken.join(projectId)).toContain("tasks");
	});

	test("is refused for an expired token, a token signed with another secret and a refresh-kind token", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("badtok"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const real = await accessTokenOf(
			(await connectAs(playwright, bob.username)).api,
		);

		const refused = async (token: string) =>
			(await Probe.tryConnect({ token })).error;
		expect(await refused(mintToken(real, -120))).toBe(
			"invalid or expired token",
		);
		expect(await refused(mintToken(real, 600, {}, "some-other-secret"))).toBe(
			"invalid or expired token",
		);
		expect(await refused(mintToken(real, 600, { kind: "refresh" }))).toBe(
			"invalid or expired token",
		);
		// Control: the unmodified minted token is accepted.
		expect(
			(await Probe.tryConnect({ token: mintToken(real, 600) })).probe,
		).toBeDefined();
	});

	test("is refused for a user who has been deleted", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("deleted"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const real = await accessTokenOf(
			(await connectAs(playwright, bob.username)).api,
		);
		await request.delete(`${API_URL}/admin/users/${bob.userId}`);

		const { probe } = await Probe.tryConnect({ token: real });
		if (probe) {
			// Authentication may still accept the token; what matters is that a
			// deleted account holds nothing in any project.
			track(probe);
			expect(await probe.join(projectId)).toEqual([]);
		}
	});
});

// ─── Joining a project ───────────────────────────────────────────────────────

test.describe("Joining a project", () => {
	test("puts the socket in exactly the rooms its roles give it an action for", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("rooms"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks", "docs", "views"),
		);
		const { namespaces } = await connectAs(playwright, bob.username, projectId);
		expect([...namespaces].sort()).toEqual(["docs", "tasks", "views"]);
	});

	test("gives a member who may only open the project no rooms at all", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("noroom"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			allow(["projects:read"], [res(projectId)]),
		);
		const { namespaces } = await connectAs(playwright, bob.username, projectId);
		expect(namespaces).toEqual([]);
	});

	test("gives the admin every room", async ({ request }) => {
		await authRequest(request);
		const projectId = await createProject(request, name("admin"));
		const probe = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		expect([...(await probe.join(projectId))].sort()).toEqual([
			"conversations",
			"docs",
			"environments",
			"sprints",
			"tasks",
			"views",
			"workflows",
		]);
	});

	test("still gives the tasks room to a role that holds tasks:read on one task only", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("childonly"));
		const task = await createTask(request, projectId, name("t"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			allow(["projects:read"], [res(projectId)]),
			allow(["tasks:read"], [`${res(projectId)}/task/${task}`]),
		);
		const { namespaces } = await connectAs(playwright, bob.username, projectId);
		expect(namespaces).toContain("tasks");
	});

	test("gives someone who is not a member nothing, and delivers nothing", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("nonmember"));
		const other = await createProject(request, name("other"));
		const outsider = await memberWith(
			request,
			playwright,
			other,
			"outsider",
			readAll(other, "tasks"),
		);
		const { probe, namespaces } = await connectAs(
			playwright,
			outsider.username,
			projectId,
		);
		expect(namespaces).toEqual([]);

		const admin = await adminProbe(request, projectId);
		const task = await createTask(request, projectId, name("t"));
		const since = Date.now();
		await patchTask(request, projectId, task, name("renamed"));
		await afterControl(admin, task, since);
		expect(probe.events).toEqual([]);
	});

	test("does not carry a member of one project into another", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const p1 = await createProject(request, name("p1"));
		const p2 = await createProject(request, name("p2"));
		const bob = await memberWith(
			request,
			playwright,
			p1,
			"bob",
			readAll(p1, "tasks"),
		);
		const { probe } = await connectAs(playwright, bob.username, p1);
		expect(await probe.join(p2)).toEqual([]);

		const admin2 = await adminProbe(request, p2);
		const t2 = await createTask(request, p2, name("t2"));
		const since = Date.now();
		await patchTask(request, p2, t2, name("renamed"));
		await afterControl(admin2, t2, since);
		expect(probe.eventsAbout(t2)).toEqual([]);
	});

	test("ignores a malformed join and keeps working", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("malformed"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe } = await connectAs(playwright, bob.username);

		for (const payload of [
			undefined,
			null,
			{},
			{ projectId: 42 },
			{ projectId: "" },
			"p",
		]) {
			expect(await probe.joinRaw(payload, 600), JSON.stringify(payload)).toBe(
				false,
			);
		}
		expect(probe.connected).toBe(true);
		expect(await probe.join(projectId)).toContain("tasks");
	});

	test("stops delivering after leave, and delivers again after a new join", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("leave"));
		const task = await createTask(request, projectId, name("t"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);
		const admin = await adminProbe(request, projectId);

		let since = Date.now();
		await patchTask(request, projectId, task, name("one"));
		await expect
			.poll(() => probe.eventsAbout(task, since).length)
			.toBeGreaterThan(0);

		probe.leave(projectId);
		await sleep(300);
		since = Date.now();
		await patchTask(request, projectId, task, name("two"));
		await afterControl(admin, task, since);
		expect(probe.eventsAbout(task, since)).toEqual([]);

		await probe.join(projectId);
		since = Date.now();
		await patchTask(request, projectId, task, name("three"));
		await expect
			.poll(() => probe.eventsAbout(task, since).length)
			.toBeGreaterThan(0);
	});
});

// ─── Tasks ───────────────────────────────────────────────────────────────────

test.describe("Task events", () => {
	test("hold back the events of a task the member is denied, and deliver the rest", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("deny"));
		const open = await createTask(request, projectId, name("open"));
		const secret = await createTask(request, projectId, name("secret"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
			deny(["tasks:read"], [`${res(projectId)}/task/${secret}`]),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);
		const admin = await adminProbe(request, projectId);

		let since = Date.now();
		await patchTask(request, projectId, open, name("open2"));
		await expect
			.poll(() => probe.eventsAbout(open, since).length)
			.toBeGreaterThan(0);

		since = Date.now();
		await patchTask(request, projectId, secret, name("secret2"));
		await afterControl(admin, secret, since);
		expect(probe.eventsAbout(secret, since)).toEqual([]);
	});

	test("hold back a denied task's creation, update and deletion alike", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("lifecycle"));
		const secret = await createTask(request, projectId, name("secret"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
			deny(["tasks:read"], [`${res(projectId)}/task/${secret}`]),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);
		const admin = await adminProbe(request, projectId);

		const since = Date.now();
		await patchTask(request, projectId, secret, name("renamed"));
		await request.delete(`${API_URL}/projects/${projectId}/tasks/${secret}`);
		await expect
			.poll(() => admin.eventsAbout(secret, since).map((e) => e.type))
			.toEqual(expect.arrayContaining(["task.updated", "task.deleted"]));
		await sleep(400);
		expect(probe.eventsAbout(secret, since)).toEqual([]);
	});

	test("deliver a task's creation, update and deletion to a member allowed on the whole project", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("whole"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);

		const since = Date.now();
		const task = await createTask(request, projectId, name("t"));
		await patchTask(request, projectId, task, name("renamed"));
		await request.delete(`${API_URL}/projects/${projectId}/tasks/${task}`);
		await expect
			.poll(() => probe.eventsAbout(task, since).map((e) => e.type), {
				timeout: 10_000,
			})
			.toEqual(
				expect.arrayContaining([
					"task.created",
					"task.updated",
					"task.deleted",
				]),
			);
	});

	test("reach a member in the order they happened", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("order"));
		const task = await createTask(request, projectId, name("t"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);

		const since = Date.now();
		const titles = ["one", "two", "three", "four", "five"].map((n) =>
			name(`step_${n}`),
		);
		for (const title of titles)
			await patchTask(request, projectId, task, title);

		await expect
			.poll(() => probe.eventsAbout(task, since).length, { timeout: 10_000 })
			.toBe(titles.length);
		const received = probe
			.eventsAbout(task, since)
			.map((e) => JSON.parse(String(e.payload.content)).changes?.[0]?.new);
		expect(received).toEqual(titles);
	});

	test("follow a role limited to one sprint's tasks", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("sprint"));
		const s1 = await createSprint(request, projectId, name("S1"));
		const s2 = await createSprint(request, projectId, name("S2"));
		const inS1 = await createTask(request, projectId, name("in1"), s1);
		const inS2 = await createTask(request, projectId, name("in2"), s2);
		const noSprint = await createTask(request, projectId, name("none"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			allow(["projects:read"], [res(projectId)]),
			allow(["tasks:read"], [`${res(projectId)}/task/*`], {
				StringEquals: { "task.sprint_id": s1 },
			}),
		);
		const { probe, namespaces } = await connectAs(
			playwright,
			bob.username,
			projectId,
		);
		expect(namespaces).toContain("tasks");
		const admin = await adminProbe(request, projectId);

		const since = Date.now();
		await patchTask(request, projectId, inS2, name("a"));
		await patchTask(request, projectId, noSprint, name("b"));
		await patchTask(request, projectId, inS1, name("c"));
		await expect
			.poll(() => probe.eventsAbout(inS1, since).length)
			.toBeGreaterThan(0);
		await afterControl(admin, inS2, since);
		await afterControl(admin, noSprint, since);
		expect(probe.eventsAbout(inS2, since)).toEqual([]);
		expect(probe.eventsAbout(noSprint, since)).toEqual([]);
	});

	test("deliver to a member who holds tasks:read on a single task only that task's events", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("single"));
		const mine = await createTask(request, projectId, name("mine"));
		const other = await createTask(request, projectId, name("other"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			allow(["projects:read"], [res(projectId)]),
			allow(["tasks:read"], [`${res(projectId)}/task/${mine}`]),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);
		const admin = await adminProbe(request, projectId);

		const since = Date.now();
		await patchTask(request, projectId, other, name("x"));
		await patchTask(request, projectId, mine, name("y"));
		await expect
			.poll(() => probe.eventsAbout(mine, since).length)
			.toBeGreaterThan(0);
		await afterControl(admin, other, since);
		expect(probe.eventsAbout(other, since)).toEqual([]);
	});

	test("combine roles: an Allow from one role and a Deny from another", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("roles"));
		const a = await createTask(request, projectId, name("a"));
		const b = await createTask(request, projectId, name("b"));
		const allowRole = await projectRoleFrom(
			request,
			projectId,
			name("allow"),
			readAll(projectId, "tasks"),
		);
		const denyRole = await projectRoleFrom(
			request,
			projectId,
			name("deny"),
			deny(["tasks:read"], [`${res(projectId)}/task/${b}`]),
		);
		const bob = await createMember(
			request,
			playwright,
			projectId,
			name("bob"),
			[allowRole, denyRole],
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);
		const admin = await adminProbe(request, projectId);

		const since = Date.now();
		await patchTask(request, projectId, b, name("x"));
		await patchTask(request, projectId, a, name("y"));
		await expect
			.poll(() => probe.eventsAbout(a, since).length)
			.toBeGreaterThan(0);
		await afterControl(admin, b, since);
		expect(probe.eventsAbout(b, since)).toEqual([]);
	});

	test("union two roles that each allow a different sprint", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("union"));
		const s1 = await createSprint(request, projectId, name("S1"));
		const s2 = await createSprint(request, projectId, name("S2"));
		const t1 = await createTask(request, projectId, name("t1"), s1);
		const t2 = await createTask(request, projectId, name("t2"), s2);
		const onlySprint = (sprint: string) =>
			allow(
				["projects:read", "tasks:read"],
				[`${res(projectId)}/task/*`, res(projectId)],
				{
					StringEquals: { "task.sprint_id": sprint },
				},
			);
		const r1 = await projectRoleFrom(
			request,
			projectId,
			name("r1"),
			allow(["projects:read"], [res(projectId)]),
			onlySprint(s1),
		);
		const r2 = await projectRoleFrom(
			request,
			projectId,
			name("r2"),
			onlySprint(s2),
		);
		const bob = await createMember(
			request,
			playwright,
			projectId,
			name("bob"),
			[r1, r2],
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);

		const since = Date.now();
		await patchTask(request, projectId, t1, name("x"));
		await patchTask(request, projectId, t2, name("y"));
		await expect
			.poll(() => probe.eventsAbout(t1, since).length)
			.toBeGreaterThan(0);
		await expect
			.poll(() => probe.eventsAbout(t2, since).length)
			.toBeGreaterThan(0);
	});

	test("give two members the events each is allowed and no others", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("two"));
		const a = await createTask(request, projectId, name("a"));
		const b = await createTask(request, projectId, name("b"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
			deny(["tasks:read"], [`${res(projectId)}/task/${b}`]),
		);
		const carol = await memberWith(
			request,
			playwright,
			projectId,
			"carol",
			readAll(projectId, "tasks"),
			deny(["tasks:read"], [`${res(projectId)}/task/${a}`]),
		);
		const bobSocket = (await connectAs(playwright, bob.username, projectId))
			.probe;
		const carolSocket = (await connectAs(playwright, carol.username, projectId))
			.probe;

		const since = Date.now();
		await patchTask(request, projectId, a, name("x"));
		await patchTask(request, projectId, b, name("y"));
		await expect
			.poll(() => bobSocket.eventsAbout(a, since).length)
			.toBeGreaterThan(0);
		await expect
			.poll(() => carolSocket.eventsAbout(b, since).length)
			.toBeGreaterThan(0);
		await sleep(400);
		expect(bobSocket.eventsAbout(b, since)).toEqual([]);
		expect(carolSocket.eventsAbout(a, since)).toEqual([]);
	});

	test("reach every socket of the same account", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("tabs"));
		const task = await createTask(request, projectId, name("t"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const tab1 = (await connectAs(playwright, bob.username, projectId)).probe;
		const tab2 = (await connectAs(playwright, bob.username, projectId)).probe;

		const since = Date.now();
		await patchTask(request, projectId, task, name("x"));
		await expect
			.poll(() => tab1.eventsAbout(task, since).length)
			.toBeGreaterThan(0);
		await expect
			.poll(() => tab2.eventsAbout(task, since).length)
			.toBeGreaterThan(0);
	});

	test("do not reach a user whose only role is a workspace role", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("platform"));
		// A workspace USER who was never added to the project.
		const other = await createProject(request, name("elsewhere"));
		const stranger = await memberWith(
			request,
			playwright,
			other,
			"stranger",
			readAll(other, "tasks"),
		);
		const { probe } = await connectAs(playwright, stranger.username, projectId);
		const admin = await adminProbe(request, projectId);

		const task = await createTask(request, projectId, name("t"));
		const since = Date.now();
		await patchTask(request, projectId, task, name("x"));
		await afterControl(admin, task, since);
		expect(probe.events).toEqual([]);
	});

	// A task that moves out of a member's scope: the member's role no longer
	// covers it, so by the same rule as a denied task nothing about the move
	// is delivered, and the card stays on the member's board until it
	// refreshes. Telling the member it left (a content-free "left your scope"
	// message) is a design decision not taken yet.
	test.fixme("tell a member that a task left their scope", async () => {});

	// Same for deletions under a conditional grant: the task's attributes are
	// gone by the time the event is checked.
	test.fixme("deliver the deletion of a task inside a role limited by a condition", async () => {});
});

// ─── Notifications ───────────────────────────────────────────────────────────

test.describe("Notifications", () => {
	test("reach the assignee only", async ({ request, playwright }) => {
		await authRequest(request);
		const projectId = await createProject(request, name("notify"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const carol = await memberWith(
			request,
			playwright,
			projectId,
			"carol",
			readAll(projectId, "tasks"),
		);
		const bobSocket = (await connectAs(playwright, bob.username, projectId))
			.probe;
		const carolSocket = (await connectAs(playwright, carol.username, projectId))
			.probe;

		const response = await request.post(
			`${API_URL}/projects/${projectId}/tasks`,
			{
				data: { title: name("assigned"), assignee_ids: [bob.memberId] },
			},
		);
		expect(response.ok(), await response.text()).toBeTruthy();

		await expect
			.poll(() => bobSocket.notifications.length, { timeout: 10_000 })
			.toBeGreaterThan(0);
		await sleep(400);
		expect(carolSocket.notifications).toEqual([]);
		expect(bobSocket.notifications[0]?.type).toBe("notification.created");
	});
});

// ─── Documents ───────────────────────────────────────────────────────────────

test.describe("Document events", () => {
	test("hold back a denied document", async ({ request, playwright }) => {
		await authRequest(request);
		const projectId = await createProject(request, name("docdeny"));
		const open = await createDoc(request, projectId, name("open"));
		const secret = await createDoc(request, projectId, name("secret"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "docs"),
			deny(["docs:read"], [`${res(projectId)}/doc/${secret}`]),
		);
		const { probe, namespaces } = await connectAs(
			playwright,
			bob.username,
			projectId,
		);
		expect(namespaces).toContain("docs");
		const admin = await adminProbe(request, projectId);

		const rename = (id: string, title: string) =>
			request.patch(`${API_URL}/projects/${projectId}/docs/${id}`, {
				data: { title },
			});
		let since = Date.now();
		expect((await rename(open, name("open2"))).ok()).toBeTruthy();
		await expect
			.poll(() => probe.eventsAbout(open, since).length)
			.toBeGreaterThan(0);

		since = Date.now();
		expect((await rename(secret, name("secret2"))).ok()).toBeTruthy();
		await afterControl(admin, secret, since);
		expect(probe.eventsAbout(secret, since)).toEqual([]);
	});

	test("follow a role limited to a folder and its subfolders", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("folder"));
		const folder = await createFolder(request, projectId, name("F"));
		const sub = await createFolder(request, projectId, name("sub"), folder);
		const elsewhere = await createFolder(request, projectId, name("G"));
		const inFolder = await createDoc(request, projectId, name("in"), folder);
		const inSub = await createDoc(request, projectId, name("insub"), sub);
		const outside = await createDoc(request, projectId, name("out"), elsewhere);
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			allow(["projects:read"], [res(projectId)]),
			allow(["docs:read"], [`${res(projectId)}/doc/*`], {
				In: { "doc.ancestor_folder_ids": [folder] },
			}),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);
		const admin = await adminProbe(request, projectId);

		const rename = (id: string, title: string) =>
			request.patch(`${API_URL}/projects/${projectId}/docs/${id}`, {
				data: { title },
			});
		const since = Date.now();
		await rename(outside, name("a"));
		await rename(inFolder, name("b"));
		await rename(inSub, name("c"));
		await expect
			.poll(() => probe.eventsAbout(inFolder, since).length)
			.toBeGreaterThan(0);
		await expect
			.poll(() => probe.eventsAbout(inSub, since).length)
			.toBeGreaterThan(0);
		await afterControl(admin, outside, since);
		expect(probe.eventsAbout(outside, since)).toEqual([]);
	});

	test("are not sent to a member who may read tasks but not documents", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("nodocs"));
		const doc = await createDoc(request, projectId, name("d"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "tasks"),
		);
		const { probe, namespaces } = await connectAs(
			playwright,
			bob.username,
			projectId,
		);
		expect(namespaces).not.toContain("docs");
		const admin = await adminProbe(request, projectId);

		const since = Date.now();
		await request.patch(`${API_URL}/projects/${projectId}/docs/${doc}`, {
			data: { title: name("x") },
		});
		await afterControl(admin, doc, since);
		expect(probe.eventsAbout(doc, since)).toEqual([]);
	});
});

// ─── Sprints and views ───────────────────────────────────────────────────────

test.describe("Sprint events", () => {
	test("hold back a denied sprint", async ({ request, playwright }) => {
		await authRequest(request);
		const projectId = await createProject(request, name("sprintdeny"));
		const open = await createSprint(request, projectId, name("open"));
		const secret = await createSprint(request, projectId, name("secret"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "sprints"),
			deny(["sprints:read"], [`${res(projectId)}/sprint/${secret}`]),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);
		const admin = await adminProbe(request, projectId);

		const rename = (id: string, n: string) =>
			request.patch(`${API_URL}/projects/${projectId}/sprints/${id}`, {
				data: { name: n },
			});
		let since = Date.now();
		expect((await rename(open, name("o2"))).ok()).toBeTruthy();
		await expect
			.poll(() => probe.eventsAbout(open, since).length)
			.toBeGreaterThan(0);

		since = Date.now();
		expect((await rename(secret, name("s2"))).ok()).toBeTruthy();
		await afterControl(admin, secret, since);
		expect(probe.eventsAbout(secret, since)).toEqual([]);
	});
});

test.describe("View events", () => {
	test("need views:read: sprints:read alone does not receive them", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("viewperm"));
		const sprintOnly = await memberWith(
			request,
			playwright,
			projectId,
			"sprintonly",
			readAll(projectId, "sprints"),
		);
		const viewer = await memberWith(
			request,
			playwright,
			projectId,
			"viewer",
			readAll(projectId, "views"),
		);
		const a = await connectAs(playwright, sprintOnly.username, projectId);
		const b = await connectAs(playwright, viewer.username, projectId);
		expect(a.namespaces).not.toContain("views");
		expect(b.namespaces).toContain("views");

		const since = Date.now();
		const response = await request.post(
			`${API_URL}/projects/${projectId}/views?context=backlog`,
			{
				data: { name: name("board"), view_type: "board" },
			},
		);
		expect(response.ok(), await response.text()).toBeTruthy();
		await expect
			.poll(() => b.probe.eventsOfType("view.created", since).length)
			.toBeGreaterThan(0);
		await sleep(400);
		expect(a.probe.eventsOfType("view.created", since)).toEqual([]);
	});

	test("follow a role limited to one sprint's views", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("viewsprint"));
		const s1 = await createSprint(request, projectId, name("S1"));
		const s2 = await createSprint(request, projectId, name("S2"));
		const [v1] = await sprintViewIds(request, projectId, s1);
		const [v2] = await sprintViewIds(request, projectId, s2);
		expect(v1 && v2).toBeTruthy();
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			allow(["projects:read"], [res(projectId)]),
			allow(["views:read"], [`${res(projectId)}/view/*`], {
				StringEquals: { "view.sprint_id": s1 },
			}),
		);
		const { probe } = await connectAs(playwright, bob.username, projectId);
		const admin = await adminProbe(request, projectId);

		const rename = (id: string, n: string) =>
			request.patch(
				`${API_URL}/projects/${projectId}/views/${id}?context=sprint&sprint_id=${id === v1 ? s1 : s2}`,
				{ data: { name: n } },
			);
		const since = Date.now();
		expect((await rename(v2 as string, name("b"))).ok()).toBeTruthy();
		expect((await rename(v1 as string, name("a"))).ok()).toBeTruthy();
		await expect
			.poll(() => probe.eventsAbout(v1 as string, since).length)
			.toBeGreaterThan(0);
		await afterControl(admin, v2 as string, since);
		expect(probe.eventsAbout(v2 as string, since)).toEqual([]);
	});
});

// ─── Automations and environments ────────────────────────────────────────────

test.describe("Automation events", () => {
	test("hold back a denied automation and skip members without workflows:read", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("auto"));
		const create = async (n: string) => {
			const r = await request.post(
				`${API_URL}/projects/${projectId}/automations`,
				{ data: { name: n } },
			);
			expect(r.ok(), await r.text()).toBeTruthy();
			return (await r.json()).data.id as string;
		};
		const open = await create(name("open"));
		const secret = await create(name("secret"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "workflows"),
			deny(["workflows:read"], [`${res(projectId)}/workflow/${secret}`]),
		);
		const carol = await memberWith(
			request,
			playwright,
			projectId,
			"carol",
			readAll(projectId, "tasks"),
		);
		const bobSocket = await connectAs(playwright, bob.username, projectId);
		const carolSocket = await connectAs(playwright, carol.username, projectId);
		expect(bobSocket.namespaces).toContain("workflows");
		expect(carolSocket.namespaces).not.toContain("workflows");
		const admin = await adminProbe(request, projectId);

		const rename = (id: string, n: string) =>
			request.patch(`${API_URL}/projects/${projectId}/automations/${id}`, {
				data: { name: n },
			});
		let since = Date.now();
		expect((await rename(open, name("o2"))).ok()).toBeTruthy();
		await expect
			.poll(() => bobSocket.probe.eventsAbout(open, since).length)
			.toBeGreaterThan(0);

		since = Date.now();
		expect((await rename(secret, name("s2"))).ok()).toBeTruthy();
		await afterControl(admin, secret, since);
		expect(bobSocket.probe.eventsAbout(secret, since)).toEqual([]);
		expect(
			carolSocket.probe.events.filter((e) => e.type.startsWith("automation.")),
		).toEqual([]);
	});
});

test.describe("Environment events", () => {
	test("hold back a denied environment", async ({ request, playwright }) => {
		await authRequest(request);
		const projectId = await createProject(request, name("env"));
		const open = await createEnvironment(request, projectId, name("open"));
		const secret = await createEnvironment(request, projectId, name("secret"));
		const bob = await memberWith(
			request,
			playwright,
			projectId,
			"bob",
			readAll(projectId, "environments"),
			deny(
				["environments:read"],
				[`${res(projectId)}/environment/${secret.id}`],
			),
		);
		const { probe, namespaces } = await connectAs(
			playwright,
			bob.username,
			projectId,
		);
		expect(namespaces).toContain("environments");
		const admin = await adminProbe(request, projectId);

		// Sockets are joined first, then each environment is renamed, so the
		// events under test are published after everyone is listening (an
		// environment also emits status events of its own as it provisions).
		const rename = (id: string, n: string) =>
			request.patch(`${API_URL}/projects/${projectId}/environments/${id}`, {
				data: { name: n },
			});
		let since = Date.now();
		expect((await rename(open.id, name("open2"))).ok()).toBeTruthy();
		await expect
			.poll(() => probe.eventsAbout(open.id, since).length)
			.toBeGreaterThan(0);

		since = Date.now();
		expect((await rename(secret.id, name("secret2"))).ok()).toBeTruthy();
		await afterControl(admin, secret.id, since);
		expect(probe.eventsAbout(secret.id, since)).toEqual([]);
	});
});

// ─── Conversations ───────────────────────────────────────────────────────────

test.describe("Conversation events", () => {
	// The e2e stack has no LLM credentials, so no agent ever replies. What the
	// checks must cover once one can: a conversation event reaches only
	// accounts holding conversations:read on THAT conversation, an owner-private
	// conversation reaches its owner alone, and agent.session.started refreshes
	// task boards without carrying any content.
	test.fixme("deliver a conversation's events only to members who may read it", async () => {});
	test.fixme("keep an owner-private conversation to its owner", async () => {});
	test.fixme("refresh task boards on agent.session.started without its content", async () => {});
});
