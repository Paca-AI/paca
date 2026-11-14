import { describe, expect, test } from "bun:test";
import { BlockNoteEditor } from "@blocknote/core";
import { blocksToYDoc } from "@blocknote/core/yjs";
import { Awareness, encodeAwarenessUpdate } from "y-protocols/awareness";
import * as Y from "yjs";
import type { AccessDecider } from "./access.ts";
import {
	COLLAB_FRAGMENT,
	CollabHub,
	isEmpty,
	MAX_ROOM_BYTES,
	parseContentChange,
	parseRoomRef,
} from "./collab.ts";

const P = "11111111-1111-4111-8111-111111111111";
const D = "22222222-2222-4222-8222-222222222222";
const ref = { projectId: P, kind: "doc" as const, id: D };

function setup(
	allowed: () => boolean | Promise<boolean> = () => true,
	seedDeadlineMs = 10_000,
) {
	const granted: string[] = [];
	const cancelled: string[] = [];
	const errors: { id: string; reason: string }[] = [];
	const sent: {
		room: string;
		except?: string;
		event: string;
		payload: unknown;
	}[] = [];
	const revoked: string[] = [];
	const decider = {
		can: async () => allowed(),
	} as unknown as AccessDecider;
	const hub = new CollabHub(
		decider,
		{
			broadcast: (room, except, event, payload) =>
				sent.push({ room, except, event, payload }),
			revoke: (id) => revoked.push(id),
			grantSeed: (id) => granted.push(id),
			cancelSeed: (id) => cancelled.push(id),
			error: (id, _room, reason) => errors.push({ id, reason }),
		},
		undefined,
		seedDeadlineMs,
	);
	return { hub, sent, revoked, granted, cancelled, errors };
}

const viewer = { userId: "u", rawToken: "t" };
const tick = () => new Promise((r) => setTimeout(r, 5));

function contentUpdate(): Uint8Array {
	const d = new Y.Doc();
	const el = new Y.XmlElement("blockContainer");
	d.getXmlFragment(COLLAB_FRAGMENT).insert(0, [el]);
	return Y.encodeStateAsUpdate(d);
}

describe("parseRoomRef", () => {
	test("rejects bad ids and kinds", () => {
		expect(parseRoomRef({ ...ref, id: "x/../y" })).toBeUndefined();
		expect(parseRoomRef({ ...ref, kind: "sprint" })).toBeUndefined();
		expect(parseRoomRef(ref)).toEqual(ref);
	});
});

describe("CollabHub", () => {
	test("forbidden without write access", async () => {
		const { hub } = setup(() => false);
		const res = await hub.join("a", viewer, ref);
		expect(res).toEqual({ ok: false, reason: "forbidden" });
		expect(hub.roomCount).toBe(0);
	});

	test("only the first joiner of an empty room seeds", async () => {
		const { hub } = setup();
		const a = await hub.join("a", viewer, ref);
		const b = await hub.join("b", viewer, ref);
		expect(a.ok && a.seed).toBe(true);
		expect(b.ok && b.seed).toBe(false);
	});

	test("seed passes on when the seeder leaves before writing", async () => {
		const { hub } = setup();
		await hub.join("a", viewer, ref);
		hub.leave("a", ref);
		const b = await hub.join("b", viewer, ref);
		expect(b.ok && b.seed).toBe(true);
	});

	test("updates are relayed to peers and served to later joiners", async () => {
		const { hub, sent } = setup();
		await hub.join("a", viewer, ref);
		const u = contentUpdate();
		hub.update("a", ref, u);
		await tick();
		expect(
			sent.some((s) => s.event === "collab:update" && s.except === "a"),
		).toBe(true);
		const b = await hub.join("b", viewer, ref);
		expect(b.ok && b.seed).toBe(false);
		if (!b.ok) throw new Error("join failed");
		const peer = new Y.Doc();
		Y.applyUpdate(peer, b.update);
		expect(peer.getXmlFragment(COLLAB_FRAGMENT).length).toBe(1);
	});

	test("an update after access is revoked evicts the member", async () => {
		let ok = true;
		const { hub, revoked, sent } = setup(() => ok);
		await hub.join("a", viewer, ref);
		ok = false;
		hub.update("a", ref, contentUpdate());
		await tick();
		expect(revoked).toEqual(["a"]);
		expect(sent.filter((s) => s.event === "collab:update")).toHaveLength(0);
	});

	test("recheck evicts members who lost access", async () => {
		let ok = true;
		const { hub, revoked } = setup(() => ok);
		await hub.join("a", viewer, ref);
		ok = false;
		await hub.recheck();
		expect(revoked).toEqual(["a"]);
	});

	test("oversized or malformed updates are ignored", async () => {
		const { hub, sent } = setup();
		await hub.join("a", viewer, ref);
		hub.update("a", ref, "not bytes");
		hub.update("a", ref, new Uint8Array([255, 255, 255]));
		await tick();
		expect(sent.filter((s) => s.event === "collab:update")).toHaveLength(0);
		hub.close();
	});
});

describe("CollabHub placeholder content", () => {
	test("an empty placeholder paragraph does not stop the room being seeded", async () => {
		const { hub } = setup();
		await hub.join("a", viewer, ref);
		// An editor mounted and wrote only BlockNote's empty placeholder block.
		const d = new Y.Doc();
		const container = new Y.XmlElement("blockContainer");
		container.insert(0, [new Y.XmlElement("paragraph")]);
		d.getXmlFragment(COLLAB_FRAGMENT).insert(0, [container]);
		hub.update("a", ref, Y.encodeStateAsUpdate(d));
		await tick();
		hub.leave("a", ref);
		const b = await hub.join("b", viewer, ref);
		expect(b.ok && b.seed).toBe(true);
	});
});

describe("CollabHub hardening", () => {
	test("a join whose socket died during the access check leaves nothing behind", async () => {
		const { hub } = setup();
		const res = await hub.join("a", viewer, ref, { isAlive: () => false });
		expect(res).toEqual({ ok: false, reason: "unavailable" });
		expect(hub.roomCount).toBe(0);
		// And it holds no seed grant: the next joiner can seed.
		const b = await hub.join("b", viewer, ref);
		expect(b.ok && b.seed).toBe(true);
	});

	test("the seed moves to another member when the seeder leaves", async () => {
		const { hub, granted } = setup();
		await hub.join("a", viewer, ref);
		const b = await hub.join("b", viewer, ref);
		expect(b.ok && b.seed).toBe(false);
		hub.leaveAll("a");
		expect(granted).toEqual(["b"]);
		hub.close();
	});

	test("the seed moves on when the seeder never writes within the deadline", async () => {
		const { hub, granted } = setup(() => true, 20);
		await hub.join("a", viewer, ref);
		await hub.join("b", viewer, ref);
		await new Promise((r) => setTimeout(r, 60));
		expect(granted).toContain("b");
		hub.close();
	});

	test("the seed grant does not cycle between idle members forever", async () => {
		const { hub, granted } = setup(() => true, 15);
		await hub.join("a", viewer, ref);
		await hub.join("b", viewer, ref);
		await new Promise((r) => setTimeout(r, 120));
		// a -> b once; then everyone has been tried and the timer stops.
		expect(granted).toEqual(["b"]);
		// A brand-new joiner can still take the free grant.
		const c = await hub.join("c", viewer, ref);
		expect(c.ok && c.seed).toBe(true);
		hub.close();
	});

	test("one account cannot hold more than its share of rooms", async () => {
		const { hub } = setup();
		let last: unknown;
		for (let i = 0; i < 12; i++) {
			const id = `00000000-0000-4000-8000-${String(i).padStart(12, "0")}`;
			last = await hub.join("a", viewer, { ...ref, id });
		}
		expect(last).toEqual({ ok: false, reason: "full" });
		// Another account is unaffected.
		const other = await hub.join("o", { userId: "other", rawToken: "t" }, ref);
		expect(other.ok).toBe(true);
		hub.close();
	});
});

describe("CollabHub reconnect", () => {
	test("a reconnecting editor syncs what each side missed, both ways", async () => {
		const { hub, sent } = setup();
		const a = new Y.Doc();
		const b = new Y.Doc();
		const frag = (d: Y.Doc) => d.getXmlFragment(COLLAB_FRAGMENT);
		const toRoom = (d: Y.Doc, id: string) =>
			d.on("update", (u, origin) => {
				if (origin !== "remote") hub.update(id, ref, u);
			});

		// Both join an empty room; A seeds.
		const ja = await hub.join("a", viewer, ref);
		const jb = await hub.join("b1", viewer, ref);
		if (!ja.ok || !jb.ok) throw new Error("join failed");
		toRoom(a, "a");
		frag(a).insert(0, [new Y.XmlElement("one")]);
		await tick();
		// B's first socket is gone (reconnect), A keeps editing meanwhile.
		hub.leaveAll("b1");
		frag(a).insert(1, [new Y.XmlElement("two")]);
		await tick();
		// B edits offline, then rejoins on a new socket id.
		frag(b).insert(0, [new Y.XmlElement("from-b")]);
		const rejoin = await hub.join("b2", viewer, {
			...ref,
			stateVector: Y.encodeStateVector(b),
		});
		if (!rejoin.ok) throw new Error("rejoin failed");
		expect(rejoin.seed).toBe(false);
		Y.applyUpdate(b, rejoin.update, "remote");
		const missing = Y.encodeStateAsUpdate(b, rejoin.stateVector);
		sent.length = 0;
		hub.update("b2", ref, missing);
		await tick();

		// A is told about B's edit, and B got A's.
		const toA = sent.find(
			(s) => s.event === "collab:update" && s.except === "b2",
		);
		expect(toA).toBeDefined();
		Y.applyUpdate(a, (toA?.payload as { update: Uint8Array }).update, "remote");
		expect(frag(a).length).toBe(3);
		expect(frag(b).length).toBe(3);
		hub.close();
	});
});

describe("CollabHub review fixes", () => {
	test("a failed access check does not evict editors or drop their place", async () => {
		let fail = false;
		const { hub, revoked, errors } = setup(async () => {
			if (fail) throw new Error("api down");
			return true;
		});
		await hub.join("a", viewer, ref);
		fail = true;
		await hub.recheck();
		expect(revoked).toEqual([]);
		hub.update("a", ref, contentUpdate());
		await tick();
		expect(revoked).toEqual([]);
		expect(errors).toEqual([{ id: "a", reason: "unavailable" }]);
		// Still a member once the API is back.
		fail = false;
		hub.update("a", ref, contentUpdate());
		await tick();
		expect(revoked).toEqual([]);
		const j = await hub.join("b", viewer, ref);
		expect(j.ok).toBe(true);
		hub.close();
	});

	test("a join during an access outage is unavailable, not forbidden", async () => {
		const { hub } = setup(async () => {
			throw new Error("api down");
		});
		expect(await hub.join("a", viewer, ref)).toEqual({
			ok: false,
			reason: "unavailable",
		});
	});

	test("the previous seeder is told to stop when the grant moves on", async () => {
		const { hub, granted, cancelled } = setup(() => true, 20);
		await hub.join("a", viewer, ref);
		await hub.join("b", viewer, ref);
		await new Promise((r) => setTimeout(r, 60));
		expect(granted).toContain("b");
		expect(cancelled).toEqual(["a"]);
		hub.close();
	});

	test("a member cannot overwrite another member's awareness", async () => {
		const { hub, sent } = setup();
		await hub.join("a", viewer, ref);
		await hub.join("b", viewer, ref);
		const mk = (clientID: number, state: object | null) => {
			const d = new Y.Doc();
			d.clientID = clientID;
			const aw = new Awareness(d);
			aw.setLocalState(state);
			return encodeAwarenessUpdate(aw, [clientID]);
		};
		hub.awarenessUpdate("a", ref, mk(111, { user: "a" }));
		sent.length = 0;
		hub.awarenessUpdate("b", ref, mk(111, null));
		expect(sent).toHaveLength(0);
		hub.awarenessUpdate("b", ref, mk(222, { user: "b" }));
		expect(sent.filter((s) => s.event === "collab:awareness")).toHaveLength(1);
		hub.close();
	});

	test("a room stops accepting updates past its size budget", async () => {
		const { hub, errors } = setup();
		await hub.join("a", viewer, ref);
		const big = new Y.Doc();
		big
			.getXmlFragment(COLLAB_FRAGMENT)
			.insert(0, [new Y.XmlText("x".repeat(4_000_000))]);
		const u = Y.encodeStateAsUpdate(big);
		for (let i = 0; i < 20; i++) hub.update("a", ref, u);
		await new Promise((r) => setTimeout(r, 50));
		expect(errors.some((e) => e.reason === "too_large")).toBe(true);
		hub.close();
	});

	test("a flood of messages from one socket is rate limited", async () => {
		const { hub, errors } = setup();
		await hub.join("a", viewer, ref);
		for (let i = 0; i < 400; i++) hub.update("a", ref, new Uint8Array([0, 0]));
		expect(errors.some((e) => e.reason === "rate")).toBe(true);
		hub.close();
	});

	test("updates waiting on a stalled access check are capped", async () => {
		let release: () => void = () => {};
		const gate = new Promise<void>((r) => {
			release = r;
		});
		let stall = false;
		const { hub, errors } = setup(async () => {
			if (stall) await gate;
			return true;
		});
		await hub.join("a", viewer, ref);
		stall = true;
		for (let i = 0; i < 150; i++) hub.update("a", ref, new Uint8Array([0, 0]));
		expect(errors.some((e) => e.reason === "backlog")).toBe(true);
		release();
		await tick();
		hub.close();
	});

	test("isEmpty ignores placeholders but sees text and other blocks", () => {
		const d = new Y.Doc();
		const f = d.getXmlFragment(COLLAB_FRAGMENT);
		const c = new Y.XmlElement("blockContainer");
		const p = new Y.XmlElement("paragraph");
		c.insert(0, [p]);
		f.insert(0, [c]);
		expect(isEmpty(d)).toBe(true);
		p.insert(0, [new Y.XmlText("hi")]);
		expect(isEmpty(d)).toBe(false);
	});
});

describe("external writes (agent / REST) into a live room", () => {
	const ed = BlockNoteEditor.create();
	const para = (id: string, text: string) => ({
		id,
		type: "paragraph",
		content: text,
		children: [],
	});
	const stored = [para("a", "alpha"), para("b", "bravo")];

	// A room with two editors whose doc holds `stored`.
	async function liveRoom(editors = ["u1", "u2"]) {
		const t = setup();
		const seedDoc = blocksToYDoc(ed, stored as never, COLLAB_FRAGMENT);
		for (const [i, userId] of editors.entries()) {
			await t.hub.join(`s${i + 1}`, { userId, rawToken: "t" }, ref);
		}
		t.hub.update("s1", ref, Y.encodeStateAsUpdate(seedDoc));
		await tick();
		t.sent.length = 0;
		return t;
	}
	const event = (oldC: unknown[], newC: unknown[], actor = "agent") => ({
		project_id: P,
		document_id: D,
		actor_id: actor,
		activity_type: "doc.updated",
		content: JSON.stringify({
			changes: [{ field: "content", old: oldC, new: newC }],
		}),
	});

	test("parseContentChange reads docs and task descriptions", () => {
		const c = parseContentChange("doc.updated", event(stored, []));
		expect(c?.ref).toEqual(ref);
		expect(c?.old).toHaveLength(2);
		const task = parseContentChange("task.updated", {
			project_id: P,
			task_id: D,
			content: JSON.stringify({
				changes: [{ field: "description", old: null, new: stored }],
			}),
		});
		expect(task?.ref.kind).toBe("task");
		expect(task?.old).toEqual([]);
		expect(
			parseContentChange("doc.updated", { ...event([], []), content: "{}" }),
		).toBeUndefined();
		expect(
			parseContentChange("task.updated", {
				project_id: P,
				task_id: D,
				content: JSON.stringify({ changes: [{ field: "title" }] }),
			}),
		).toBeUndefined();
	});

	test("merges an agent's write and broadcasts it to every editor", async () => {
		const { hub, sent } = await liveRoom();
		await hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "bravo by agent")]),
		);
		expect(sent).toHaveLength(1);
		expect(sent[0]?.event).toBe("collab:update");
		expect(sent[0]?.except).toBeUndefined();
	});

	test("ignores the save of an editor in the room", async () => {
		const { hub, sent } = await liveRoom();
		await hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "edited")], "u2"),
			{ collabSocket: "s2" },
		);
		expect(sent).toHaveLength(0);
	});

	test("merges a write by an editor's own account made outside the room (API key, MCP)", async () => {
		// u2 is editing in the browser while an agent writes with u2's personal
		// key: same account, but not the editor's save, so it must be merged.
		const { hub, sent } = await liveRoom();
		await hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "bravo by agent")], "u2"),
		);
		expect(sent).toHaveLength(1);
		expect(sent[0]?.event).toBe("collab:update");
	});

	test("merges a write from a browser socket that is not editing this room", async () => {
		const { hub, sent } = await liveRoom();
		await hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "bravo elsewhere")], "u2"),
			{ collabSocket: "s9" },
		);
		expect(sent).toHaveLength(1);
	});

	test("does nothing when nobody has the resource open", async () => {
		const { hub, sent } = setup();
		await hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "x")]),
		);
		expect(sent).toHaveLength(0);
		expect(hub.roomCount).toBe(0);
	});

	test("an unchanged merge broadcasts nothing", async () => {
		const { hub, sent } = await liveRoom();
		await hub.handleContentEvent("doc.updated", event(stored, stored));
		expect(sent).toHaveLength(0);
	});

	test("waits for editor updates still in their access check before merging", async () => {
		let release: () => void = () => {};
		let slow = false;
		const gate = new Promise<void>((r) => {
			release = r;
		});
		const t = setup(async () => {
			if (slow) await gate;
			return true;
		});
		const seedDoc = blocksToYDoc(ed, stored as never, COLLAB_FRAGMENT);
		await t.hub.join("s1", { userId: "u1", rawToken: "t" }, ref);
		t.hub.update("s1", ref, Y.encodeStateAsUpdate(seedDoc));
		await tick();

		// An editor types into "bravo"; its update is stuck on the access check.
		const client = new Y.Doc();
		Y.applyUpdate(client, Y.encodeStateAsUpdate(seedDoc));
		const before = Y.encodeStateVector(client);
		const para1 = client
			.getXmlFragment(COLLAB_FRAGMENT)
			.toArray()[0] as Y.XmlElement;
		const bravo = para1.toArray()[1] as Y.XmlElement;
		((bravo.get(0) as Y.XmlElement).get(0) as Y.XmlText).insert(0, "LIVE ");
		slow = true;
		t.sent.length = 0;
		t.hub.update("s1", ref, Y.encodeStateAsUpdate(client, before));

		// Meanwhile the agent rewrites the same block.
		const merged = t.hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "bravo by agent")]),
		);
		await tick();
		expect(t.sent.some((m) => m.event === "collab:update")).toBe(false);
		release();
		await merged;
		await tick();
		// The editor's keystrokes were applied first; the merge saw them and
		// left the block alone instead of deleting the node they target.
		const room = (
			t.hub as unknown as { rooms: Map<string, { doc: Y.Doc }> }
		).rooms.get(`collab:${P}:doc:${D}`);
		const text = room?.doc.getXmlFragment(COLLAB_FRAGMENT).toString() ?? "";
		expect(text).toContain("LIVE bravo");
		expect(text).not.toContain("by agent");
	});

	// The room every editor left is kept for a grace period. A write made
	// then cannot be merged (nobody would receive it), so the room must not
	// survive it: the next editor would get the old content from the room and
	// their next save would overwrite the write.
	const roomsOf = (hub: CollabHub) =>
		(hub as unknown as { rooms: Map<string, unknown> }).rooms;
	const docOf = (update: Uint8Array) => {
		const d = new Y.Doc();
		Y.applyUpdate(d, update);
		return d;
	};

	test("drops the room when the stored content changes after every editor left", async () => {
		const { hub, sent } = await liveRoom();
		hub.leave("s1", ref);
		hub.leave("s2", ref);
		expect(hub.roomCount).toBe(1); // still in its grace period

		const written = [para("a", "alpha"), para("b", "bravo by agent")];
		await hub.handleContentEvent("doc.updated", event(stored, written));
		expect(hub.roomCount).toBe(0);
		expect(sent.some((m) => m.event === "collab:update")).toBe(false);

		// The next editor gets a fresh, empty room and the seed grant, so it
		// writes the stored content (the agent's) into the room.
		const res = await hub.join("s3", { userId: "u3", rawToken: "t" }, ref);
		expect(res.ok).toBe(true);
		if (!res.ok) return;
		expect(res.seed).toBe(true);
		expect(isEmpty(docOf(res.update))).toBe(true);

		// It seeds the written content; nothing of the old room comes back.
		const seedDoc = blocksToYDoc(ed, written as never, COLLAB_FRAGMENT);
		hub.update("s3", ref, Y.encodeStateAsUpdate(seedDoc));
		await tick();
		const again = await hub.join("s4", { userId: "u4", rawToken: "t" }, ref);
		expect(again.ok && again.seed).toBe(false);
		if (!again.ok) return;
		const text = docOf(again.update).getXmlFragment(COLLAB_FRAGMENT).toString();
		expect(text).toContain("bravo by agent");
		expect(text.match(/alpha/g)).toHaveLength(1);
		expect(text).not.toMatch(/bravo(?! by agent)/);
	});

	test("drops the room when its last editor leaves while the write waits on their updates", async () => {
		let release: () => void = () => {};
		let slow = false;
		const gate = new Promise<void>((r) => {
			release = r;
		});
		const t = setup(async () => {
			if (slow) await gate;
			return true;
		});
		const seedDoc = blocksToYDoc(ed, stored as never, COLLAB_FRAGMENT);
		await t.hub.join("s1", { userId: "u1", rawToken: "t" }, ref);
		t.hub.update("s1", ref, Y.encodeStateAsUpdate(seedDoc));
		await tick();

		// An update of s1 is stuck on its access check when the write arrives.
		slow = true;
		t.hub.update("s1", ref, contentUpdate());
		const merged = t.hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "bravo by agent")]),
		);
		await tick();
		t.hub.leave("s1", ref);
		release();
		await merged;
		expect(t.hub.roomCount).toBe(0);
	});

	test("a join racing the write ends up in a fresh room, and the old grace timer cannot remove it", async () => {
		let release: () => void = () => {};
		let slow = false;
		const gate = new Promise<void>((r) => {
			release = r;
		});
		const t = setup(async () => {
			if (slow) await gate;
			return true;
		});
		const seedDoc = blocksToYDoc(ed, stored as never, COLLAB_FRAGMENT);
		await t.hub.join("s1", { userId: "u1", rawToken: "t" }, ref);
		t.hub.update("s1", ref, Y.encodeStateAsUpdate(seedDoc));
		await tick();
		t.hub.leave("s1", ref);
		const old = roomsOf(t.hub).get(`collab:${P}:doc:${D}`) as {
			destroyTimer?: unknown;
		};
		expect(old.destroyTimer).toBeDefined();

		// A join is waiting on its access check while the write drops the room.
		slow = true;
		const joining = t.hub.join("s2", { userId: "u2", rawToken: "t" }, ref);
		await t.hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "bravo by agent")]),
		);
		expect(t.hub.roomCount).toBe(0);
		release();
		const res = await joining;
		expect(res.ok && res.seed).toBe(true);
		if (!res.ok) return;
		expect(isEmpty(docOf(res.update))).toBe(true);
		const fresh = roomsOf(t.hub).get(`collab:${P}:doc:${D}`);
		expect(fresh).toBeDefined();
		expect(fresh).not.toBe(old);
		// The dropped room's grace timer was cleared with it.
		expect(old.destroyTimer).toBeUndefined();
	});

	test("never drops a room that has editors", async () => {
		const { hub } = await liveRoom();
		hub.leave("s1", ref);
		await hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "bravo by agent")]),
		);
		expect(hub.roomCount).toBe(1);
		// Even when the write is the save of the remaining editor.
		await hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "edited")], "u2"),
			{ collabSocket: "s2" },
		);
		expect(hub.roomCount).toBe(1);
	});

	test("refuses an external write once the room's byte budget is spent", async () => {
		const { hub, sent } = await liveRoom();
		const rooms = (hub as unknown as { rooms: Map<string, { bytes: number }> })
			.rooms;
		for (const r of rooms.values()) r.bytes = MAX_ROOM_BYTES;
		await hub.handleContentEvent(
			"doc.updated",
			event(stored, [para("a", "alpha"), para("b", "bravo by agent")]),
		);
		expect(sent).toHaveLength(0);
	});
});
