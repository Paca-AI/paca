import { describe, expect, test } from "bun:test";
import * as Y from "yjs";
import type { AccessDecider } from "./access.ts";
import { COLLAB_FRAGMENT, CollabHub, parseRoomRef } from "./collab.ts";

const P = "11111111-1111-4111-8111-111111111111";
const D = "22222222-2222-4222-8222-222222222222";
const ref = { projectId: P, kind: "doc" as const, id: D };

function setup(allowed: () => boolean = () => true, seedDeadlineMs = 10_000) {
	const granted: string[] = [];
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
		},
		undefined,
		seedDeadlineMs,
	);
	return { hub, sent, revoked, granted };
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
