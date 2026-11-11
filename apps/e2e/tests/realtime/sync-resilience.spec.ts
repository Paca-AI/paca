// What the sync feed guarantees under load and failure: order and gaplessness
// in bursts and under concurrent writers, resuming after any number of
// disconnects without losing or repeating a change, a log that was lost or
// trimmed, sockets that come and go, malformed requests, the legacy events next
// to the feed, and the token lifecycle.

import { execFileSync } from "node:child_process";
import { type APIRequestContext, expect, test } from "@playwright/test";
import { API_URL, authRequest, createProject } from "../helpers/e2e-api";
import {
	allow,
	createMember,
	createTask,
	projectRoleFrom,
} from "../helpers/iam";
import {
	accessTokenOf,
	connectAs,
	cookieHeaderOf,
	mintToken,
	Probe,
	sleep,
	track,
} from "../helpers/realtime";
import {
	chainProblems,
	changes,
	inParallel,
	syncProbe,
	useSyncScaffold,
	waitForCursor,
	waitForType,
} from "../helpers/sync";

const { name } = useSyncScaffold("E2E_RTSYNCRS_");

/** The titles of the task.updated changes a probe received, in order. */
const titles = (p: Probe) =>
	p
		.syncedChanges()
		.filter((c) => c.type === "task.updated")
		.map((c) => String(c.entity?.title));

async function reconnect(
	request: APIRequestContext,
	projectId: string,
	cursor: number,
	epoch: number,
) {
	return syncProbe(request, projectId, cursor, epoch);
}

test.describe("order and gaplessness", () => {
	test("sixty edits one after another arrive in order, once each, in a chain of gapless batches", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("burst"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const { probe, ack } = await syncProbe(request, projectId);

		const wanted = Array.from({ length: 60 }, (_, i) => name(`v${i}`));
		for (const title of wanted) await api.patchTask(taskId, { title });
		await expect.poll(() => titles(probe).length, { timeout: 30_000 }).toBe(60);
		expect(titles(probe)).toEqual(wanted);
		expect(chainProblems(probe, ack.head)).toEqual([]);
		const seqs = probe.syncedChanges().map((c) => c.seq);
		expect(new Set(seqs).size).toBe(seqs.length);
	});

	test("forty edits in parallel are each delivered once, with unique contiguous sequence numbers", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("parallel"));
		const api = changes(request, projectId);
		const ids = await Promise.all(
			Array.from({ length: 40 }, (_, i) =>
				createTask(request, projectId, name(`t${i}`)),
			),
		);
		const { probe, ack } = await syncProbe(request, projectId);
		await inParallel(40, 20, (i) =>
			api.patchTask(ids[i] as string, { title: name(`p${i}`) }),
		);
		await expect.poll(() => titles(probe).length, { timeout: 30_000 }).toBe(40);
		expect([...titles(probe)].sort()).toEqual(
			Array.from({ length: 40 }, (_, i) => name(`p${i}`)).sort(),
		);
		expect(chainProblems(probe, ack.head)).toEqual([]);
	});

	test("two writers at once, a user and an agent's key, interleave without a hole", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("writers"));
		const a = changes(request, projectId);
		const roleId = await projectRoleFrom(
			request,
			projectId,
			name("role"),
			allow(
				["projects:read", "tasks:read", "tasks:write"],
				[`project/${projectId}/*`],
			),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("bob"),
			[roleId],
		);
		const bob = await connectAs(playwright, member.username);
		const b = changes(bob.api, projectId);
		const t1 = await createTask(request, projectId, name("t1"));
		const t2 = await createTask(request, projectId, name("t2"));
		const { probe, ack } = await syncProbe(request, projectId);

		await Promise.all([
			inParallel(15, 5, (i) => a.patchTask(t1, { title: name(`a${i}`) })),
			inParallel(15, 5, (i) => b.patchTask(t2, { title: name(`b${i}`) })),
		]);
		await expect.poll(() => titles(probe).length, { timeout: 30_000 }).toBe(30);
		expect(chainProblems(probe, ack.head)).toEqual([]);
	});

	test("heartbeats report a head that has caught up with what the socket received", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("beat"));
		const { probe, ack } = await syncProbe(request, projectId);
		await createTask(request, projectId, name("a"));
		await createTask(request, projectId, name("b"));
		await waitForCursor(probe, ack.head + 2);
		await expect
			.poll(() => probe.heartbeats.at(-1)?.head ?? 0, { timeout: 25_000 })
			.toBe(probe.batches.at(-1)?.to);
		expect(probe.heartbeats.at(-1)?.epoch).toBe(ack.epoch);
	});
});

test.describe("joining and leaving", () => {
	test("a late joiner starts at the head and sees no history", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("late"));
		await createTask(request, projectId, name("a"));
		await createTask(request, projectId, name("b"));
		const { probe, ack } = await syncProbe(request, projectId);
		expect(ack.mode).toBe("live");
		expect(ack.head).toBeGreaterThanOrEqual(2);
		await sleep(700);
		expect(probe.batches).toEqual([]);
		const next = await createTask(request, projectId, name("c"));
		const c = await waitForType(probe, "task.created", { entityId: next });
		expect(c.seq).toBe(ack.head + 1);
	});

	test("joining twice on one socket delivers each change once", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("twice"));
		const { probe, ack } = await syncProbe(request, projectId);
		await probe.syncJoin(projectId, ack.head, ack.epoch);
		await probe.syncJoin(projectId);
		const taskId = await createTask(request, projectId, name("t"));
		await waitForType(probe, "task.created", { entityId: taskId });
		await sleep(700);
		expect(
			probe.syncedChanges().filter((c) => c.type === "task.created").length,
		).toBe(1);
	});

	test("after leave the socket gets nothing; joining again with its cursor replays what it missed", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("leave"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const { probe, ack } = await syncProbe(request, projectId);
		await api.patchTask(taskId, { title: name("one") });
		await waitForType(probe, "task.updated", { entityId: taskId });
		const cursor = probe.batches.at(-1)?.to as number;

		probe.leave(projectId);
		await sleep(400);
		await api.patchTask(taskId, { title: name("two") });
		await api.patchTask(taskId, { title: name("three") });
		await sleep(1_200);
		expect(titles(probe)).toEqual([name("one")]);

		const rejoined = await probe.syncJoin(projectId, cursor, ack.epoch);
		expect(rejoined.mode).toBe("replay");
		expect(titles(probe)).toEqual([name("one"), name("two"), name("three")]);
	});

	test("two projects on one socket stay apart, each with its own numbering", async ({
		request,
	}) => {
		await authRequest(request);
		const a = await createProject(request, name("pa"));
		const b = await createProject(request, name("pb"));
		const probe = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		const ackA = await probe.syncJoin(a);
		const ackB = await probe.syncJoin(b);
		const ta = await createTask(request, a, name("ta"));
		const tb = await createTask(request, b, name("tb"));
		await expect
			.poll(
				() =>
					probe.batches
						.map((x) => x.projectId)
						.sort()
						.join(","),
				{ timeout: 10_000 },
			)
			.toBe([a, b].sort().join(","));
		const forA = probe.batches.filter((x) => x.projectId === a);
		const forB = probe.batches.filter((x) => x.projectId === b);
		expect(JSON.stringify(forA)).toContain(ta);
		expect(JSON.stringify(forA)).not.toContain(tb);
		expect(JSON.stringify(forB)).toContain(tb);
		expect(forA[0]?.from).toBe(ackA.head);
		expect(forB[0]?.from).toBe(ackB.head);
		expect(forA[0]?.epoch).not.toBe(undefined);
	});

	test("one socket closing does not disturb another on the same project", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("two"));
		const one = await syncProbe(request, projectId);
		const two = await syncProbe(request, projectId);
		one.probe.close();
		const taskId = await createTask(request, projectId, name("t"));
		await waitForType(two.probe, "task.created", { entityId: taskId });
		expect(chainProblems(two.probe, two.ack.head)).toEqual([]);
	});

	test("two sockets of one account both receive every change", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("tabs"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const one = await syncProbe(request, projectId);
		const two = await syncProbe(request, projectId);
		for (let i = 0; i < 5; i++)
			await api.patchTask(taskId, { title: name(`v${i}`) });
		await expect
			.poll(() => titles(one.probe).length, { timeout: 15_000 })
			.toBe(5);
		await expect
			.poll(() => titles(two.probe).length, { timeout: 15_000 })
			.toBe(5);
		expect(titles(one.probe)).toEqual(titles(two.probe));
	});
});

test.describe("resuming", () => {
	test("five disconnect and reconnect cycles lose nothing and repeat nothing", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("cycles"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const received: string[] = [];
		let session = await syncProbe(request, projectId);
		let cursor = session.ack.head;
		const epoch = session.ack.epoch;
		const expected: string[] = [];

		for (let cycle = 0; cycle < 5; cycle++) {
			for (let i = 0; i < 4; i++) {
				const title = name(`c${cycle}_${i}`);
				expected.push(title);
				await api.patchTask(taskId, { title });
				// Half the changes happen while connected, half while away.
				if (i === 1) {
					await waitForCursor(session.probe, cursor + 2);
					session.probe.close();
					received.push(...titles(session.probe));
					cursor = session.probe.batches.at(-1)?.to ?? cursor;
				}
			}
			session = await reconnect(request, projectId, cursor, epoch);
			expect(session.ack.mode).toBe("replay");
		}
		await waitForCursor(session.probe, cursor + 1);
		await sleep(300);
		received.push(...titles(session.probe));
		expect(received.length).toBeGreaterThanOrEqual(expected.length - 4);
		// The union across sessions is every edit, in order, with no repeat.
		expect(new Set(received).size).toBe(received.length);
		expect(received).toEqual(expected.slice(0, received.length));
	});

	test("a cursor equal to the head is live and replays nothing", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("same"));
		await createTask(request, projectId, name("t"));
		const first = await syncProbe(request, projectId);
		const again = await reconnect(
			request,
			projectId,
			first.ack.head,
			first.ack.epoch,
		);
		expect(again.ack.mode).toBe("live");
		await sleep(600);
		expect(again.probe.batches).toEqual([]);
	});

	test("a hundred and twenty missed changes arrive as one gapless replay", async ({
		request,
	}) => {
		test.slow();
		await authRequest(request);
		const projectId = await createProject(request, name("hundred"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const first = await syncProbe(request, projectId);
		first.probe.close();
		await inParallel(120, 10, (i) =>
			api.patchTask(taskId, { title: name(`m${i}`) }),
		);

		const { probe, ack } = await reconnect(
			request,
			projectId,
			first.ack.head,
			first.ack.epoch,
		);
		expect(ack.mode).toBe("replay");
		expect(probe.syncedChanges().length).toBe(120);
		expect(chainProblems(probe, first.ack.head)).toEqual([]);
	});

	test("more than a thousand missed changes are answered with a reset, not a replay", async ({
		request,
	}) => {
		test.slow();
		test.setTimeout(240_000);
		await authRequest(request);
		const projectId = await createProject(request, name("thousand"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const first = await syncProbe(request, projectId);
		first.probe.close();
		await inParallel(1_010, 25, (i) =>
			api.patchTask(taskId, { title: name(`m${i}`) }),
		);

		const { probe, ack } = await reconnect(
			request,
			projectId,
			first.ack.head,
			first.ack.epoch,
		);
		expect(ack.mode).toBe("reset");
		expect(probe.batches).toEqual([]);
		// After the reset it is live at the head.
		const next = await createTask(request, projectId, name("after"));
		const c = await waitForType(probe, "task.created", { entityId: next });
		expect(c.seq).toBe(ack.head + 1);
	});
});

test.describe("malformed requests", () => {
	test("a cursor that is negative, fractional or not a number is handled without harm", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("cursor"));
		await createTask(request, projectId, name("t"));
		const probe = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		const epoch = (await probe.syncJoin(projectId)).epoch;

		const negative = await probe.syncJoin(projectId, -5, epoch);
		expect(negative.mode).toBe("reset");
		const raw = (cursor: unknown) =>
			new Promise<{ mode: string }>((resolve, reject) => {
				probe.socket
					.timeout(5_000)
					.emit(
						"sync:join",
						{ projectId, cursor, epoch },
						(err: Error | null, ack: { mode: string }) =>
							err ? reject(err) : resolve(ack),
					);
			});
		// Not an integer: treated as no cursor, so a live join at the head.
		expect((await raw("abc")).mode).toBe("live");
		expect((await raw(1.5)).mode).toBe("live");
		expect((await raw(null)).mode).toBe("live");
		expect((await raw({ a: 1 })).mode).toBe("live");
		// The socket still works.
		const taskId = await createTask(request, projectId, name("after"));
		await waitForType(probe, "task.created", { entityId: taskId });
	});

	test("a join with no project, a non-string project or no callback gets no answer and harms nothing", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("badjoin"));
		const probe = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		probe.socket.emit("sync:join", {});
		probe.socket.emit("sync:join", { projectId: 42 });
		probe.socket.emit("sync:join", null);
		probe.socket.emit("sync:join", { projectId }); // no callback
		await sleep(800);
		expect(probe.connected).toBe(true);
		const ack = await probe.syncJoin(projectId);
		expect(ack.ok).toBe(true);
	});

	test("a project id with injected syntax is just an unknown project", async ({
		request,
	}) => {
		await authRequest(request);
		const probe = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		for (const id of ["*", "../other", "a:b", "{x}", "%00", "x".repeat(300)]) {
			const ack = await probe.syncJoin(id).catch(() => null);
			if (ack?.ok) expect(ack.namespaces).toEqual([]);
		}
		expect(probe.connected).toBe(true);
	});

	test("unauthenticated sockets cannot sync", async () => {
		const { probe, error } = await Probe.tryConnect({});
		expect(probe).toBeUndefined();
		expect(error).toBe("missing authentication");
	});
});

test.describe("next to the legacy events", () => {
	test("a legacy socket gets the event, a syncing one the change, both about the same task", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("both"));
		const legacy = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		await legacy.join(projectId);
		const { probe: syncing } = await syncProbe(request, projectId);

		const taskId = await createTask(request, projectId, name("t"));
		await waitForType(syncing, "task.created", { entityId: taskId });
		await expect
			.poll(() => legacy.eventsAbout(taskId).length, { timeout: 10_000 })
			.toBeGreaterThan(0);
		expect(legacy.eventsAbout(taskId)[0]?.type).toBe("task.created");
		// Each got its own kind of message only.
		await sleep(500);
		expect(syncing.eventsAbout(taskId)).toEqual([]);
		expect(legacy.batches).toEqual([]);
	});

	test("notifications still reach a syncing socket", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("notify"));
		const roleId = await projectRoleFrom(
			request,
			projectId,
			name("role"),
			allow(
				["projects:read", "tasks:read", "tasks:write"],
				[`project/${projectId}/*`],
			),
		);
		const bob = await createMember(
			request,
			playwright,
			projectId,
			name("bob"),
			[roleId],
		);
		const session = await connectAs(playwright, bob.username);
		await session.probe.syncJoin(projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const response = await request.patch(
			`${API_URL}/projects/${projectId}/tasks/${taskId}`,
			{
				data: { assignee_ids: [bob.memberId] },
			},
		);
		expect(response.ok(), await response.text()).toBeTruthy();
		await expect
			.poll(() => session.probe.notifications.length, { timeout: 15_000 })
			.toBeGreaterThan(0);
	});

	test("a socket that never syncs still gets the old events for everything", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("legacyonly"));
		const legacy = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		await legacy.join(projectId);
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		await api.patchTask(taskId, { title: name("t2") });
		await api.comment(taskId, "hello");
		await expect
			.poll(() => legacy.events.map((e) => e.type), { timeout: 10_000 })
			.toEqual(
				expect.arrayContaining([
					"task.created",
					"task.updated",
					"task.comment.added",
				]),
			);
	});
});

// ─── The log itself ──────────────────────────────────────────────────────────

function valkey(...args: string[]): string | null {
	try {
		return execFileSync(
			"docker",
			["exec", "paca-e2e-valkey-1", "valkey-cli", ...args],
			{
				encoding: "utf8",
				timeout: 10_000,
			},
		).trim();
	} catch {
		return null;
	}
}

test.describe("when the log is lost or trimmed", () => {
	test.skip(
		valkey("PING") !== "PONG",
		"needs `docker exec` access to the e2e Valkey",
	);

	const keys = (projectId: string) => [
		`paca:changes:{${projectId}}:meta`,
		`paca:changes:{${projectId}}:stream`,
	];

	test("a connected socket is sent a reset when the log is recreated, then follows the new log", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("flush"));
		await createTask(request, projectId, name("a"));
		const { probe, ack } = await syncProbe(request, projectId);

		expect(valkey("DEL", ...keys(projectId))).not.toBeNull();
		await sleep(50);
		await createTask(request, projectId, name("b"));

		await expect
			.poll(() => probe.resets.length, { timeout: 15_000 })
			.toBeGreaterThan(0);
		const reset = probe.resets[0];
		expect(reset?.epoch).not.toBe(ack.epoch);
		expect(reset?.head).toBeGreaterThanOrEqual(1);

		const next = await createTask(request, projectId, name("c"));
		const c = await waitForType(probe, "task.created", { entityId: next });
		expect(c.seq).toBeGreaterThan(reset?.head ?? 0);
	});

	test("a join with a cursor from the old log is answered with a reset", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("oldlog"));
		await createTask(request, projectId, name("a"));
		const first = await syncProbe(request, projectId);
		first.probe.close();
		valkey("DEL", ...keys(projectId));
		await createTask(request, projectId, name("b"));

		const again = await reconnect(
			request,
			projectId,
			first.ack.head,
			first.ack.epoch,
		);
		expect(again.ack.mode).toBe("reset");
		expect(again.ack.epoch).not.toBe(first.ack.epoch);
	});

	test("a cursor older than what the log still keeps is answered with a reset", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("trim"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const first = await syncProbe(request, projectId);
		for (let i = 0; i < 4; i++)
			await api.patchTask(taskId, { title: name(`v${i}`) });
		await waitForCursor(first.probe, first.ack.head + 4);
		first.probe.close();

		const [, stream] = keys(projectId);
		expect(valkey("XTRIM", stream as string, "MAXLEN", "0")).not.toBeNull();
		await api.patchTask(taskId, { title: name("after") });

		const again = await reconnect(
			request,
			projectId,
			first.ack.head,
			first.ack.epoch,
		);
		expect(again.ack.mode).toBe("reset");
		expect(again.probe.batches).toEqual([]);
	});

	test("the change log keeps a bounded, expiring stream and a counter that does not expire", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("keys"));
		await createTask(request, projectId, name("t"));
		const [meta, stream] = keys(projectId);
		await sleep(300);
		const ttl = Number(valkey("TTL", stream as string));
		expect(ttl).toBeGreaterThan(6 * 24 * 3600);
		expect(Number(valkey("TTL", meta as string))).toBe(-1);
		expect(Number(valkey("XLEN", stream as string))).toBeGreaterThan(0);
		expect(
			valkey("XRANGE", stream as string, "-", "+", "COUNT", "1"),
		).toContain("1-0");
	});
});

// ─── The token lifecycle ─────────────────────────────────────────────────────

test.describe("a syncing socket and its token", () => {
	test("is asked to reauthenticate before expiry, keeps receiving until then, and is disconnected at expiry", async ({
		request,
		playwright,
	}) => {
		test.slow();
		test.setTimeout(150_000);
		await authRequest(request);
		const projectId = await createProject(request, name("token"));
		const roleId = await projectRoleFrom(
			request,
			projectId,
			name("role"),
			allow(["projects:read", "tasks:read"], [`project/${projectId}/*`]),
		);
		const bob = await createMember(
			request,
			playwright,
			projectId,
			name("bob"),
			[roleId],
		);
		const real = await accessTokenOf(
			(await connectAs(playwright, bob.username)).api,
		);
		const probe = track(await Probe.connect({ token: mintToken(real, 70) }));
		const ack = await probe.syncJoin(projectId);
		expect(ack.namespaces).toContain("tasks");

		await expect.poll(() => probe.reauths.length, { timeout: 30_000 }).toBe(1);
		const taskId = await createTask(request, projectId, name("t"));
		await waitForType(probe, "task.created", { entityId: taskId });

		await expect.poll(() => probe.connected, { timeout: 90_000 }).toBe(false);
		expect(probe.disconnects.at(-1)?.reason).toBe("io server disconnect");
		expect(probe.reauths.length).toBe(1);
	});
});
