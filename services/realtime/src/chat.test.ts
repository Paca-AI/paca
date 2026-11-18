import { describe, expect, it } from "bun:test";
import type { Logger } from "pino";
import type { Server } from "socket.io";
import { AccessDecider, KeyedQueue } from "./access.ts";
import { ChatHub, MAX_CHANNELS_PER_SOCKET } from "./chat.ts";
import { routeEvent } from "./subscriber.ts";

const P = "11111111-1111-1111-1111-111111111111";
const C = "22222222-2222-2222-2222-222222222222";
const C2 = "33333333-3333-3333-3333-333333333333";
const U1 = "44444444-4444-4444-4444-444444444444";
const U2 = "55555555-5555-5555-5555-555555555555";
const log = {
	debug() {},
	warn() {},
	info() {},
	error() {},
} as unknown as Logger;

function fakeSocket(userId: string) {
	const rooms = new Set<string>();
	const sent: Array<{ event: string; payload: unknown }> = [];
	return {
		rooms,
		sent,
		connected: true,
		data: {
			userId,
			rawToken: `tok-${userId}`,
			chatChannels: new Map<string, string>(),
		},
		join: (r: string) => rooms.add(r),
		leave: (r: string) => rooms.delete(r),
		emit: (event: string, payload: unknown) => sent.push({ event, payload }),
	};
}

function setup(allow: (userId: string, resource: string) => boolean) {
	const sockets = new Map<string, ReturnType<typeof fakeSocket>>();
	const emitted: Array<{ room: string; event: string; payload: unknown }> = [];
	const asked: string[] = [];
	const io = {
		sockets: { sockets },
		to: (room: string) => ({
			emit: (event: string, payload: unknown) =>
				emitted.push({ room, event, payload }),
		}),
	} as unknown as Server;
	const decider = new AccessDecider("http://api", {
		check: async (_u, token, _p, queries) =>
			queries.map((q) => {
				asked.push(`${q.action}:${q.resource}`);
				return allow(token.replace("tok-", ""), q.resource);
			}),
	});
	const hub = new ChatHub(io, decider, log);
	const add = (userId: string) => {
		const s = fakeSocket(userId);
		sockets.set(`${userId}-${sockets.size}`, s);
		return s;
	};
	return { hub, decider, io, emitted, asked, add };
}

// biome-ignore lint/suspicious/noExplicitAny: the fake is structurally a socket
const asSocket = (s: ReturnType<typeof fakeSocket>) => s as any;

describe("ChatHub.join", () => {
	it("joins the channel room only when the API allows channels:read on the channel", async () => {
		const t = setup((u) => u === U1);
		const yes = t.add(U1);
		const no = t.add(U2);
		expect(await t.hub.join(asSocket(yes), P, C)).toEqual({
			ok: true,
			channelId: C,
		});
		expect(await t.hub.join(asSocket(no), P, C)).toEqual({
			ok: false,
			reason: "forbidden",
		});
		expect(yes.rooms.has(`channel:${C}`)).toBe(true);
		expect(no.rooms.has(`channel:${C}`)).toBe(false);
		expect(t.asked[0]).toBe(`channels:read:project/${P}/channel/${C}`);
	});

	it("refuses malformed ids and does not ask the API", async () => {
		const t = setup(() => true);
		const s = t.add(U1);
		for (const [p, c] of [
			["x", C],
			[P, "channel:../../etc"],
			[P, 5],
			[undefined, undefined],
		]) {
			expect(await t.hub.join(asSocket(s), p, c)).toEqual({
				ok: false,
				reason: "invalid",
			});
		}
		expect(t.asked).toHaveLength(0);
		expect(s.rooms.size).toBe(0);
	});

	it("caps the channels one socket may watch", async () => {
		const t = setup(() => true);
		const s = t.add(U1);
		for (let i = 0; i < MAX_CHANNELS_PER_SOCKET; i++) {
			s.data.chatChannels.set(`c${i}`, P);
		}
		expect(await t.hub.join(asSocket(s), P, C)).toEqual({
			ok: false,
			reason: "limit",
		});
	});

	it("fails closed when the API cannot be reached", async () => {
		const sockets = new Map();
		const io = {
			sockets: { sockets },
			to: () => ({ emit() {} }),
		} as unknown as Server;
		const decider = new AccessDecider("http://api", {
			check: async () => {
				throw new Error("down");
			},
		});
		const hub = new ChatHub(io, decider, log);
		const s = fakeSocket(U1);
		expect(await hub.join(asSocket(s), P, C)).toEqual({
			ok: false,
			reason: "unavailable",
		});
		expect(s.rooms.size).toBe(0);
	});
});

describe("direct messages (no project)", () => {
	it("joins after the API confirms channels:read on channel/<id>, for members only", async () => {
		const t = setup((u, resource) => u === U1 && resource === `channel/${C}`);
		const member = t.add(U1);
		const stranger = t.add(U2);
		for (const none of [undefined, null, ""]) {
			expect(await t.hub.join(asSocket(member), none, C)).toEqual({
				ok: true,
				channelId: C,
			});
			t.hub.leave(asSocket(member), C);
		}
		expect(await t.hub.join(asSocket(stranger), undefined, C)).toEqual({
			ok: false,
			reason: "forbidden",
		});
		expect(t.asked[0]).toBe(`channels:read:channel/${C}`);
		expect(member.data.chatChannels.get(C)).toBeUndefined();
	});

	it("re-checks a direct message with no project and revokes when access goes", async () => {
		let allowed = true;
		const t = setup(() => allowed);
		const s = t.add(U1);
		await t.hub.join(asSocket(s), undefined, C);
		expect(s.data.chatChannels.get(C)).toBe("");
		allowed = false;
		t.decider.flush();
		await t.hub.recheck();
		expect(s.rooms.has(`channel:${C}`)).toBe(false);
		expect(s.sent).toEqual([
			{ event: "chat:revoked", payload: { channelId: C } },
		]);
	});

	it("still rejects a malformed project id", async () => {
		const t = setup(() => true);
		expect(await t.hub.join(asSocket(t.add(U1)), "not-a-uuid", C)).toEqual({
			ok: false,
			reason: "invalid",
		});
	});

	it("nudges both members when a direct message is created, without a project", () => {
		const t = setup(() => true);
		routeEvent(
			t.io,
			{
				type: "chat.dm.created",
				payload: { channel_id: C, seq: 0, actor_id: U1 },
				recipient_ids: [U1, U2],
			} as never,
			log,
			{
				decider: t.decider,
				queue: new KeyedQueue(),
				onIamChanged() {},
				warnedNoAuthz: new Set(),
				chat: t.hub,
			},
		);
		expect(
			t.emitted.filter((e) => e.event === "chat:unread").map((e) => e.room),
		).toEqual([`user:${U1}:chat`, `user:${U2}:chat`]);
	});
});

describe("ChatHub.recheck", () => {
	it("takes a socket out of the room once its access is gone", async () => {
		let allowed = true;
		const t = setup(() => allowed);
		const s = t.add(U1);
		await t.hub.join(asSocket(s), P, C);
		await t.hub.join(asSocket(s), P, C2);

		allowed = false;
		t.decider.flush();
		await t.hub.recheck(C);
		expect(s.rooms.has(`channel:${C}`)).toBe(false);
		expect(s.data.chatChannels.has(C)).toBe(false);
		expect(s.sent).toEqual([
			{ event: "chat:revoked", payload: { channelId: C } },
		]);
		// Only the named channel was re-checked.
		expect(s.rooms.has(`channel:${C2}`)).toBe(true);

		await t.hub.recheck();
		expect(s.rooms.has(`channel:${C2}`)).toBe(false);
	});

	it("keeps the room when the check fails", async () => {
		let down = false;
		const t = setup(() => true);
		const s = t.add(U1);
		await t.hub.join(asSocket(s), P, C);
		(t.decider as unknown as { check: unknown }).check = async () => {
			down = true;
			throw new Error("down");
		};
		t.decider.flush();
		await t.hub.recheck();
		expect(down).toBe(true);
		expect(s.rooms.has(`channel:${C}`)).toBe(true);
	});
});

describe("chat announcements through the subscriber", () => {
	function route(t: ReturnType<typeof setup>, msg: Record<string, unknown>) {
		routeEvent(t.io, msg as never, log, {
			decider: t.decider,
			queue: new KeyedQueue(),
			onIamChanged() {},
			warnedNoAuthz: new Set(),
			chat: t.hub,
		});
	}

	it("sends a message nudge to the channel room and members' own rooms, without leaking the recipient list", () => {
		const t = setup(() => true);
		route(t, {
			type: "chat.message.new",
			payload: {
				project_id: P,
				channel_id: C,
				seq: 9,
				message_id: C2,
				actor_id: U1,
			},
			recipient_ids: [U1, U2, "not-a-uuid"],
		});
		expect(t.emitted.map((e) => `${e.room}|${e.event}`)).toEqual([
			`channel:${C}|chat`,
			`user:${U1}:chat|chat:unread`,
			`user:${U2}:chat|chat:unread`,
		]);
		for (const e of t.emitted) {
			expect(JSON.stringify(e.payload)).not.toContain("recipient");
		}
		expect(
			(t.emitted[1]?.payload as { payload: { seq: number } }).payload.seq,
		).toBe(9);
	});

	it("does not nudge unread for a reaction", () => {
		const t = setup(() => true);
		route(t, {
			type: "chat.reaction.changed",
			payload: { project_id: P, channel_id: C, seq: 9, message_id: C2 },
			recipient_ids: [U1],
		});
		expect(t.emitted.map((e) => e.event)).toEqual(["chat"]);
	});

	it("drops an announcement without a channel", () => {
		const t = setup(() => true);
		route(t, {
			type: "chat.message.new",
			payload: { project_id: P },
			recipient_ids: [U1],
		});
		expect(t.emitted).toHaveLength(0);
	});

	it("re-checks a channel's room on an access change and sends nothing to clients", async () => {
		let allowed = true;
		const t = setup(() => allowed);
		const s = t.add(U1);
		await t.hub.join(asSocket(s), P, C);
		allowed = false;
		route(t, {
			type: "chat.channel.access_changed",
			payload: { project_id: P, channel_id: C },
		});
		await new Promise((r) => setTimeout(r, 10));
		expect(s.rooms.has(`channel:${C}`)).toBe(false);
		expect(t.emitted).toHaveLength(0);
	});
});
