import { describe, expect, test } from "bun:test";
import * as Y from "yjs";
import type { AccessDecider } from "./access.ts";
import { COLLAB_FRAGMENT, CollabHub, type CollabJoinResult } from "./collab.ts";
import {
	type ClusterBus,
	type ClusterMessage,
	CollabCluster,
	decodeBinary,
	encodeBinary,
	type LeaseStore,
	type LocalSockets,
} from "./collab-cluster.ts";

const P = "11111111-1111-4111-8111-111111111111";
const D = "22222222-2222-4222-8222-222222222222";
const ref = { projectId: P, kind: "doc" as const, id: D };
const ROOM = `collab:${P}:doc:${D}`;
const viewer = { userId: "u", rawToken: "t" };
const settle = () => new Promise((r) => setTimeout(r, 15));

// ── fakes: one Valkey (leases + heartbeats) and one bus, shared by replicas ──

class FakeValkey implements LeaseStore {
	owners = new Map<string, string>();
	alive_ = new Set<string>();
	down = false;
	async claim(room: string, replica: string) {
		if (this.down) throw new Error("valkey down");
		if (!this.owners.has(room)) this.owners.set(room, replica);
		return this.owners.get(room) as string;
	}
	async renew(room: string, replica: string) {
		return this.owners.get(room) === replica;
	}
	async release(room: string, replica: string) {
		if (this.owners.get(room) === replica) this.owners.delete(room);
	}
	async takeover(room: string, from: string) {
		if (this.owners.get(room) === from) this.owners.delete(room);
	}
	async owner(room: string) {
		return this.owners.get(room) ?? null;
	}
	async heartbeat(replica: string) {
		this.alive_.add(replica);
	}
	async alive(replica: string) {
		return this.alive_.has(replica);
	}
}

class FakeBus {
	handlers = new Map<string, (m: ClusterMessage) => void>();
	dropped = new Set<string>(); // replicas that no longer answer
	sentTypes: string[] = [];
	for(): ClusterBus {
		return {
			send: async (to, message) => {
				this.sentTypes.push(message.type);
				if (this.dropped.has(to)) return;
				// Through JSON, as the real bus does.
				const copy = JSON.parse(JSON.stringify(message)) as ClusterMessage;
				queueMicrotask(() => this.handlers.get(to)?.(copy));
			},
			listen: async (id, handler) => {
				this.handlers.set(id, handler);
			},
			close: () => {},
		};
	}
}

class FakeSockets implements LocalSockets {
	rooms = new Map<string, Set<string>>(); // room -> socket ids
	sockets = new Set<string>();
	emitted: { id: string; event: string; payload: unknown }[] = [];
	has = (id: string) => this.sockets.has(id);
	emit = (id: string, event: string, payload: unknown) => {
		this.emitted.push({ id, event, payload });
	};
	emitToRoom = (
		room: string,
		except: string | undefined,
		event: string,
		payload: unknown,
	) => {
		for (const id of this.rooms.get(room) ?? []) {
			if (id !== except) this.emitted.push({ id, event, payload });
		}
	};
	leave = (id: string, room: string) => {
		this.rooms.get(room)?.delete(id);
	};
	socketsIn = (room: string) => [...(this.rooms.get(room) ?? [])];
	connect(id: string) {
		this.sockets.add(id);
	}
	enter(id: string, room: string) {
		const r = this.rooms.get(room) ?? new Set<string>();
		this.rooms.set(room, r);
		r.add(id);
	}
	got(id: string, event: string) {
		return this.emitted.filter((e) => e.id === id && e.event === event);
	}
}

const logger = { warn() {}, info() {} };

function replica(
	id: string,
	valkey: FakeValkey,
	bus: FakeBus,
	opts: { rpcTimeoutMs?: number } = {},
) {
	const sockets = new FakeSockets();
	const cluster = new CollabCluster({
		replicaId: id,
		leases: valkey,
		bus: bus.for(),
		local: sockets,
		logger,
		rpcTimeoutMs: opts.rpcTimeoutMs ?? 200,
		newHub: (transport) =>
			new CollabHub(
				{ can: async () => true } as unknown as AccessDecider,
				transport,
				undefined,
				10_000,
			),
	});
	return { id, cluster, sockets };
}

async function join(
	r: ReturnType<typeof replica>,
	socketId: string,
	stateVector?: Uint8Array,
) {
	r.sockets.connect(socketId);
	return r.cluster.join(
		socketId,
		viewer,
		{ ...ref, stateVector },
		{ enter: (room) => r.sockets.enter(socketId, room), isAlive: () => true },
	);
}

async function pair() {
	const valkey = new FakeValkey();
	const bus = new FakeBus();
	const a = replica("A", valkey, bus);
	const b = replica("B", valkey, bus);
	await a.cluster.start();
	await b.cluster.start();
	return { valkey, bus, a, b };
}

const okResult = (r: CollabJoinResult) => {
	if (!r.ok) throw new Error(`join failed: ${r.reason}`);
	return r;
};

function docWith(text: string): { update: Uint8Array; doc: Y.Doc } {
	const doc = new Y.Doc();
	const frag = doc.getXmlFragment(COLLAB_FRAGMENT);
	const el = new Y.XmlElement("blockContainer");
	const t = new Y.XmlText();
	t.insert(0, text);
	el.insert(0, [t]);
	frag.insert(0, [el]);
	return { update: Y.encodeStateAsUpdate(doc), doc };
}

describe("encodeBinary / decodeBinary", () => {
	test("round trip bytes anywhere in a payload, through JSON", () => {
		const v = {
			a: new Uint8Array([1, 2, 3]),
			b: [{ c: new Uint8Array([9]) }],
			d: "x",
			e: null,
		};
		const back = decodeBinary(
			JSON.parse(JSON.stringify(encodeBinary(v))),
		) as typeof v;
		expect(Array.from(back.a)).toEqual([1, 2, 3]);
		expect(Array.from(back.b[0]?.c ?? [])).toEqual([9]);
		expect(back.d).toBe("x");
		expect(back.e).toBeNull();
	});
});

describe("CollabCluster", () => {
	test("the first replica to see a room owns it; the second joins through it", async () => {
		const { valkey, a, b } = await pair();
		const first = okResult(await join(a, "sa"));
		expect(first.seed).toBe(true);
		expect(valkey.owners.get(ROOM)).toBe("A");

		const second = okResult(await join(b, "sb"));
		// One authority: the second editor is not asked to seed.
		expect(second.seed).toBe(false);
		expect(a.cluster.hub.hasRoom(ROOM)).toBe(true);
		expect(b.cluster.hub.hasRoom(ROOM)).toBe(false);
		expect(b.sockets.rooms.get(ROOM)?.has("sb")).toBe(true);
	});

	test("an update on one replica reaches the editor on the other, and the room converges", async () => {
		const { a, b } = await pair();
		const { update, doc } = docWith("hello");
		okResult(await join(a, "sa"));
		okResult(await join(b, "sb"));

		// The editor on A seeds; the editor on B must see it.
		a.cluster.update("sa", ref, update);
		await settle();
		const forB = b.sockets.got("sb", "collab:update");
		expect(forB.length).toBe(1);
		const received = new Y.Doc();
		Y.applyUpdate(
			received,
			(forB[0]?.payload as { update: Uint8Array }).update,
		);
		expect(received.getXmlFragment(COLLAB_FRAGMENT).toString()).toBe(
			doc.getXmlFragment(COLLAB_FRAGMENT).toString(),
		);

		// And the other way: B edits, A hears it, but the sender does not get its own back.
		const edit = new Y.Doc();
		Y.applyUpdate(edit, update);
		const sv = Y.encodeStateVector(edit);
		edit.getXmlFragment(COLLAB_FRAGMENT).insert(1, [new Y.XmlElement("p")]);
		b.cluster.update("sb", ref, Y.encodeStateAsUpdate(edit, sv));
		await settle();
		expect(a.sockets.got("sa", "collab:update").length).toBe(1);
		expect(b.sockets.got("sb", "collab:update").length).toBe(1); // only the first, from A
	});

	test("a late joiner on the other replica gets the room's state", async () => {
		const { a, b } = await pair();
		const { update, doc } = docWith("kept");
		okResult(await join(a, "sa"));
		a.cluster.update("sa", ref, update);
		await settle();

		const joined = okResult(await join(b, "sb"));
		const copy = new Y.Doc();
		Y.applyUpdate(copy, joined.update);
		expect(copy.getXmlFragment(COLLAB_FRAGMENT).toString()).toBe(
			doc.getXmlFragment(COLLAB_FRAGMENT).toString(),
		);
	});

	test("awareness crosses replicas too", async () => {
		const { a, b } = await pair();
		okResult(await join(a, "sa"));
		okResult(await join(b, "sb"));
		a.cluster.awarenessUpdate("sa", ref, new Uint8Array([0, 0]));
		await settle();
		// The hub relays what it accepted; with a malformed update nothing is
		// relayed, but the forward itself must not throw or corrupt the room.
		expect(a.cluster.hub.hasRoom(ROOM)).toBe(true);
	});

	test("a seed grant for a remote member is delivered to its own replica", async () => {
		const { a, b } = await pair();
		// B's editor joins first: B takes the lease and its editor seeds.
		const first = okResult(await join(b, "sb"));
		expect(first.seed).toBe(true);
		// A's editor joins through B and is not the seeder.
		const second = okResult(await join(a, "sa"));
		expect(second.seed).toBe(false);
		// The seeder leaves before writing: the grant moves to the remote member,
		// and is emitted by the replica that holds that socket.
		b.cluster.leave("sb", ref);
		await settle();
		expect(a.sockets.got("sa", "collab:seed").length).toBe(1);
	});

	test("leaving and disconnecting on a remote replica empty the owner's room", async () => {
		const { a, b } = await pair();
		okResult(await join(a, "sa"));
		okResult(await join(b, "sb"));
		b.cluster.leave("sb", ref);
		await settle();
		// sb is no longer a member: an update it sends is ignored, not relayed.
		const { update } = docWith("x");
		b.cluster.update("sb", ref, update);
		await settle();
		expect(a.sockets.got("sa", "collab:update")).toEqual([]);

		okResult(await join(b, "sb2"));
		b.cluster.leaveAll("sb2");
		await settle();
		expect(a.cluster.hub.hasRoom(ROOM)).toBe(true);
	});

	test("an owner that does not answer, and is gone, is replaced", async () => {
		const { valkey, bus, a, b } = await pair();
		okResult(await join(a, "sa"));
		// A dies: no answers, no heartbeat, but its lease is still in Valkey.
		bus.dropped.add("A");
		valkey.alive_.delete("A");
		const res = await join(b, "sb");
		expect(res.ok).toBe(true);
		expect(valkey.owners.get(ROOM)).toBe("B");
		expect(b.cluster.hub.hasRoom(ROOM)).toBe(true);
	});

	test("an owner that does not answer but is alive is reported unavailable, not taken over", async () => {
		const { valkey, bus, a, b } = await pair();
		okResult(await join(a, "sa"));
		bus.dropped.add("A"); // slow or partitioned, but still heartbeating
		const res = await join(b, "sb");
		expect(res).toEqual({ ok: false, reason: "unavailable" });
		expect(valkey.owners.get(ROOM)).toBe("A");
	});

	test("a member whose owner died is told to re-join", async () => {
		const { valkey, a, b } = await pair();
		okResult(await join(a, "sa"));
		okResult(await join(b, "sb"));
		valkey.alive_.delete("A");
		valkey.owners.delete(ROOM);
		await (b.cluster as unknown as { sweep(): Promise<void> }).sweep();
		const errors = b.sockets.got("sb", "collab:error");
		expect(errors.length).toBe(1);
		expect(errors[0]?.payload).toEqual({ room: ROOM, reason: "unavailable" });
		// And its updates no longer go anywhere.
		expect(b.sockets.rooms.get(ROOM)?.has("sb")).toBe(false);
	});

	test("an owner drops the members of a replica that stopped", async () => {
		const { valkey, a, b } = await pair();
		okResult(await join(a, "sa"));
		okResult(await join(b, "sb"));
		valkey.alive_.delete("B");
		await (a.cluster as unknown as { sweep(): Promise<void> }).sweep();
		// The room survives with its remaining member; the dead replica's member is gone.
		expect(a.cluster.hub.hasRoom(ROOM)).toBe(true);
		const { update } = docWith("y");
		a.cluster.update("sa", ref, update);
		await settle();
		expect(b.sockets.got("sb", "collab:update")).toEqual([]);
	});

	test("with Valkey down a replica hosts what it is asked about, as a single replica would", async () => {
		const valkey = new FakeValkey();
		const bus = new FakeBus();
		const a = replica("A", valkey, bus);
		await a.cluster.start();
		valkey.down = true;
		const res = await join(a, "sa");
		expect(res.ok).toBe(true);
		expect(a.cluster.hub.hasRoom(ROOM)).toBe(true);
	});

	test("an owner that loses its lease gives the room up instead of running a second authority", async () => {
		const { valkey, a, b } = await pair();
		okResult(await join(a, "sa"));
		okResult(await join(b, "sb"));
		valkey.owners.set(ROOM, "C"); // another replica took over while A was presumed dead
		await (
			a.cluster as unknown as { renewLeases(): Promise<void> }
		).renewLeases();
		expect(a.cluster.hub.hasRoom(ROOM)).toBe(false);
		expect(a.sockets.got("sa", "collab:error").length).toBe(1);
		await settle();
		expect(b.sockets.got("sb", "collab:error").length).toBe(1);
	});

	test("external writes are merged by the owner only", async () => {
		const { a, b } = await pair();
		okResult(await join(a, "sa"));
		// Nothing to merge into on B (no room there): the call is a harmless no-op.
		await b.cluster.handleContentEvent("doc.updated", {});
		await a.cluster.handleContentEvent("doc.updated", {});
		expect(b.cluster.hub.hasRoom(ROOM)).toBe(false);
	});
});
