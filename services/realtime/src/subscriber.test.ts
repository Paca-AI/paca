import { describe, expect, it } from "bun:test";
import type { Logger } from "pino";
import type { Server } from "socket.io";

import { AccessDecider, KeyedQueue, type Viewer } from "./access.ts";
import { routeEvent } from "./subscriber.ts";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const P = "11111111-1111-1111-1111-111111111111";
const log = {
	debug() {},
	warn() {},
	info() {},
	error() {},
} as unknown as Logger;

// fakeIo has rooms of fake sockets; `emitted` records room-wide emits.
function fakeIo(rooms: Record<string, Array<ReturnType<typeof socket>>>) {
	const emitted: Array<{ room: string; event: unknown }> = [];
	const io = {
		in: (room: string) => ({ fetchSockets: async () => rooms[room] ?? [] }),
		to: (room: string) => ({
			emit: (_n: string, event: unknown) => emitted.push({ room, event }),
		}),
	} as unknown as Server;
	return { io, emitted };
}

function socket(userId: string) {
	const sent: unknown[] = [];
	return {
		sent,
		data: { userId, rawToken: `tok-${userId}` } satisfies Viewer,
		emit: (_n: string, ev: unknown) => sent.push(ev),
		disconnect() {},
	};
}

function routing(
	allow: (userId: string, resource: string, action: string) => boolean,
	extra: { onIamChanged?: () => void } = {},
) {
	const asked: string[] = [];
	return {
		asked,
		routing: {
			decider: new AccessDecider("http://api", {
				check: async (_u, token, _p, queries) =>
					queries.map((x) => {
						asked.push(`${token}:${x.action}:${x.resource}`);
						return allow(token.replace("tok-", ""), x.resource, x.action);
					}),
			}),
			queue: new KeyedQueue(),
			onIamChanged: extra.onIamChanged ?? (() => {}),
			warnedNoAuthz: new Set<string>(),
		},
	};
}

const authzFor = (action: string, resource: string) => ({ action, resource });

describe("routeEvent with an authz descriptor", () => {
	it("delivers only to sockets the API allows on the event's resource", async () => {
		const open = socket("open");
		const restricted = socket("restricted");
		const { io } = fakeIo({
			[`project:${P}:tasks`]: [open, restricted],
		});
		const secret = `project/${P}/task/secret`;
		const r = routing((u, res) => !(u === "restricted" && res === secret));

		routeEvent(
			io,
			{
				type: "task.updated",
				payload: { id: "secret" },
				authz: authzFor("tasks:read", secret),
			},
			log,
			r.routing,
		);
		routeEvent(
			io,
			{
				type: "task.updated",
				payload: { id: "other" },
				authz: authzFor("tasks:read", `project/${P}/task/other`),
			},
			log,
			r.routing,
		);
		await sleep(20);

		expect(open.sent.length).toBe(2);
		expect(restricted.sent).toEqual([
			{ type: "task.updated", payload: { id: "other" } },
		]);
	});

	it("takes the project from the resource, not the payload", async () => {
		const s = socket("u");
		const { io } = fakeIo({ [`project:${P}:docs`]: [s] });
		routeEvent(
			io,
			{
				type: "doc.updated",
				payload: { project_id: "someone-elses-project" },
				authz: authzFor("docs:read", `project/${P}/doc/d1`),
			},
			log,
			routing(() => true).routing,
		);
		await sleep(20);
		expect(s.sent.length).toBe(1);
	});

	it("routes view events to the views room (views:read)", async () => {
		const s = socket("u");
		const { io } = fakeIo({ [`project:${P}:views`]: [s] });
		routeEvent(
			io,
			{
				type: "view.updated",
				payload: {},
				authz: authzFor("views:read", `project/${P}/view/v1`),
			},
			log,
			routing(() => true).routing,
		);
		await sleep(20);
		expect(s.sent.length).toBe(1);
	});

	it("drops a descriptor whose action has no room instead of guessing", async () => {
		const s = socket("u");
		const { io, emitted } = fakeIo({ [`project:${P}:tasks`]: [s] });
		routeEvent(
			io,
			{
				type: "task.updated",
				payload: {},
				authz: authzFor("tasks:write", `project/${P}/task/t`),
			},
			log,
			routing(() => true).routing,
		);
		await sleep(20);
		expect(s.sent).toEqual([]);
		expect(emitted).toEqual([]);
	});

	it("keeps the events of one resource in order", async () => {
		const s = socket("u");
		const { io } = fakeIo({ [`project:${P}:tasks`]: [s] });
		let n = 0;
		const r = routing(() => true);
		// The first event's check is the slow one (a cache miss); the second
		// finds the answer cached.
		const slow = new AccessDecider("http://api", {
			check: async (_u, _t, _p, queries) => {
				if (n++ === 0) await sleep(30);
				return queries.map(() => true);
			},
		});
		const resource = `project/${P}/task/t`;
		for (const id of ["first", "second"]) {
			routeEvent(
				io,
				{
					type: "task.updated",
					payload: { id },
					authz: authzFor("tasks:read", resource),
				},
				log,
				{ ...r.routing, decider: slow },
			);
		}
		await sleep(80);
		expect(
			s.sent.map((e) => (e as { payload: { id: string } }).payload.id),
		).toEqual(["first", "second"]);
	});
});

describe("routeEvent control and legacy paths", () => {
	it("iam.changed re-syncs and delivers nothing", () => {
		let changed = 0;
		const s = socket("u");
		const { io, emitted } = fakeIo({ [`project:${P}:tasks`]: [s] });
		routeEvent(
			io,
			{ type: "iam.changed", payload: { role_ids: ["r"] } },
			log,
			routing(() => true, { onIamChanged: () => changed++ }).routing,
		);
		expect(changed).toBe(1);
		expect(s.sent).toEqual([]);
		expect(emitted).toEqual([]);
	});

	it("checks a descriptor-less agent conversation event as conversations:read on the conversation", async () => {
		const reader = socket("reader");
		const other = socket("other");
		const { io, emitted } = fakeIo({
			[`project:${P}:conversations`]: [reader, other],
		});
		const conv = `project/${P}/conversation/c1`;
		const r = routing(
			(u, res, action) =>
				u === "reader" && res === conv && action === "conversations:read",
		);
		routeEvent(
			io,
			{
				type: "agent.session.started",
				payload: { project_id: P, conversation_id: "c1", secret: "x" },
			},
			log,
			r.routing,
		);
		await sleep(20);
		expect(reader.sent.length).toBe(1);
		expect(other.sent).toEqual([]);
		// The tasks-room copy is payload-free.
		expect(emitted).toEqual([
			{
				room: `project:${P}:tasks`,
				event: {
					type: "agent.session.started",
					payload: { project_id: P, conversation_id: "c1" },
				},
			},
		]);
	});

	it("a session start that carries a descriptor still refreshes task boards, without its content", async () => {
		const reader = socket("reader");
		const { io, emitted } = fakeIo({
			[`project:${P}:conversations`]: [reader],
		});
		routeEvent(
			io,
			{
				type: "agent.session.started",
				payload: { project_id: P, conversation_id: "c1", secret: "x" },
				authz: authzFor("conversations:read", `project/${P}/conversation/c1`),
			},
			log,
			routing(() => true).routing,
		);
		await sleep(20);
		expect(reader.sent.length).toBe(1);
		expect(emitted).toEqual([
			{
				room: `project:${P}:tasks`,
				event: {
					type: "agent.session.started",
					payload: { project_id: P, conversation_id: "c1" },
				},
			},
		]);
	});

	it("falls back to room delivery for a project event with no descriptor", () => {
		const { io, emitted } = fakeIo({});
		const r = routing(() => true);
		const msg = { type: "sprint.updated", payload: { project_id: P } };
		routeEvent(io, msg, log, r.routing);
		routeEvent(io, msg, log, r.routing);
		expect(emitted.length).toBe(2);
		expect(emitted[0]?.room).toBe(`project:${P}:sprints`);
		expect(r.routing.warnedNoAuthz.has("sprint.updated")).toBe(true);
	});

	it("routes notifications to the recipient and owner-private agent events to their owner", () => {
		const { io, emitted } = fakeIo({});
		const r = routing(() => true);
		routeEvent(
			io,
			{ type: "notification.created", payload: { recipient_account_id: "u9" } },
			log,
			r.routing,
		);
		routeEvent(
			io,
			{
				type: "agent.message",
				payload: { actor_account_id: "u9", project_id: P },
			},
			log,
			r.routing,
		);
		expect(emitted.map((e) => e.room)).toEqual([
			"user:u9:notifications",
			"user:u9:agent-chat",
		]);
	});
});

describe("routeEvent content changes", () => {
	it("hands doc.updated and task.updated to the collab merge, and nothing else", () => {
		const seen: string[] = [];
		const { io } = fakeIo({});
		const r = routing(() => true);
		const withHook = {
			...r.routing,
			onContentChange: (type: string) => seen.push(type),
		};
		for (const type of ["doc.updated", "task.updated", "task.created"]) {
			routeEvent(io, { type, payload: { project_id: P } }, log, withHook);
		}
		expect(seen).toEqual(["doc.updated", "task.updated"]);
	});
});
