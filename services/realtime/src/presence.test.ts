import { describe, expect, it } from "bun:test";
import type { AccessDecider, Viewer } from "./access.ts";
import {
	PresenceHub,
	type PresenceSnapshot,
	type PresenceSummary,
	parseLiveRef,
	TYPING_TTL_MS,
} from "./presence.ts";

const P = "11111111-1111-4111-8111-111111111111";
const T = "22222222-2222-4222-8222-222222222222";
const ref = { kind: "task" as const, projectId: P, id: T };

const v = (userId: string): Viewer => ({ userId, rawToken: "t" });

function setup(
	opts: { write?: boolean; read?: boolean; replica?: string } = {},
) {
	const emitted: PresenceSnapshot[] = [];
	const published: PresenceSummary[] = [];
	let now = 1_000;
	const decider = {
		can: async (_v: Viewer, _p: string, q: { action: string }) =>
			q.action === "tasks:read" ? (opts.read ?? true) : (opts.write ?? true),
	} as unknown as AccessDecider;
	const hub = new PresenceHub({
		decider,
		replica: opts.replica ?? "a",
		bus: { publish: (s) => published.push(s) },
		transport: { emit: (_r, s) => emitted.push(s) },
		now: () => now,
	});
	return {
		hub,
		emitted,
		published,
		advance: (ms: number) => {
			now += ms;
		},
	};
}

describe("parseLiveRef", () => {
	it("accepts a task ref and rejects junk", () => {
		expect(parseLiveRef(ref)).toEqual(ref);
		expect(parseLiveRef({ ...ref, id: "x" })).toBeUndefined();
		expect(parseLiveRef({ ...ref, kind: "doc" })).toBeUndefined();
		expect(parseLiveRef(null)).toBeUndefined();
	});
});

describe("PresenceHub", () => {
	it("refuses a join without read access", async () => {
		const { hub } = setup({ read: false });
		expect(await hub.join("s1", v("u1"), ref)).toEqual({
			ok: false,
			reason: "forbidden",
		});
	});

	it("lists viewers once per account across sockets", async () => {
		const { hub } = setup();
		await hub.join("s1", v("u1"), ref);
		const r = await hub.join("s2", v("u1"), ref);
		const r2 = await hub.join("s3", v("u2"), ref);
		expect(r.ok && r.snapshot.viewers).toEqual(["u1"]);
		expect(r2.ok && r2.snapshot.viewers).toEqual(["u1", "u2"]);
		hub.leave("s1", ref);
		hub.leave("s3", ref);
		const r3 = await hub.join("s4", v("u3"), ref);
		expect(r3.ok && r3.snapshot.viewers).toEqual(["u1", "u3"]);
	});

	it("emits typing, expires it and clears on stop", async () => {
		const { hub, emitted, advance } = setup();
		await hub.join("s1", v("u1"), ref);
		hub.typing("s1", "task", T, true);
		expect(emitted.at(-1)?.typing).toEqual(["u1"]);
		advance(TYPING_TTL_MS + 1);
		(hub as unknown as { tick(): void }).tick();
		expect(emitted.at(-1)?.typing).toEqual([]);
		hub.typing("s1", "task", T, true);
		hub.typing("s1", "task", T, false);
		expect(emitted.at(-1)?.typing).toEqual([]);
	});

	it("ignores typing from a read-only viewer or a non-member", async () => {
		const { hub, emitted } = setup({ write: false });
		await hub.join("s1", v("u1"), ref);
		const n = emitted.length;
		hub.typing("s1", "task", T, true);
		hub.typing("nobody", "task", T, true);
		expect(emitted.length).toBe(n);
	});

	it("drops typing when the account's last socket leaves", async () => {
		const { hub, emitted } = setup();
		await hub.join("s1", v("u1"), ref);
		await hub.join("s2", v("u2"), ref);
		hub.typing("s1", "task", T, true);
		hub.leaveAll("s1");
		expect(emitted.at(-1)).toMatchObject({ viewers: ["u2"], typing: [] });
	});

	it("merges another replica's summary and answers a hello", async () => {
		const { hub, emitted, published } = setup();
		await hub.join("s1", v("u1"), ref);
		expect(published.at(-1)?.hello).toBe(true);
		hub.hear({
			replica: "b",
			room: `live:task:${T}`,
			viewers: ["u9"],
			typing: ["u9"],
			hello: true,
		});
		expect(emitted.at(-1)).toMatchObject({
			viewers: ["u1", "u9"],
			typing: ["u9"],
		});
		// replied to the hello with its own summary
		expect(published.at(-1)).toMatchObject({ replica: "a", viewers: ["u1"] });
		// own echoes are ignored
		const n = emitted.length;
		hub.hear({
			replica: "a",
			room: `live:task:${T}`,
			viewers: ["zz"],
			typing: [],
		});
		expect(emitted.length).toBe(n);
	});

	it("ages out a silent replica", async () => {
		const { hub, emitted, advance } = setup();
		await hub.join("s1", v("u1"), ref);
		hub.hear({
			replica: "b",
			room: `live:task:${T}`,
			viewers: ["u9"],
			typing: [],
		});
		advance(30_000);
		(hub as unknown as { tick(): void }).tick();
		expect(emitted.at(-1)?.viewers).toEqual(["u1"]);
	});

	it("recheck evicts a socket that lost read access", async () => {
		const s = setup();
		await s.hub.join("s1", v("u1"), ref);
		(s.hub as unknown as { opts: { decider: unknown } }).opts.decider = {
			can: async () => false,
		};
		const revoked: string[] = [];
		await s.hub.recheck((id) => revoked.push(id));
		expect(revoked).toEqual(["s1"]);
	});
});
