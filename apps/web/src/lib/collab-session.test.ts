import type { Socket } from "socket.io-client";
import { describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import {
	COLLAB_FRAGMENT,
	CollabSession,
	colorForUser,
	isCollabDocEmpty,
	seedCollabDoc,
} from "./collab-session";

vi.mock("./socket-client", () => ({ connectSocket: vi.fn() }));

const ref = {
	projectId: "11111111-1111-4111-8111-111111111111",
	kind: "doc" as const,
	id: "22222222-2222-4222-8222-222222222222",
};
const user = { name: "Ada", color: "#fff" };

class FakeSocket {
	private handlers = new Map<string, Set<(p: unknown) => void>>();
	on(event: string, fn: (p: unknown) => void) {
		const set = this.handlers.get(event) ?? new Set();
		set.add(fn);
		this.handlers.set(event, set);
		return this;
	}
	off(event: string, fn: (p: unknown) => void) {
		this.handlers.get(event)?.delete(fn);
		return this;
	}
	connected = true;
	emitted: { event: string; payload: unknown }[] = [];
	joinResult: unknown = {
		ok: true,
		room: "r",
		seed: true,
		update: Y.encodeStateAsUpdate(new Y.Doc()),
		stateVector: Y.encodeStateVector(new Y.Doc()),
		awareness: null,
	};
	timeout() {
		return {
			emit: (
				event: string,
				payload: unknown,
				cb: (e: Error | null, r: unknown) => void,
			) => {
				this.emitted.push({ event, payload });
				cb(null, this.joinResult);
			},
		};
	}
	// Overrides EventEmitter.emit for the client -> server direction.
	emit(event: string, payload?: unknown): boolean {
		this.emitted.push({ event, payload });
		return true;
	}
	fromServer(event: string, payload: unknown) {
		for (const fn of this.handlers.get(event) ?? []) fn(payload);
	}
}

function make(sock = new FakeSocket()) {
	return {
		sock,
		session: new CollabSession(sock as unknown as Socket, ref, user),
	};
}

describe("CollabSession", () => {
	it("joins, exposes the seed flag and becomes ready", async () => {
		const { session } = make();
		expect(await session.join()).toBe(true);
		expect(session.status).toBe("ready");
		expect(session.seed).toBe(true);
	});

	it("retries the join when the socket drops before the ack", async () => {
		const sock = new FakeSocket();
		const ok = sock.joinResult;
		let calls = 0;
		sock.timeout = () => ({
			emit: (
				event: string,
				payload: unknown,
				cb: (e: Error | null, r: unknown) => void,
			) => {
				sock.emitted.push({ event, payload });
				calls++;
				// First attempt: the connection drops, the ack is lost.
				if (calls === 1) sock.fromServer("disconnect", undefined);
				else cb(null, ok);
			},
		});
		const { session } = make(sock);
		expect(await session.join()).toBe(true);
		expect(calls).toBe(2);
		expect(session.status).toBe("ready");
	});

	it("does not retry a join that merely timed out", async () => {
		const sock = new FakeSocket();
		let calls = 0;
		sock.timeout = () => ({
			emit: (
				_event: string,
				_payload: unknown,
				cb: (e: Error | null, r: unknown) => void,
			) => {
				calls++;
				cb(new Error("timeout"), undefined);
			},
		});
		const { session } = make(sock);
		expect(await session.join()).toBe(false);
		expect(calls).toBe(1);
		expect(session.status).toBe("offline");
	});

	it("becomes the seeder later when the server hands the grant over", async () => {
		const sock = new FakeSocket();
		(sock.joinResult as { seed: boolean }).seed = false;
		const { session } = make(sock);
		await session.join();
		const seeded = vi.fn();
		session.whenSeeder(seeded);
		expect(seeded).not.toHaveBeenCalled();
		sock.fromServer("collab:seed", { room: `collab:p:doc:${ref.id}` });
		expect(seeded).toHaveBeenCalledTimes(1);
		expect(session.seed).toBe(true);
	});

	it("reports lost when the server refuses the join", async () => {
		const sock = new FakeSocket();
		sock.joinResult = { ok: false, reason: "forbidden" };
		const { session } = make(sock);
		expect(await session.join()).toBe(false);
		expect(session.status).toBe("lost");
	});

	it("sends local edits and does not echo remote ones", async () => {
		const { sock, session } = make();
		await session.join();
		sock.emitted.length = 0;

		session.fragment.insert(0, [new Y.XmlElement("p")]);
		const sent = sock.emitted.filter((e) => e.event === "collab:update");
		expect(sent).toHaveLength(1);

		const peer = new Y.Doc();
		peer.getXmlFragment(COLLAB_FRAGMENT).insert(0, [new Y.XmlElement("q")]);
		sock.emitted.length = 0;
		sock.fromServer("collab:update", {
			room: ref,
			update: Y.encodeStateAsUpdate(peer),
		});
		expect(session.fragment.length).toBe(2);
		expect(
			sock.emitted.filter((e) => e.event === "collab:update"),
		).toHaveLength(0);
	});

	it("ignores updates for another room", async () => {
		const { sock, session } = make();
		await session.join();
		const peer = new Y.Doc();
		peer.getXmlFragment(COLLAB_FRAGMENT).insert(0, [new Y.XmlElement("q")]);
		sock.fromServer("collab:update", {
			room: { ...ref, id: "33333333-3333-4333-8333-333333333333" },
			update: Y.encodeStateAsUpdate(peer),
		});
		expect(session.fragment.length).toBe(0);
	});

	it("goes offline on disconnect and lost when revoked", async () => {
		const { sock, session } = make();
		await session.join();
		sock.fromServer("disconnect", undefined);
		expect(session.status).toBe("offline");
		sock.fromServer("collab:revoked", { room: `collab:p:doc:${ref.id}` });
		expect(session.status).toBe("lost");
	});
});

describe("seedCollabDoc", () => {
	it("does not overwrite content that is already in the shared doc", async () => {
		vi.useFakeTimers();
		const { session } = make();
		session.fragment.insert(0, [new Y.XmlText("peer typed this")]);
		const write = vi.fn();
		seedCollabDoc(session, write);
		await vi.advanceTimersByTimeAsync(100);
		expect(write).not.toHaveBeenCalled();
		vi.useRealTimers();
	});
});

describe("seed hand-off and server errors", () => {
	it("stops seeding after the server moves the grant to someone else", async () => {
		vi.useFakeTimers();
		const { sock, session } = make();
		await session.join();
		sock.fromServer("collab:seed-cancel", { room: `collab:p:doc:${ref.id}` });
		expect(session.seed).toBe(false);
		const write = vi.fn();
		seedCollabDoc(session, write);
		await vi.advanceTimersByTimeAsync(100);
		expect(write).not.toHaveBeenCalled();
		vi.useRealTimers();
	});

	it("re-joins to resync after the server drops an update", async () => {
		vi.useFakeTimers();
		const { sock, session } = make();
		await session.join();
		const joins = () =>
			sock.emitted.filter((e) => e.event === "collab:join").length;
		const before = joins();
		sock.fromServer("collab:error", {
			room: `collab:p:doc:${ref.id}`,
			reason: "unavailable",
		});
		await vi.advanceTimersByTimeAsync(1_100);
		expect(joins()).toBe(before + 1);
		expect(session.status).toBe("ready");
		vi.useRealTimers();
	});

	it("treats a doc holding only placeholder blocks as empty", () => {
		const { session } = make();
		const c = new Y.XmlElement("blockContainer");
		const p = new Y.XmlElement("paragraph");
		c.insert(0, [p]);
		session.fragment.insert(0, [c]);
		expect(isCollabDocEmpty(session)).toBe(true);
		p.insert(0, [new Y.XmlText("hi")]);
		expect(isCollabDocEmpty(session)).toBe(false);
	});
});

describe("colorForUser", () => {
	it("is stable per user", () => {
		expect(colorForUser("abc")).toBe(colorForUser("abc"));
		expect(colorForUser("abc")).toMatch(/^#[0-9a-f]{6}$/);
	});
});
