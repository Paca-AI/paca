// The resumable sync protocol (services/realtime/src/sync.ts), against the real
// stack: a member joins with "sync:join", content changes over the API, and the
// socket must receive the changes in order, gapless, with the entity attached,
// held back as a bare skip where IAM denies them, replayed after a disconnect
// and reset when its cursor means nothing.

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
	allow,
	createMember,
	createSprint,
	createTask,
	deny,
	type Member,
	projectRoleFrom,
} from "../helpers/iam";
import {
	closeAll,
	connectAs,
	cookieHeaderOf,
	Probe,
	sleep,
	track,
} from "../helpers/realtime";

const PREFIX = "E2E_RTSYNC_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
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

const res = (projectId: string) => `project/${projectId}`;

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

const readAll = (projectId: string, ...domains: string[]) =>
	allow(
		["projects:read", ...domains.map((d) => `${d}:read`)],
		[`${res(projectId)}/*`],
	);

async function adminSyncProbe(request: APIRequestContext, projectId: string) {
	const probe = track(
		await Probe.connect({ cookie: await cookieHeaderOf(request) }),
	);
	const ack = await probe.syncJoin(projectId);
	return { probe, ack };
}

async function patchTask(
	request: APIRequestContext,
	projectId: string,
	taskId: string,
	data: Record<string, unknown>,
) {
	const response = await request.patch(
		`${API_URL}/projects/${projectId}/tasks/${taskId}`,
		{ data },
	);
	expect(response.ok(), await response.text()).toBeTruthy();
}

test.describe("sync:join", () => {
	test("acks with the rooms, the epoch and the head, and a socket that syncs gets no legacy event", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("ack"));
		const { probe, ack } = await adminSyncProbe(request, projectId);

		expect(ack.ok).toBe(true);
		expect(ack.mode).toBe("live");
		expect(ack.namespaces).toContain("tasks");
		expect(typeof ack.epoch).toBe("number");
		const taskId = await createTask(request, projectId, name("t1"));

		await expect
			.poll(() => probe.syncedChanges().length, { timeout: 10_000 })
			.toBeGreaterThan(0);
		await sleep(500);
		// The change reached the socket through the feed only.
		expect(probe.eventsAbout(taskId)).toEqual([]);
		const created = probe
			.syncedChanges()
			.find((c) => c.type === "task.created");
		expect(created?.entity).toMatchObject({ id: taskId, title: name("t1") });
	});

	test("delivers changes in order, in gapless batches, with the entity after each", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("order"));
		const taskId = await createTask(request, projectId, name("a"));
		const { probe, ack } = await adminSyncProbe(request, projectId);

		for (const title of ["one", "two", "three"]) {
			await patchTask(request, projectId, taskId, { title: name(title) });
		}
		await expect
			.poll(
				() =>
					probe.syncedChanges().filter((c) => c.type === "task.updated").length,
				{ timeout: 10_000 },
			)
			.toBe(3);

		// Batches chain: each starts where the previous ended, from the ack's head.
		let cursor = ack.head;
		for (const b of probe.batches) {
			expect(b.from).toBe(cursor);
			expect(b.changes.map((c) => c.seq)).toEqual(
				Array.from({ length: b.to - b.from }, (_, i) => b.from + i + 1),
			);
			cursor = b.to;
		}
		const titles = probe
			.syncedChanges()
			.filter((c) => c.type === "task.updated")
			.map((c) => c.entity?.title);
		expect(titles).toEqual([name("one"), name("two"), name("three")]);
	});

	test("asks the same of a member: a Deny on one task arrives as a bare skip, the rest as changes", async ({
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
		const { probe } = await connectAs(playwright, bob.username);
		const ack = await probe.syncJoin(projectId);
		expect(ack.namespaces).toContain("tasks");

		const since = Date.now();
		await patchTask(request, projectId, secret, { title: name("secret2") });
		await patchTask(request, projectId, open, { title: name("open2") });
		await expect
			.poll(
				() => probe.syncedChanges(since).some((c) => c.entity?.id === open),
				{ timeout: 10_000 },
			)
			.toBe(true);

		const seen = JSON.stringify(probe.batches.filter((b) => b.at >= since));
		expect(seen).not.toContain(secret);
		expect(seen).not.toContain(name("secret2"));
		// The secret change still occupies a seq, as a bare skip, so the batch stays gapless.
		const skips = probe.batches
			.filter((b) => b.at >= since)
			.flatMap((b) => b.changes)
			.filter((c) => c.skip);
		expect(skips.length).toBeGreaterThan(0);
		expect(Object.keys(skips[0] ?? {}).sort()).toEqual(["seq", "skip"]);
	});
});

test.describe("resuming", () => {
	test("replays what was missed while disconnected, then continues live", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("resume"));
		const taskId = await createTask(request, projectId, name("a"));
		const first = await adminSyncProbe(request, projectId);
		await patchTask(request, projectId, taskId, { title: name("before") });
		await expect
			.poll(() => first.probe.syncedChanges().length, { timeout: 10_000 })
			.toBeGreaterThan(0);
		const cursor = first.probe.batches.at(-1)?.to as number;
		first.probe.close();

		// Changes made while the socket is away.
		await patchTask(request, projectId, taskId, { title: name("missed1") });
		await patchTask(request, projectId, taskId, { title: name("missed2") });

		const second = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		const ack = await second.syncJoin(projectId, cursor, first.ack.epoch);
		expect(ack.mode).toBe("replay");
		const replayed = second
			.syncedChanges()
			.filter((c) => c.type === "task.updated")
			.map((c) => c.entity?.title);
		expect(replayed).toEqual([name("missed1"), name("missed2")]);
		expect(second.batches[0]?.from).toBe(cursor);

		await patchTask(request, projectId, taskId, { title: name("after") });
		await expect
			.poll(
				() =>
					second.syncedChanges().some((c) => c.entity?.title === name("after")),
				{ timeout: 10_000 },
			)
			.toBe(true);
	});

	test("answers reset for a cursor from another epoch or ahead of the log", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("reset"));
		await createTask(request, projectId, name("a"));
		const { probe, ack } = await adminSyncProbe(request, projectId);

		const otherEpoch = await probe.syncJoin(projectId, 1, ack.epoch + 12345);
		expect(otherEpoch.mode).toBe("reset");
		const ahead = await probe.syncJoin(projectId, ack.head + 1000, ack.epoch);
		expect(ahead.mode).toBe("reset");
		const same = await probe.syncJoin(projectId, ack.head, ack.epoch);
		expect(same.mode).toBe("live");
	});

	test("heartbeats carry the epoch and the head", async ({ request }) => {
		await authRequest(request);
		const projectId = await createProject(request, name("beat"));
		await createTask(request, projectId, name("a"));
		const { probe, ack } = await adminSyncProbe(request, projectId);
		await expect
			.poll(() => probe.heartbeats.length, { timeout: 25_000 })
			.toBeGreaterThan(0);
		expect(probe.heartbeats[0]?.epoch).toBe(ack.epoch);
		expect(probe.heartbeats[0]?.head).toBeGreaterThanOrEqual(ack.head);
	});
});

test.describe("entities", () => {
	test("a sprint change arrives with the sprint", async ({ request }) => {
		await authRequest(request);
		const projectId = await createProject(request, name("sprint"));
		const { probe } = await adminSyncProbe(request, projectId);
		const sprintId = await createSprint(request, projectId, name("s1"));
		await expect
			.poll(
				() =>
					probe
						.syncedChanges()
						.some(
							(c) => c.type === "sprint.created" && c.entity?.id === sprintId,
						),
				{ timeout: 10_000 },
			)
			.toBe(true);
	});

	test("a member change reaches a member who may read members, and nobody else", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("members"));
		const reader = await memberWith(
			request,
			playwright,
			projectId,
			"reader",
			allow(
				["projects:read", "project.members:read"],
				[`${res(projectId)}/*`, res(projectId)],
			),
		);
		const blind = await memberWith(
			request,
			playwright,
			projectId,
			"blind",
			readAll(projectId, "tasks"),
		);
		const a = await connectAs(playwright, reader.username);
		const b = await connectAs(playwright, blind.username);
		await a.probe.syncJoin(projectId);
		await b.probe.syncJoin(projectId);

		const since = Date.now();
		await memberWith(
			request,
			playwright,
			projectId,
			"newcomer",
			readAll(projectId, "tasks"),
		);
		await expect
			.poll(
				() =>
					a.probe
						.syncedChanges(since)
						.some((c) => c.type?.startsWith("member.")),
				{ timeout: 10_000 },
			)
			.toBe(true);
		expect(
			b.probe.syncedChanges(since).filter((c) => c.type?.startsWith("member.")),
		).toEqual([]);
	});
});
