// The guarantees added on top of the feed: row versions and If-Match, the
// client operation id that lets a client recognise its own echo, the change
// head that closes the window before a first join, and the outbox that
// announces a database change whose announcement was lost.

import { execFileSync } from "node:child_process";
import { type APIRequestContext, expect, test } from "@playwright/test";
import { API_URL, authRequest, createProject } from "../helpers/e2e-api";
import { createSprint, createTask } from "../helpers/iam";
import { cookieHeaderOf, Probe, sleep, track } from "../helpers/realtime";
import {
	allChanges,
	changes,
	syncProbe,
	useSyncScaffold,
	waitForType,
} from "../helpers/sync";

const { name } = useSyncScaffold("E2E_RTSYNCGAP_");

function psql(sql: string): string | null {
	try {
		return execFileSync(
			"docker",
			[
				"exec",
				"paca-e2e-postgres-1",
				"psql",
				"-U",
				"paca",
				"-d",
				"paca",
				"-At",
				"-c",
				sql,
			],
			{ encoding: "utf8", timeout: 15_000 },
		).trim();
	} catch {
		return null;
	}
}

const versionOf = async (request: APIRequestContext, url: string) =>
	Number((await (await request.get(url)).json()).data.version);

test.describe("row versions and If-Match", () => {
	test("a task and a sprint carry a version that goes up with every update, in the change too", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("ver"));
		const api = changes(request, projectId);
		const { probe } = await syncProbe(request, projectId);

		const taskId = await createTask(request, projectId, name("t"));
		const created = await waitForType(probe, "task.created", {
			entityId: taskId,
		});
		// Whatever the creation itself did to the row, every update adds one.
		const base = Number(created.entity?.version);
		expect(base).toBeGreaterThanOrEqual(1);

		await api.patchTask(taskId, { title: name("t2") });
		await api.patchTask(taskId, { title: name("t3") });
		await expect
			.poll(
				() =>
					probe.syncedChanges().filter((c) => c.type === "task.updated").length,
			)
			.toBe(2);
		const versions = probe
			.syncedChanges()
			.filter((c) => c.type === "task.updated")
			.map((c) => c.entity?.version);
		// Workers (auto-assign, autofill) may touch the row in between, so the
		// versions only have to rise, one step per update at least.
		expect(Number(versions[0])).toBeGreaterThan(base);
		expect(Number(versions[1])).toBeGreaterThan(Number(versions[0]));

		const got = await request.get(
			`${API_URL}/projects/${projectId}/tasks/${taskId}`,
		);
		expect((await got.json()).data.version).toBeGreaterThanOrEqual(
			Number(versions[1]),
		);

		const sprintId = await createSprint(request, projectId, name("s"));
		const sprint = await waitForType(probe, "sprint.created", {
			entityId: sprintId,
		});
		const sprintBase = Number(sprint.entity?.version);
		expect(sprintBase).toBeGreaterThanOrEqual(1);
		await api.patchSprint(sprintId, { name: name("s2") });
		const updated = await waitForType(probe, "sprint.updated");
		expect(Number(updated.entity?.version)).toBeGreaterThan(sprintBase);
	});

	test("If-Match applies a write at the current version and refuses a stale one with 412", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("ifm"));
		const taskId = await createTask(request, projectId, name("t"));
		const url = `${API_URL}/projects/${projectId}/tasks/${taskId}`;
		const v = await versionOf(request, url);

		const stale = await request.patch(url, {
			data: { title: name("stale") },
			headers: { "If-Match": `"${v + 90}"` },
		});
		expect(stale.status()).toBe(412);
		expect((await stale.json()).error_code).toBe("VERSION_CONFLICT");
		expect((await (await request.get(url)).json()).data.title).toBe(name("t"));

		const ok = await request.patch(url, {
			data: { title: name("fresh") },
			headers: { "If-Match": `"${v}"` },
		});
		expect(ok.status(), await ok.text()).toBe(200);
		expect((await ok.json()).data.version).toBeGreaterThan(v);
		// The version moved on, so the same precondition now fails.
		const again = await request.patch(url, {
			data: { title: name("again") },
			headers: { "If-Match": String(v) },
		});
		expect(again.status()).toBe(412);

		const bad = await request.patch(url, {
			data: { title: name("bad") },
			headers: { "If-Match": "abc" },
		});
		expect(bad.status()).toBe(400);
		const none = await request.patch(url, { data: { title: name("none") } });
		expect(none.status()).toBe(200);
	});

	test("two writers naming the same version cannot both win", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("race"));
		const taskId = await createTask(request, projectId, name("t"));
		const url = `${API_URL}/projects/${projectId}/tasks/${taskId}`;
		const v = await versionOf(request, url);
		const results = await Promise.all(
			Array.from({ length: 8 }, (_, i) =>
				request.patch(url, {
					data: { title: name(`w${i}`) },
					headers: { "If-Match": String(v) },
				}),
			),
		);
		const statuses = results.map((r) => r.status()).sort();
		expect(statuses.filter((s) => s === 200)).toHaveLength(1);
		expect(statuses.filter((s) => s === 412)).toHaveLength(7);
	});

	test("a sprint honours If-Match too", async ({ request }) => {
		await authRequest(request);
		const projectId = await createProject(request, name("sifm"));
		const sprintId = await createSprint(request, projectId, name("s"));
		const url = `${API_URL}/projects/${projectId}/sprints/${sprintId}`;
		const stale = await request.patch(url, {
			data: { name: name("x") },
			headers: { "If-Match": "7" },
		});
		expect(stale.status()).toBe(412);
		const ok = await request.patch(url, {
			data: { name: name("y") },
			headers: { "If-Match": "1" },
		});
		expect(ok.status(), await ok.text()).toBe(200);
	});
});

test.describe("the client operation id", () => {
	test("a mutation's X-Client-Op-Id comes back on the change it caused, and an invalid one is dropped", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("op"));
		const taskId = await createTask(request, projectId, name("t"));
		const { probe } = await syncProbe(request, projectId);
		const url = `${API_URL}/projects/${projectId}/tasks/${taskId}`;

		await request.patch(url, {
			data: { title: name("a") },
			headers: { "X-Client-Op-Id": "op-abc_123.x" },
		});
		await expect
			.poll(
				() => allChanges(probe).filter((c) => c.type === "task.updated").length,
			)
			.toBe(1);
		expect(allChanges(probe)[0]?.client_op_id).toBe("op-abc_123.x");

		await request.patch(url, {
			data: { title: name("b") },
			headers: { "X-Client-Op-Id": "has space;drop" },
		});
		await request.patch(url, { data: { title: name("c") } });
		await expect
			.poll(
				() => allChanges(probe).filter((c) => c.type === "task.updated").length,
			)
			.toBe(3);
		const later = allChanges(probe)
			.filter((c) => c.type === "task.updated")
			.slice(1);
		expect(later.map((c) => c.client_op_id)).toEqual([undefined, undefined]);
	});
});

test.describe("the change head on reads", () => {
	test("a read of a project carries the head it was read at, matching the feed's epoch", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("head"));
		await createTask(request, projectId, name("a"));
		const read = await request.get(`${API_URL}/projects/${projectId}/tasks`);
		const seq = Number(read.headers()["x-change-seq"]);
		const epoch = Number(read.headers()["x-change-epoch"]);
		expect(seq).toBeGreaterThanOrEqual(1);

		const { ack } = await syncProbe(request, projectId);
		expect(ack.epoch).toBe(epoch);
		expect(ack.head).toBeGreaterThanOrEqual(seq);
	});

	test("a join from the head a read was made at replays what changed in between", async ({
		request,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("window"));
		await createTask(request, projectId, name("seed"));
		const read = await request.get(`${API_URL}/projects/${projectId}/tasks`);
		const seq = Number(read.headers()["x-change-seq"]);
		const epoch = Number(read.headers()["x-change-epoch"]);

		// A change lands after the read and before the join: with a plain first
		// join (at the head) it would be missed.
		const between = await createTask(request, projectId, name("between"));
		const probe = track(
			await Probe.connect({ cookie: await cookieHeaderOf(request) }),
		);
		const ack = await probe.syncJoin(projectId, seq, epoch);
		expect(ack.mode).toBe("replay");
		expect(JSON.stringify(allChanges(probe))).toContain(between);
	});

	test("a project the caller cannot read gives no head", async ({
		request,
		playwright,
	}) => {
		await authRequest(request);
		const projectId = await createProject(request, name("secret"));
		await createTask(request, projectId, name("a"));
		const outsider = await playwright.request.newContext();
		const response = await outsider.get(
			`${API_URL}/projects/${projectId}/tasks`,
		);
		expect([401, 403]).toContain(response.status());
		expect(response.headers()["x-change-seq"]).toBeUndefined();
		await outsider.dispose();
	});
});

test.describe("the outbox", () => {
	test.skip(
		psql("select 1") !== "1",
		"needs `docker exec` access to the e2e Postgres",
	);

	const outboxRows = (taskId: string) =>
		Number(
			psql(`select count(*) from change_outbox where entity_id = '${taskId}'`),
		);

	test("every change is recorded with the commit, and a change that was announced is not announced again", async ({
		request,
	}) => {
		test.slow();
		test.setTimeout(120_000);
		await authRequest(request);
		const projectId = await createProject(request, name("normal"));
		const api = changes(request, projectId);
		const taskId = await createTask(request, projectId, name("t"));
		const { probe } = await syncProbe(request, projectId);
		await api.patchTask(taskId, { title: name("t2") });
		await waitForType(probe, "task.updated", { entityId: taskId });

		// The trigger wrote a row per change (insert + update) in the commit.
		expect(outboxRows(taskId)).toBeGreaterThanOrEqual(2);
		// The reconciler looks at them after the grace period and finds them announced.
		await expect
			.poll(() => outboxRows(taskId), { timeout: 70_000, intervals: [3_000] })
			.toBe(0);
		expect(allChanges(probe).filter((c) => c.payload?.repair)).toEqual([]);
	});

	test("a database change that was never announced is announced by the reconciler, with the entity", async ({
		request,
	}) => {
		test.slow();
		test.setTimeout(120_000);
		await authRequest(request);
		const projectId = await createProject(request, name("lost"));
		const taskId = await createTask(request, projectId, name("t"));
		const { probe } = await syncProbe(request, projectId);
		await sleep(500);
		const before = await versionOf(
			request,
			`${API_URL}/projects/${projectId}/tasks/${taskId}`,
		);

		// A write that bypasses the API: no event, as if the process had died
		// between the commit and the announcement.
		expect(
			psql(
				`update tasks set title = '${name("sneaky")}' where id = '${taskId}'`,
			),
		).not.toBeNull();

		const repair = await waitForType(probe, "task.updated", {
			entityId: taskId,
			timeoutMs: 80_000,
		});
		expect(repair.payload?.repair).toBe(true);
		expect(repair.entity).toMatchObject({ id: taskId, title: name("sneaky") });
		expect(repair.entity?.version).toBe(before + 1);
		await expect.poll(() => outboxRows(taskId), { timeout: 20_000 }).toBe(0);
	});

	test("a delete that was never announced arrives as a delete", async ({
		request,
	}) => {
		test.slow();
		test.setTimeout(120_000);
		await authRequest(request);
		const projectId = await createProject(request, name("lostdel"));
		const taskId = await createTask(request, projectId, name("t"));
		const { probe } = await syncProbe(request, projectId);
		await sleep(500);
		expect(
			psql(`update tasks set deleted_at = now() where id = '${taskId}'`),
		).not.toBeNull();
		const gone = await waitForType(probe, "task.deleted", {
			entityId: taskId,
			timeoutMs: 80_000,
		});
		expect(gone.payload?.repair).toBe(true);
	});

	test("tasks moved by completing a sprint are announced one by one", async ({
		request,
	}) => {
		test.slow();
		test.setTimeout(150_000);
		await authRequest(request);
		const projectId = await createProject(request, name("complete"));
		const api = changes(request, projectId);
		const sprintId = await createSprint(request, projectId, name("s"));
		const a = await createTask(request, projectId, name("a"), sprintId);
		const b = await createTask(request, projectId, name("b"), sprintId);
		await api.patchSprint(sprintId, { status: "active" });
		const { probe } = await syncProbe(request, projectId);
		await sleep(500);
		await api.completeSprint(sprintId);
		await waitForType(probe, "sprint.completed");

		for (const id of [a, b]) {
			const moved = await waitForType(probe, "task.updated", {
				entityId: id,
				timeoutMs: 90_000,
			});
			expect(moved.entity?.sprint_id ?? null).not.toBe(sprintId);
		}
	});
});
