import { afterEach, describe, expect, it } from "bun:test";
import type { Logger } from "pino";
import type { Server, Socket } from "socket.io";

import { AccessDecider } from "./access.ts";
import { createResyncer } from "./resync.ts";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const log = {
	debug() {},
	warn() {},
	info() {},
	error() {},
} as unknown as Logger;
const realFetch = globalThis.fetch;
afterEach(() => {
	globalThis.fetch = realFetch;
});

function fakeSocket(userId: string, projects: string[], rooms: string[]) {
	const emitted: Array<[string, unknown]> = [];
	let disconnected = false;
	const s = {
		data: { userId, rawToken: `tok-${userId}`, projects: new Set(projects) },
		rooms: new Set(rooms),
		join(r: string) {
			this.rooms.add(r);
		},
		leave(r: string) {
			this.rooms.delete(r);
		},
		emit: (n: string, p: unknown) => emitted.push([n, p]),
		disconnect: () => {
			disconnected = true;
		},
	};
	return { s, emitted, wasDisconnected: () => disconnected };
}

function ioOf(...sockets: Array<ReturnType<typeof fakeSocket>>) {
	return {
		sockets: {
			sockets: new Map(
				sockets.map((x, i) => [String(i), x.s as unknown as Socket]),
			),
		},
	} as unknown as Server;
}

function api(handler: (token: string, url: string) => Response) {
	const urls: string[] = [];
	globalThis.fetch = (async (url: string, init: RequestInit) => {
		urls.push(String(url));
		const auth = (init.headers as Record<string, string>).Authorization ?? "";
		return handler(auth.replace("Bearer ", ""), String(url));
	}) as unknown as typeof fetch;
	return urls;
}

const actions = (...a: string[]) =>
	new Response(JSON.stringify({ data: { actions: a } }));

const isProjectPermissions = (u: string) =>
	u.includes("/members/me/permissions");

// A JWT whose payload the API client can decode (the signature is never checked).
const jwt = () =>
	`h.${Buffer.from(JSON.stringify({ sub: "u", username: "u", role: "r" })).toString("base64url")}.s`;

describe("resyncer", () => {
	it("keeps the workspace files room true to the account's platform files:read", async () => {
		const u = fakeSocket("u", [], ["workspace:files"]);
		u.s.data.rawToken = jwt();
		api((_token, url) =>
			url.includes("global-permissions")
				? actions("tasks:read")
				: new Response("{}", { status: 404 }),
		);
		createResyncer(
			ioOf(u),
			"http://api",
			new AccessDecider("http://api"),
			log,
		).onIamChanged();
		await sleep(30);
		expect([...u.s.rooms]).toEqual([]);

		api(() => actions("files:read"));
		createResyncer(
			ioOf(u),
			"http://api",
			new AccessDecider("http://api"),
			log,
		).onIamChanged();
		await sleep(30);
		expect([...u.s.rooms]).toEqual(["workspace:files"]);
	});

	it("on iam.changed drops cached decisions, takes a revoked room away and tells the client", async () => {
		const u = fakeSocket("u", ["p1"], ["project:p1:tasks", "project:p1:docs"]);
		let flushed = 0;
		const decider = new AccessDecider("http://api");
		decider.flush = () => {
			flushed++;
		};
		api(() => actions("tasks:read"));
		createResyncer(ioOf(u), "http://api", decider, log).onIamChanged();
		await sleep(30);

		expect(flushed).toBe(1);
		expect([...u.s.rooms]).toEqual(["project:p1:tasks"]);
		expect(u.emitted).toEqual([
			["permissions:changed", { projectId: "p1", namespaces: ["tasks"] }],
		]);
	});

	it("asks the API once per account and project, however many sockets", async () => {
		const a = fakeSocket("u", ["p1"], ["project:p1:tasks"]);
		const b = fakeSocket("u", ["p1"], ["project:p1:tasks"]);
		const urls = api(() => actions("tasks:read"));
		createResyncer(
			ioOf(a, b),
			"http://api",
			new AccessDecider("http://api"),
			log,
		).onIamChanged();
		await sleep(30);
		expect(urls.filter(isProjectPermissions).length).toBe(1);
		expect(a.emitted.length).toBe(1);
		expect(b.emitted.length).toBe(1);
	});

	it("empties the rooms of a removed member and forgets the project", async () => {
		const u = fakeSocket("u", ["p1"], ["project:p1:tasks"]);
		api(() => new Response("{}", { status: 404 }));
		createResyncer(
			ioOf(u),
			"http://api",
			new AccessDecider("http://api"),
			log,
		).onIamChanged();
		await sleep(30);
		expect([...u.s.rooms]).toEqual([]);
		expect(u.s.data.projects.size).toBe(0);
	});

	it("ends only the sockets whose own token is rejected, and still re-syncs the account's other sockets", async () => {
		// Two tabs of one account: one on an expired token, one on a fresh one.
		const stale = fakeSocket(
			"u",
			["p1"],
			["project:p1:tasks", "project:p1:docs"],
		);
		const fresh = fakeSocket(
			"u",
			["p1"],
			["project:p1:tasks", "project:p1:docs"],
		);
		stale.s.data.rawToken = "tok-stale";
		fresh.s.data.rawToken = "tok-fresh";
		const urls = api((token) =>
			token === "tok-stale"
				? new Response("{}", { status: 401 })
				: actions("tasks:read"),
		);
		createResyncer(
			ioOf(stale, fresh),
			"http://api",
			new AccessDecider("http://api"),
			log,
		).onIamChanged();
		await sleep(30);

		expect(stale.wasDisconnected()).toBe(true);
		expect(fresh.wasDisconnected()).toBe(false);
		// The healthy socket was re-synced on the account's answer.
		expect([...fresh.s.rooms]).toEqual(["project:p1:tasks"]);
		expect(fresh.emitted.length).toBe(1);
		// the stale token, then the fresh one
		expect(urls.filter(isProjectPermissions).length).toBe(2);
	});

	it("ends every socket of the account when all of its tokens are rejected", async () => {
		const a = fakeSocket("u", ["p1"], ["project:p1:tasks"]);
		const b = fakeSocket("u", ["p1"], ["project:p1:tasks"]);
		a.s.data.rawToken = "tok-a";
		b.s.data.rawToken = "tok-b";
		api(() => new Response("{}", { status: 401 }));
		createResyncer(
			ioOf(a, b),
			"http://api",
			new AccessDecider("http://api"),
			log,
		).onIamChanged();
		await sleep(30);
		expect(a.wasDisconnected()).toBe(true);
		expect(b.wasDisconnected()).toBe(true);
	});

	it("disconnects a socket whose token the API rejects, and keeps rooms on other failures", async () => {
		const dead = fakeSocket("dead", ["p1"], ["project:p1:tasks"]);
		const flaky = fakeSocket("flaky", ["p1"], ["project:p1:tasks"]);
		api(
			(token) =>
				new Response("{}", { status: token === "tok-dead" ? 401 : 500 }),
		);
		createResyncer(
			ioOf(dead, flaky),
			"http://api",
			new AccessDecider("http://api"),
			log,
		).onIamChanged();
		await sleep(30);
		expect(dead.wasDisconnected()).toBe(true);
		expect(flaky.wasDisconnected()).toBe(false);
		expect([...flaky.s.rooms]).toEqual(["project:p1:tasks"]);
	});
});
