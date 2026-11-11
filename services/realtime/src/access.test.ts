import { describe, expect, it } from "bun:test";

import {
	AccessDecider,
	deliverToReaders,
	KeyedQueue,
	type Viewer,
} from "./access.ts";
import { type AccessQuery, ApiRequestError } from "./api-client.ts";

const viewer = (userId: string): Viewer => ({
	userId,
	rawToken: `tok-${userId}`,
});
const q = (id: string): AccessQuery => ({
	action: "tasks:read",
	resource: `project/p/task/${id}`,
});
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// recorder returns a check function that answers `answer` and records each
// request as [token, projectId, resources].
function recorder(answer: (query: AccessQuery, token: string) => boolean) {
	const calls: Array<[string, string, string[]]> = [];
	const check = async (
		_url: string,
		token: string,
		projectId: string,
		queries: AccessQuery[],
	) => {
		calls.push([token, projectId, queries.map((x) => x.resource)]);
		return queries.map((x) => answer(x, token));
	};
	return { calls, check };
}

describe("AccessDecider", () => {
	it("asks the API once per account + action + resource within the TTL", async () => {
		const r = recorder(() => true);
		const d = new AccessDecider("http://api", {
			ttlMs: 60_000,
			check: r.check,
		});
		expect(await d.can(viewer("u1"), "p", q("t1"))).toBe(true);
		expect(await d.can(viewer("u1"), "p", q("t1"))).toBe(true);
		await d.can(viewer("u2"), "p", q("t1"));
		await d.can(viewer("u1"), "p", q("t2"));
		expect(r.calls.length).toBe(3);
	});

	it("batches the questions of one event-loop turn for one account + project into one request", async () => {
		const r = recorder((x) => x.resource.endsWith("t2"));
		const d = new AccessDecider("http://api", {
			check: r.check,
		});
		const answers = await Promise.all([
			d.can(viewer("u1"), "p", q("t1")),
			d.can(viewer("u1"), "p", q("t2")),
			d.can(viewer("u1"), "p", q("t3")),
			d.can(viewer("u2"), "p", q("t1")),
		]);
		expect(answers).toEqual([false, true, false, false]);
		expect(r.calls.length).toBe(2); // u1's three together, u2's one
		expect(r.calls.find((c) => c[0] === "tok-u1")?.[2]).toEqual([
			"project/p/task/t1",
			"project/p/task/t2",
			"project/p/task/t3",
		]);
	});

	it("a cold-cache burst costs one request per account and a warm one costs none", async () => {
		// 30 viewers each asked about the same 200 changes (a pump's batch).
		const r = recorder(() => true);
		const d = new AccessDecider("http://api", { check: r.check });
		const burst = () =>
			Promise.all(
				Array.from({ length: 30 }, (_, u) =>
					Promise.all(
						Array.from({ length: 200 }, (_, i) =>
							d.can(viewer(`u${u}`), "p", q(`t${i}`)),
						),
					),
				),
			);
		await burst();
		expect(r.calls.length).toBe(30);
		await burst();
		expect(r.calls.length).toBe(30);
	});

	it("splits a batch above the API's limit of 200", async () => {
		const r = recorder(() => true);
		const d = new AccessDecider("http://api", { check: r.check });
		const all = await Promise.all(
			Array.from({ length: 450 }, (_, i) =>
				d.can(viewer("u"), "p", q(`t${i}`)),
			),
		);
		expect(all.every(Boolean)).toBe(true);
		expect(r.calls.map((c) => c[2].length)).toEqual([200, 200, 50]);
	});

	it("does not cache a failed check, and fails every question of the batch", async () => {
		let fail = true;
		const calls: number[] = [];
		const d = new AccessDecider("http://api", {
			check: async (_u, _t, _p, queries) => {
				calls.push(queries.length);
				if (fail) throw new ApiRequestError("boom", 500);
				return queries.map(() => true);
			},
		});
		const results = await Promise.allSettled([
			d.can(viewer("u1"), "p", q("t1")),
			d.can(viewer("u1"), "p", q("t2")),
		]);
		expect(results.map((x) => x.status)).toEqual(["rejected", "rejected"]);
		fail = false;
		expect(await d.can(viewer("u1"), "p", q("t1"))).toBe(true);
		expect(calls).toEqual([2, 1]);
	});

	it("flush forgets answers, so a revoked grant is seen at the next event", async () => {
		let allowed = true;
		const d = new AccessDecider("http://api", {
			ttlMs: 60_000,
			check: async (_u, _t, _p, queries) => queries.map(() => allowed),
		});
		expect(await d.can(viewer("u1"), "p", q("t1"))).toBe(true);
		allowed = false;
		expect(await d.can(viewer("u1"), "p", q("t1"))).toBe(true); // still cached
		d.flush();
		expect(await d.can(viewer("u1"), "p", q("t1"))).toBe(false);
	});

	it("expires answers after the TTL", async () => {
		const r = recorder(() => true);
		const d = new AccessDecider("http://api", {
			ttlMs: 10,
			check: r.check,
		});
		await d.can(viewer("u1"), "p", q("t1"));
		await sleep(25);
		await d.can(viewer("u1"), "p", q("t1"));
		expect(r.calls.length).toBe(2);
	});
});

describe("KeyedQueue", () => {
	it("runs jobs of one key in order even when an earlier one is slower", async () => {
		const queue = new KeyedQueue();
		const order: string[] = [];
		queue.run("a", async () => {
			await sleep(20);
			order.push("a1");
		});
		queue.run("a", async () => {
			order.push("a2");
		});
		queue.run("b", async () => {
			order.push("b1");
		});
		await sleep(60);
		expect(order).toEqual(["b1", "a1", "a2"]);
	});
});

describe("deliverToReaders", () => {
	function fakeSocket(userId: string) {
		const sent: unknown[] = [];
		let disconnected = false;
		return {
			sent,
			get disconnected() {
				return disconnected;
			},
			socket: {
				data: viewer(userId),
				emit: (_name: string, ev: unknown) => sent.push(ev),
				disconnect: () => {
					disconnected = true;
				},
				// biome-ignore lint/suspicious/noExplicitAny: minimal RemoteSocket stand-in
			} as any,
		};
	}
	const event = { type: "task.updated", payload: { id: "t1" } };

	it("delivers only to accounts the API allows on the resource", async () => {
		const allowed = fakeSocket("allowed");
		const denied = fakeSocket("denied");
		const d = new AccessDecider("http://api", {
			check: async (_u, token, _p, queries) =>
				queries.map(() => token === "tok-allowed"),
		});
		await deliverToReaders(
			[allowed.socket, denied.socket],
			d,
			"p",
			q("t1"),
			event,
			() => {},
		);
		expect(allowed.sent).toEqual([event]);
		expect(denied.sent).toEqual([]);
	});

	it("fails closed on API errors and disconnects a 401 socket", async () => {
		const flaky = fakeSocket("flaky");
		const expired = fakeSocket("expired");
		const d = new AccessDecider("http://api", {
			check: async (_u, token) => {
				throw new ApiRequestError("x", token === "tok-expired" ? 401 : 500);
			},
		});
		const errors: string[] = [];
		await deliverToReaders(
			[flaky.socket, expired.socket],
			d,
			"p",
			q("t1"),
			event,
			(_e, userId) => errors.push(userId),
		);
		expect(flaky.sent).toEqual([]);
		expect(expired.sent).toEqual([]);
		expect(flaky.disconnected).toBe(false);
		expect(expired.disconnected).toBe(true);
		expect(errors.sort()).toEqual(["expired", "flaky"]);
	});
});
