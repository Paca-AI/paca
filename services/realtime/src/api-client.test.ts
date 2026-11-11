import { afterEach, describe, expect, it } from "bun:test";

import {
	ApiRequestError,
	checkAccess,
	fetchProjectPermissions,
} from "./api-client.ts";
import {
	eventNamespace,
	hasAction,
	NAMESPACE_PERMISSIONS,
	namespaceForAction,
} from "./permissions.ts";

const realFetch = globalThis.fetch;
afterEach(() => {
	globalThis.fetch = realFetch;
});

function mockFetch(status: number, body: unknown, seen?: string[]) {
	globalThis.fetch = (async (url: string) => {
		seen?.push(String(url));
		return new Response(JSON.stringify(body), { status });
	}) as unknown as typeof fetch;
}

describe("fetchProjectPermissions", () => {
	it("turns the actions list into an action -> true map, including child-only actions", async () => {
		const seen: string[] = [];
		mockFetch(200, { data: { actions: ["tasks:read", "docs:read"] } }, seen);
		const perms = await fetchProjectPermissions("http://api", "tok", "p1");
		expect(perms).toEqual({ "tasks:read": true, "docs:read": true });
		// No scope=project: an action allowed on one sprint's tasks must still
		// put the socket in the room; the per-event check does the narrowing.
		expect(seen[0]).toEndWith("/projects/p1/members/me/permissions");
	});

	it("returns an empty map when no actions come back", async () => {
		mockFetch(200, { data: {} });
		expect(await fetchProjectPermissions("http://api", "tok", "p1")).toEqual(
			{},
		);
	});

	it("returns null for a non-member (404)", async () => {
		mockFetch(404, {});
		expect(await fetchProjectPermissions("http://api", "tok", "p1")).toBeNull();
	});

	it("throws on other failures, keeping the status so a 401 can force a reconnect", async () => {
		mockFetch(500, {});
		await expect(
			fetchProjectPermissions("http://api", "tok", "p1"),
		).rejects.toThrow();
		mockFetch(401, {});
		const err = await fetchProjectPermissions("http://api", "tok", "p1").catch(
			(e) => e,
		);
		expect(err).toBeInstanceOf(ApiRequestError);
		expect((err as ApiRequestError).status).toBe(401);
	});
});

describe("checkAccess", () => {
	const queries = [
		{ action: "tasks:read", resource: "project/p1/task/t1" },
		{ action: "tasks:read", resource: "project/p1/task/t2" },
	];

	it("posts the checks as the caller and returns the answers in order", async () => {
		let sent: { url: string; init: RequestInit } | undefined;
		globalThis.fetch = (async (url: string, init: RequestInit) => {
			sent = { url: String(url), init };
			return new Response(JSON.stringify({ data: { allowed: [true, false] } }));
		}) as unknown as typeof fetch;
		expect(await checkAccess("http://api", "tok", "p1", queries)).toEqual([
			true,
			false,
		]);
		expect(sent?.url).toEndWith("/projects/p1/members/me/permissions/check");
		expect(sent?.init.method).toBe("POST");
		expect((sent?.init.headers as Record<string, string>).Authorization).toBe(
			"Bearer tok",
		);
		expect(JSON.parse(String(sent?.init.body))).toEqual({ checks: queries });
	});

	it("rejects an incomplete or malformed answer instead of guessing", async () => {
		mockFetch(200, { data: { allowed: [true] } });
		await expect(
			checkAccess("http://api", "tok", "p1", queries),
		).rejects.toThrow();
		mockFetch(200, { data: { allowed: ["yes", "no"] } });
		await expect(
			checkAccess("http://api", "tok", "p1", queries),
		).rejects.toThrow();
		mockFetch(200, {});
		await expect(
			checkAccess("http://api", "tok", "p1", queries),
		).rejects.toThrow();
	});

	it("throws an ApiRequestError carrying the status", async () => {
		mockFetch(401, {});
		const err = await checkAccess("http://api", "tok", "p1", queries).catch(
			(e) => e,
		);
		expect(err).toBeInstanceOf(ApiRequestError);
		expect((err as ApiRequestError).status).toBe(401);
	});
});

describe("namespace permissions", () => {
	it("use domain:action names that the API actions satisfy", () => {
		for (const [ns, required] of Object.entries(NAMESPACE_PERMISSIONS)) {
			expect(required).toContain(":");
			expect(hasAction({ [required]: true }, required)).toBe(true);
			expect(namespaceForAction(required)).toBe(ns as never);
		}
		expect(hasAction({ "docs:read": true }, "tasks:read")).toBe(false);
		// The API lists concrete actions; a wildcard string means nothing here.
		expect(hasAction({ "tasks:*": true }, "tasks:read")).toBe(false);
	});

	it("gives views their own room, matching the API's views:read", () => {
		expect(namespaceForAction("views:read")).toBe("views");
		expect(namespaceForAction("tasks:write")).toBeUndefined();
	});

	it("routes project settings, members and agent configuration to their own rooms", () => {
		expect(namespaceForAction("projects:read")).toBe("project");
		expect(namespaceForAction("project.members:read")).toBe("members");
		expect(namespaceForAction("agents:read")).toBe("agents");
		expect(eventNamespace("member.added")).toBe("members");
		expect(eventNamespace("project.updated")).toBe("project");
		expect(eventNamespace("project_agent.updated")).toBe("agents");
		expect(eventNamespace("project.task_status.reordered")).toBe("tasks");
		expect(eventNamespace("project.custom_field.created")).toBe("tasks");
		// agent.* stays conversation content, not agent configuration.
		expect(eventNamespace("agent.message")).toBe("conversations");
	});
});
